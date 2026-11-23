/**
 * Typed client for the exam-service backends that the EC2/VPC consoles
 * wire to directly: EBS volumes, Auto Scaling groups, and the three VPN
 * record kinds. All five are real API-EMULATED backends
 * (backend/internal/httpapi/exam.go): durable per-user records with
 * CRUD plus actions — EBS attach/detach, ASG scale, VPN tunnel
 * activate/deactivate. The registry (services.go) already says so;
 * the consoles used to claim the opposite, and this module is the fix.
 *
 * Honesty notes the consoles must keep next to these calls:
 * - EBS attach is recorded only: the lab's disk is unchanged and no
 *   device is attached to any VM (the backend says so in its note).
 * - ASG scale updates the recorded desired/current capacity only;
 *   no instance is launched or terminated.
 * - VPN tunnel state is a teaching field: no IPsec tunnel exists and
 *   no traffic flows.
 */
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { request } from './api.ts'
import { fmtTime, listOf, num, str, useExamList, type Rec } from './examConsole.ts'

export type { Rec }
export { fmtTime, listOf, num, str }

export function useEbsVolumes() {
  return useExamList('ebs/volumes', 'volumes')
}

export function useAutoScalingGroups() {
  return useExamList('autoscaling/groups', 'groups')
}

export function useVpnConnections() {
  return useExamList('vpn/connections', 'connections')
}

export function useCustomerGateways() {
  return useExamList('vpn/customer-gateways', 'customer-gateways')
}

export function useVpnGateways() {
  return useExamList('vpn/vpn-gateways', 'vpn-gateways')
}

/** POST /api/v1/aws/<listPath>; invalidates the list query on success. */
export function useExamCreate(listPath: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) =>
      request<Rec>(`/api/v1/aws/${listPath}`, { method: 'POST', body }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['exam-list', listPath] }),
  })
}

/**
 * POST /api/v1/aws/<listPath>/<id>/<action> (attach, detach, scale,
 * activate, deactivate); invalidates the list query on success.
 */
export function useExamAction(listPath: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, action, body }: { id: string; action: string; body?: Record<string, unknown> }) =>
      request<Rec>(`/api/v1/aws/${listPath}/${encodeURIComponent(id)}/${action}`, {
        method: 'POST',
        body: body ?? {},
      }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['exam-list', listPath] }),
  })
}

/** DELETE /api/v1/aws/<listPath>/<id>; invalidates the list query on success. */
export function useExamDelete(listPath: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      request<unknown>(`/api/v1/aws/${listPath}/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['exam-list', listPath] }),
  })
}

/** Attachments recorded on an EBS volume: [{ lab_id, device, attached_at }]. */
export function volumeAttachments(r: Rec): { labId: string; device: string; attachedAt: string }[] {
  if (!Array.isArray(r.attachments)) return []
  return (r.attachments as Record<string, unknown>[])
    .filter((a) => typeof a === 'object' && a !== null)
    .map((a) => ({
      labId: typeof a.lab_id === 'string' ? a.lab_id : '—',
      device: typeof a.device === 'string' ? a.device : '—',
      attachedAt: typeof a.attached_at === 'string' ? a.attached_at : '—',
    }))
}
