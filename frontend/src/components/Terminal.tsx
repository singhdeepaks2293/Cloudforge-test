/**
 * Lab terminal (xterm.js) — Milestone 6 frontend, M9 Part 2 token fix.
 *
 * Live mode: first calls POST /api/v1/labs/:id/terminal/token for a
 * short-lived, single-use token, then opens the WebSocket to
 * /api/v1/labs/{id}/terminal with THAT token. The long-lived Bearer
 * token is never passed to the WebSocket (the backend rejects it).
 * Reconnects request a fresh token each time. Exponential backoff
 * (1s, 2s, 4s, 8s; max 5 attempts), fits on resize, reports session
 * expiry in the terminal.
 * Echo mode (`mode="echo"`): a clearly labelled UI test only — local
 * echo of typed input, never labelled as a real lab shell. Use it to
 * verify the terminal renders, resizes and handles input while the
 * backend endpoint is pending.
 */

import { useEffect, useRef, useState } from 'react'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { buildTerminalWsUrl, getToken, requestTerminalToken } from '../services/api.ts'

export type TerminalMode = 'live' | 'echo'

const MAX_RECONNECTS = 5
const BACKOFF_MS = [1000, 2000, 4000, 8000, 8000]

function terminalStatusLabel(status: string): string {
  switch (status) {
    case 'connecting':
      return 'Connecting…'
    case 'connected':
      return 'Connected'
    case 'reconnecting':
      return 'Reconnecting…'
    case 'expired':
      return 'Session expired'
    case 'unavailable':
      return 'Terminal backend not yet available'
    case 'echo':
      return 'UI test only — local echo'
    default:
      return 'Disconnected'
  }
}

export function Terminal({
  labId,
  mode = 'live',
  height = 360,
}: {
  labId: string
  mode?: TerminalMode
  height?: number
}) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [status, setStatus] = useState<string>(mode === 'echo' ? 'echo' : 'connecting')
  const [attempt, setAttempt] = useState(0)
  // Bumped by Retry: re-runs the effect, i.e. a fresh terminal + WS cycle.
  const [retryKey, setRetryKey] = useState(0)

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      theme: { background: '#0f172a', foreground: '#e2e8f0', cursor: '#e2e8f0' },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(el)
    fit.fit()

    const resizeObserver = new ResizeObserver(() => {
      try {
        fit.fit()
      } catch {
        // Terminal disposed mid-resize; ignore.
      }
    })
    resizeObserver.observe(el)
    const onWindowResize = () => {
      try {
        fit.fit()
      } catch {
        // ignore
      }
    }
    window.addEventListener('resize', onWindowResize)

    let ws: WebSocket | null = null
    let disposed = false
    let reconnects = 0
    let timer: ReturnType<typeof setTimeout> | null = null

    if (mode === 'echo') {
      term.writeln('CloudForge terminal — UI TEST ONLY (local echo).')
      term.writeln('This is not a lab shell: the terminal backend')
      term.writeln('(WS /api/v1/labs/:id/terminal) is not yet available.')
      term.writeln('Type to verify the terminal renders and echoes input.')
      term.write('\r\n$ ')
      const echoDisposable = term.onData((data) => {
        for (const ch of data) {
          if (ch === '\r') term.write('\r\n$ ')
          else if (ch === '\u007f') term.write('\b \b')
          else term.write(ch)
        }
      })
      return () => {
        echoDisposable.dispose()
        resizeObserver.disconnect()
        window.removeEventListener('resize', onWindowResize)
        term.dispose()
      }
    }

    // M9 Part 2 fix (was the M6 gap): the long-lived Bearer <redacted> must NEVER
    // be passed as the WebSocket token — the backend rejects it. Each
    // connection first calls POST /api/v1/labs/:id/terminal/token and
    // uses the returned short-lived token. Terminal tokens are
    // single-use, so every (re)connect requests a fresh one.
    if (!getToken()) {
      setStatus('unavailable')
      term.writeln('Terminal backend not yet available — and no auth token')
      term.writeln('is stored. Sign in first, then retry once the')
      term.writeln('WS /api/v1/labs/:id/terminal endpoint lands.')
      return () => {
        resizeObserver.disconnect()
        window.removeEventListener('resize', onWindowResize)
        term.dispose()
      }
    }

    const connect = async () => {
      if (disposed) return
      setStatus(reconnects === 0 ? 'connecting' : 'reconnecting')
      setAttempt(reconnects)
      let token: string
      try {
        const issued = await requestTerminalToken(labId)
        token = issued.token
      } catch (err) {
        scheduleReconnect(
          `Could not get a terminal token (POST /api/v1/labs/${labId}/terminal/token: ${
            err instanceof Error ? err.message : 'request failed'
          }).`,
        )
        return
      }
      if (disposed) return
      let socket: WebSocket
      try {
        socket = new WebSocket(buildTerminalWsUrl(labId, token))
      } catch {
        scheduleReconnect('Could not open the terminal WebSocket.')
        return
      }
      ws = socket
      socket.onopen = () => {
        if (disposed) return
        reconnects = 0
        setStatus('connected')
        term.writeln('Connected to lab terminal.')
      }
      socket.onmessage = (ev) => {
        if (disposed) return
        term.write(typeof ev.data === 'string' ? ev.data : '')
      }
      socket.onclose = (ev) => {
        if (disposed) return
        ws = null
        if (ev.code === 1008 || ev.code === 4401) {
          setStatus('expired')
          term.writeln('\r\nSession expired: the terminal token is no longer valid.')
          term.writeln('Sign in again and reopen the workstation.')
          return
        }
        scheduleReconnect(
          'Terminal backend not yet available (WS /api/v1/labs/:id/terminal). ' +
            'The connection closed before a shell was established.',
        )
      }
      socket.onerror = () => {
        // onclose follows with the detail; nothing to write here.
      }
    }

    const scheduleReconnect = (message: string) => {
      if (disposed) return
      if (reconnects >= MAX_RECONNECTS) {
        setStatus('unavailable')
        term.writeln(`\r\n${message}`)
        term.writeln('Gave up after 5 reconnect attempts. Use Retry below.')
        return
      }
      const delay = BACKOFF_MS[Math.min(reconnects, BACKOFF_MS.length - 1)]
      reconnects += 1
      setStatus('reconnecting')
      setAttempt(reconnects)
      term.writeln(`\r\n${message} Retrying in ${delay / 1000}s (attempt ${reconnects}/${MAX_RECONNECTS})…`)
      timer = setTimeout(connect, delay)
    }

    const inputDisposable = term.onData((data) => {
      if (ws && ws.readyState === WebSocket.OPEN) ws.send(data)
    })

    connect()
    return () => {
      disposed = true
      if (timer !== null) clearTimeout(timer)
      inputDisposable.dispose()
      if (ws) ws.close()
      resizeObserver.disconnect()
      window.removeEventListener('resize', onWindowResize)
      term.dispose()
    }
    // Reconnect only when the lab, mode or Retry changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [labId, mode, retryKey])

  const statusColor =
    status === 'connected'
      ? 'bg-emerald-100 text-emerald-800'
      : status === 'echo'
        ? 'bg-sky-100 text-sky-800'
        : status === 'unavailable' || status === 'expired'
          ? 'bg-amber-100 text-amber-800'
          : 'bg-slate-100 text-slate-600'

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${statusColor}`}>
          {terminalStatusLabel(status)}
          {status === 'reconnecting' && attempt > 0 ? ` (attempt ${attempt}/${MAX_RECONNECTS})` : ''}
        </span>
        {mode === 'live' && (status === 'unavailable' || status === 'expired') && (
          <button
            type="button"
            onClick={() => setRetryKey((k) => k + 1)}
            className="rounded-md border border-slate-300 px-2 py-1 text-xs font-medium hover:bg-slate-100"
          >
            Retry connection
          </button>
        )}
        {mode === 'echo' && (
          <span className="text-xs font-medium text-sky-700">UI test only — not a lab shell</span>
        )}
      </div>
      <div ref={containerRef} style={{ height }} className="overflow-hidden rounded-md bg-slate-900" />
      {mode === 'live' && (
        <p className="text-xs text-slate-500">
          Live terminal connects with a short-lived token from{' '}
          <code className="font-mono">POST /api/v1/labs/:id/terminal/token</code> — the long-lived sign-in
          token is never sent to the WebSocket. Switch to the echo test to check the UI itself.
        </p>
      )}
    </div>
  )
}
