import { ApiError, type Row } from './api'

export type LiveStatus = 'connecting' | 'live' | 'reconnecting' | 'paused' | 'blocked'

export function accountLive(options: {
  account: string
  workspace: import('./workspaces').Workspace
  view: 'messages' | 'dashboard'
  update: (data: Row) => void
  status: (state: LiveStatus) => void
}) {
  let stopped = false
  let connection: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let failures = 0
  const active = () => !stopped && !document.hidden
  const schedule = () => {
    if (!active()) return
    options.status('reconnecting')
    timer = setTimeout(connect, Math.min(30000, 1000 * 2 ** Math.min(failures++, 5)))
  }
  async function connect() {
    if (!active()) return
    const controller = new AbortController()
    connection = controller
    options.status(failures ? 'reconnecting' : 'connecting')
    let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
    try {
      const response = await fetch(
        `/api/v1/accounts/${options.account}/events?view=${options.view}`,
        {
          credentials: 'same-origin',
          headers: { 'X-Workspace': options.workspace },
          signal: controller.signal
        }
      )
      if (!response.ok) throw new ApiError(response.status, '实时订阅不可用')
      if (!response.headers.get('Content-Type')?.startsWith('text/event-stream') || !response.body)
        throw new Error('实时订阅格式不兼容')
      reader = response.body.getReader()
      const decoder = new TextDecoder('utf-8', { fatal: true })
      let buffer = '',
        sequence = 0
      while (active() && !controller.signal.aborted) {
        const { value, done } = await reader.read()
        buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
        buffer = buffer.replace(/\r\n/g, '\n')
        let boundary: number
        while ((boundary = buffer.indexOf('\n\n')) >= 0) {
          const frame = buffer.slice(0, boundary)
          buffer = buffer.slice(boundary + 2)
          if (frame.length > 65536) throw new Error('实时事件超过大小限制')
          const lines = frame.split('\n')
          const event = lines
            .find((line) => line.startsWith('event:'))
            ?.slice(6)
            .trim()
          const raw = lines
            .filter((line) => line.startsWith('data:'))
            .map((line) => line.slice(5).trimStart())
            .join('\n')
          if (!raw) continue
          if (event === 'error') throw new ApiError(403, '实时订阅权限已变化或暂不可用')
          const id = Number(
            lines
              .find((line) => line.startsWith('id:'))
              ?.slice(3)
              .trim()
          )
          const data: unknown = JSON.parse(raw)
          if (
            event !== 'update' ||
            id !== sequence + 1 ||
            !data ||
            typeof data !== 'object' ||
            Array.isArray(data)
          )
            throw new Error('实时事件不连续')
          const update = data as Row
          if (
            update.account_id !== options.account ||
            update.workspace !== options.workspace ||
            typeof update.reset !== 'boolean' ||
            (update.decoded_ids !== undefined &&
              (!Array.isArray(update.decoded_ids) ||
                update.decoded_ids.length > 64 ||
                !update.decoded_ids.every(
                  (value: unknown) => typeof value === 'string' && /^[1-9]\d{0,18}$/.test(value)
                )))
          )
            throw new Error('实时事件作用域不正确')
          sequence = id
          if (active() && !controller.signal.aborted) {
            failures = 0
            options.status('live')
            options.update(update)
          }
        }
        if (buffer.length > 65536) throw new Error('实时事件超过大小限制')
        if (done) break
      }
    } catch (error) {
      if (!controller.signal.aborted && active()) {
        if (error instanceof ApiError && [400, 401, 403, 404].includes(error.status)) {
          options.status('blocked')
          stopped = true
        }
      }
    } finally {
      await reader?.cancel().catch(() => {})
      reader?.releaseLock()
      if (!controller.signal.aborted) schedule()
    }
  }
  const visibility = () => {
    connection?.abort()
    clearTimeout(timer)
    if (active()) {
      failures = 0
      void connect()
    } else if (!stopped) options.status('paused')
  }
  document.addEventListener('visibilitychange', visibility)
  if (active()) void connect()
  else options.status('paused')
  return () => {
    stopped = true
    clearTimeout(timer)
    connection?.abort()
    document.removeEventListener('visibilitychange', visibility)
  }
}
