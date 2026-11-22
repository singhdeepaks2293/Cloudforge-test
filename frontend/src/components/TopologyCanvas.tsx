/**
 * React Flow topology canvas, shared by /labs/:id/topology and the
 * workstation. Nodes/edges come only from real data: the live
 * topology feed (GET /api/v1/labs/{id}/topology, M9 Part 3) when it
 * answers, otherwise the lab's recorded resource list (GET
 * /api/v1/labs/{id}).
 *
 * Add node: POST /api/v1/labs/{id}/resources is live — the form
 * creates a real resource through the backend (subnet creation goes
 * through the Rust engine, instance creation through the host
 * agent) and reports the real result. No fake success, no local-only
 * drafts.
 * Delete node: DELETE /api/v1/labs/{id}/resources/{id} is live and
 * idempotent. Nodes merged in from the host agent (ids prefixed
 * `hostagent:`) have no DB row, so their delete control is disabled
 * with the reason shown.
 */

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { Lab, LabResourceKind, Topology } from '../types/index.ts'
import { createLabResource, deleteLabResource } from '../services/api.ts'
import {
  buildTopologyFromLab,
  buildTopologyFromTopologyNodes,
  kindLabel,
  type FlowNode,
} from '../services/topology.ts'

const KIND_STYLES: Record<LabResourceKind, string> = {
  vpc: 'border-aws-link bg-[#e9f5fe]',
  subnet: 'border-[#007a7a] bg-[#e6f4f4]',
  instance: 'border-aws-orange bg-aws-orange-light',
  gateway: 'border-[#7a5cfa] bg-[#f0ebfe]',
  loadbalancer: 'border-aws-error bg-aws-error-bg',
  'route-table': 'border-[#007a7a] bg-[#e6f4f4]',
  'security-group': 'border-aws-error bg-aws-error-bg',
  unknown: 'border-aws-border-dark bg-aws-bg',
}

function CfNode({ data, selected }: NodeProps<FlowNode>) {
  const cls = KIND_STYLES[data.kind]
  return (
    <div
      className={`w-56 rounded-lg border-2 px-3 py-2 shadow-sm ${cls} ${selected ? 'ring-2 ring-aws-orange' : ''}`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] font-bold uppercase tracking-wide">{kindLabel(data.kind)}</span>
        {data.draft && (
          <span className="rounded-full bg-amber-200 px-1.5 py-0.5 text-[10px] font-bold text-amber-900">DRAFT</span>
        )}
      </div>
      <p className="truncate text-sm font-semibold">{data.label}</p>
      {data.state && <p className="text-xs text-aws-muted">State: {data.state}</p>}
      {data.ips.map((ip) => (
        <p key={ip} className="font-mono text-xs text-aws-muted">
          {ip}
        </p>
      ))}
    </div>
  )
}

const nodeTypes = { cfNode: CfNode }

export function TopologyCanvas({
  lab,
  topology,
  topologyError,
  onChanged,
  height = 480,
}: {
  lab: Lab
  /** Live topology payload from GET /api/v1/labs/{id}/topology. */
  topology?: Topology | null
  /** Error message when the live feed failed (e.g. engine down: 503). */
  topologyError?: string | null
  /** Called after a resource is created or deleted so the parent refetches. */
  onChanged?: () => void
  height?: number
}) {
  const built = useMemo(
    () =>
      topology && topology.nodes.length > 0
        ? buildTopologyFromTopologyNodes(topology.nodes)
        : buildTopologyFromLab(lab),
    [lab, topology],
  )
  const [nodes, setNodes, onNodesChange] = useNodesState(built.nodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(built.edges)
  const [selected, setSelected] = useState<FlowNode | null>(null)
  const [newKind, setNewKind] = useState<LabResourceKind>('instance')
  const [newName, setNewName] = useState('')
  const [newCidr, setNewCidr] = useState('')
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null)

  // Rebuild from real data when the lab/topology payload changes.
  useEffect(() => {
    setNodes(built.nodes)
    setEdges(built.edges)
    setSelected(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [built.nodes, built.edges])

  const onNodeClick = useCallback((_e: unknown, node: FlowNode) => {
    setSelected(node)
  }, [])

  const addResource = async () => {
    const name = newName.trim()
    if (name === '') {
      setNote({ tone: 'error', text: 'A name is required to create a resource.' })
      return
    }
    if (newKind === 'subnet' && newCidr.trim() === '') {
      setNote({ tone: 'error', text: 'Subnet creation requires a CIDR (e.g. 10.0.1.0/24).' })
      return
    }
    setBusy(true)
    setNote(null)
    try {
      const created = await createLabResource(lab.id, {
        kind: newKind,
        name,
        cidr: newCidr.trim() === '' ? undefined : newCidr.trim(),
      })
      setNote({ tone: 'ok', text: `Created ${kindLabel(created.kind)} "${created.name}" (${created.id}).` })
      setNewName('')
      setNewCidr('')
      onChanged?.()
    } catch (e) {
      setNote({
        tone: 'error',
        text: `Could not create ${kindLabel(newKind)} "${name}": ${e instanceof Error ? e.message : 'unknown error'}`,
      })
    } finally {
      setBusy(false)
    }
  }

  const deleteSelected = async () => {
    if (!selected) return
    setBusy(true)
    setNote(null)
    try {
      await deleteLabResource(lab.id, selected.id)
      setNote({ tone: 'ok', text: `Deleted "${selected.data.label}".` })
      setSelected(null)
      onChanged?.()
    } catch (e) {
      setNote({
        tone: 'error',
        text: `Could not delete "${selected.data.label}": ${e instanceof Error ? e.message : 'unknown error'}`,
      })
    } finally {
      setBusy(false)
    }
  }

  const selectedIsHostAgent = selected?.id.startsWith('hostagent:') ?? false
  const resourceCount = lab.resources?.length ?? 0

  return (
    <div className="space-y-3">
      {topology && (
        <div className="rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900" role="status">
          Live topology feed: {topology.nodes.length} node{topology.nodes.length === 1 ? '' : 's'} from{' '}
          <code className="font-mono">GET /api/v1/labs/{lab.id}/topology</code>.
        </div>
      )}
      {!topology && topologyError && (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900" role="status">
          Live topology feed unavailable ({topologyError}). Nodes below are built from the lab&apos;s recorded
          resource list
          {resourceCount === 0
            ? ' — which currently returns no resources, so the canvas is empty rather than invented.'
            : ` (${resourceCount} resource${resourceCount === 1 ? '' : 's'}).`}
        </div>
      )}
      {!topology && !topologyError && (
        <div className="rounded-md border border-aws-border bg-aws-bg px-4 py-3 text-sm text-aws-muted" role="status">
          No live topology nodes yet — showing the lab&apos;s recorded resources
          {resourceCount === 0 ? ' (none yet). Create a resource below to populate the canvas.' : '.'}
        </div>
      )}

      <div className="flex flex-wrap items-end gap-2">
        <label className="text-sm">
          <span className="mr-2 text-aws-muted">Kind</span>
          <select
            value={newKind}
            onChange={(e) => setNewKind(e.target.value as LabResourceKind)}
            className="rounded-md border border-aws-border-dark px-2 py-1 text-sm"
          >
            {(['vpc', 'subnet', 'instance', 'gateway', 'loadbalancer'] as LabResourceKind[]).map((k) => (
              <option key={k} value={k}>
                {kindLabel(k)}
              </option>
            ))}
          </select>
        </label>
        <label className="text-sm">
          <span className="mr-2 text-aws-muted">Name</span>
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="e.g. web-01"
            className="rounded-md border border-aws-border-dark px-2 py-1 text-sm"
          />
        </label>
        <label className="text-sm">
          <span className="mr-2 text-aws-muted">CIDR</span>
          <input
            value={newCidr}
            onChange={(e) => setNewCidr(e.target.value)}
            placeholder="10.0.1.0/24 (subnet/VPC)"
            className="rounded-md border border-aws-border-dark px-2 py-1 font-mono text-sm"
          />
        </label>
        <button
          type="button"
          onClick={addResource}
          disabled={busy}
          className="rounded-sm bg-aws-orange px-3 py-1.5 text-sm font-medium text-aws-navy hover:bg-aws-orange-dark hover:text-white disabled:opacity-50"
        >
          {busy ? 'Creating…' : 'Add resource'}
        </button>
        <span className="text-xs text-aws-muted">
          Creates a real resource via POST /api/v1/labs/{lab.id}/resources (subnets need a CIDR).
        </span>
      </div>
      {note && (
        <p
          className={`rounded-md border px-3 py-2 text-xs ${
            note.tone === 'ok'
              ? 'border-emerald-200 bg-emerald-50 text-emerald-900'
              : 'border-amber-200 bg-amber-50 text-amber-900'
          }`}
          role="status"
        >
          {note.text}
        </p>
      )}

      <div className="flex gap-4">
        <div style={{ height }} className="min-w-0 flex-1 border border-aws-border bg-white">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={onNodeClick}
            nodeTypes={nodeTypes}
            fitView
            proOptions={{ hideAttribution: true }}
          >
            <Background />
            <Controls />
            <MiniMap />
          </ReactFlow>
        </div>
        <aside className="w-64 shrink-0 border border-aws-border bg-white p-4">
          <h3 className="text-sm font-semibold">Inspector</h3>
          {!selected ? (
            <p className="mt-2 text-sm text-aws-muted">Select a node on the canvas to inspect it.</p>
          ) : (
            <div className="mt-2 space-y-1 text-sm">
              <p>
                <span className="font-medium">{kindLabel(selected.data.kind)}: </span>
                {selected.data.label}
              </p>
              <p className="break-all font-mono text-xs text-aws-muted">id: {selected.id}</p>
              {selected.data.state && <p>State: {selected.data.state}</p>}
              {selected.data.ips.length > 0 ? (
                selected.data.ips.map((ip) => (
                  <p key={ip} className="font-mono text-xs">
                    {ip}
                  </p>
                ))
              ) : (
                <p className="text-aws-muted">No IPs reported for this resource.</p>
              )}
              {selectedIsHostAgent ? (
                <p className="mt-3 text-xs text-aws-muted">
                  This node is merged in from the host agent and has no lab resource row, so it cannot be
                  deleted here — destroy the lab or remove its instance resource instead.
                </p>
              ) : (
                <button
                  type="button"
                  onClick={deleteSelected}
                  disabled={busy}
                  className="mt-3 rounded-md border border-red-300 px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 disabled:opacity-50"
                >
                  {busy ? 'Deleting…' : 'Delete resource'}
                </button>
              )}
            </div>
          )}
        </aside>
      </div>
    </div>
  )
}
