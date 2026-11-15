import { ApiError, type Row } from './api'

export function validMonitorSnapshot(value: Row, workspace: string): boolean {
  return (
    value !== null &&
    typeof value === 'object' &&
    value.workspace === workspace &&
    value.monitor_scope === 'platform_workspace' &&
    typeof value.updated_at === 'string' &&
    Number.isFinite(Date.parse(value.updated_at)) &&
    !value.updated_at.startsWith('0001-') &&
    Array.isArray(value.accounts) &&
    value.accounts.length <= 20 &&
    value.accounts.every(
      (account: Row) =>
        account !== null && typeof account === 'object' && account.workspace === workspace
    )
  )
}

export function subscribeMonitor(
  workspace: import('./workspaces').Workspace,
  receive: (value: Row) => void,
  status: (value: string) => void,
  denied: () => void
) {
  let stopped = false
  let current: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let retry = 1000
  let connecting = false
  async function connect() {
    if (stopped || document.hidden || connecting) return
    connecting = true
    current = new AbortController()
    status('连接中')
    try {
      const response = await fetch('/api/v1/system/monitor/events', {
        headers: { 'X-Workspace': workspace },
        credentials: 'same-origin',
        signal: current.signal
      })
      if ([401, 403, 404].includes(response.status)) {
        stopped = true
        denied()
        return
      }
      if (
        !response.ok ||
        !response.body ||
        !response.headers.get('content-type')?.startsWith('text/event-stream')
      )
        throw new ApiError(response.status, 'database_unavailable')
      const reader = response.body.getReader()
      const decoder = new TextDecoder('utf-8', { fatal: true })
      let buffer = ''
      let sequence = 0
      try {
        while (!stopped) {
          const part = await reader.read()
          if (part.done) break
          buffer += decoder.decode(part.value, { stream: true })
          if (buffer.length > 131072) throw new Error('monitor_frame_limit')
          let boundary: number
          while ((boundary = buffer.indexOf('\n\n')) !== -1) {
            const frame = buffer.slice(0, boundary)
            buffer = buffer.slice(boundary + 2)
            const lines = frame.split('\n')
            const id = Number(lines.find((line) => line.startsWith('id: '))?.slice(4))
            const event = lines.find((line) => line.startsWith('event: '))?.slice(7)
            const data = lines
              .filter((line) => line.startsWith('data: '))
              .map((line) => line.slice(6))
              .join('\n')
            if (event !== 'snapshot' || id !== sequence + 1) throw new Error('monitor_event_gap')
            const value = JSON.parse(data)
            if (!validMonitorSnapshot(value, workspace)) {
              throw new Error('monitor_scope_invalid')
            }
            sequence = id
            retry = 1000
            receive(value)
            status('实时连接')
          }
        }
      } finally {
        await reader.cancel().catch(() => {})
      }
    } catch {
      // A full snapshot replaces skipped intermediate samples after reconnect.
    } finally {
      connecting = false
      if (!stopped && !document.hidden) {
        status('重连中')
        timer = setTimeout(connect, retry)
        retry = Math.min(retry * 2, 30000)
      }
    }
  }
  function visibility() {
    clearTimeout(timer)
    if (document.hidden) {
      current?.abort()
      status('已暂停')
    } else {
      current?.abort()
      // The old connection owns reconnect scheduling until it finishes.
      timer = setTimeout(connect, 250)
    }
  }
  document.addEventListener('visibilitychange', visibility)
  void connect()
  return () => {
    stopped = true
    clearTimeout(timer)
    current?.abort()
    document.removeEventListener('visibilitychange', visibility)
  }
}
