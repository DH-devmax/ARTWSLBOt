import { ApiError } from '../api'

export interface Scope {
  platform?: 'wsl' | 'lajiaoliao'
  kind: 'overview' | 'account' | 'group'
  account_id?: string
  group_id?: string
}
export interface Turn {
  id: string
  session_id: string
  status: string
  result_code: string
  input?: string
  created_at?: string
}
export interface TranscriptTurn extends Turn {
  answer: string
  cards: Record<string, unknown>[]
  outcomes?: TranscriptOutcome[]
  cursor: number
  unavailable?: boolean
  truncated?: boolean
}
export interface TranscriptOutcome {
  evidence_state?: string
  plan_id: string
  kind: 'configuration' | 'action'
  status: string
  summary: string
  operation_id?: string
}
export interface Plan {
  platform?: 'wsl' | 'lajiaoliao'
  id: string
  session_id: string
  account_id: string
  group_id: string
  base_version: number
  digest: string
  version: number
  status: string
  expires_at: string
  changes: Record<string, Record<string, unknown>>
  before: Record<string, Record<string, unknown>>
  message: string
  summary: string
  execution_id?: string
  applied: false
}
export interface ActionPlan {
  policy_version?: number
  evidence_state?: string
  robot_nickname?: string
  observation?: { state: string; native_id?: string; event_id?: number; observed_at?: string }
  id: string
  session_id: string
  scope: Scope
  action: string
  name: string
  target?: { id: string; name: string; role: string }
  text?: string
  minutes?: number
  digest: string
  version: number
  status: string
  result_code: string
  summary: string
  operation_id?: string
  expires_at: string
  production_admitted: boolean
}
export interface StreamEvent {
  turn_id: string
  seq: number
  kind: string
  data: unknown
}
const kinds = new Set([
  'turn_started',
  'text_delta',
  'tool_started',
  'tool_result',
  'plan_created',
  'usage',
  'turn_finished',
  'error'
])
export async function write<T>(
  path: string,
  body: unknown,
  key: string,
  signal?: AbortSignal,
  platform: 'wsl' | 'lajiaoliao' = 'wsl'
): Promise<T> {
  const csrf =
    document.cookie
      .split('; ')
      .find((v) => v.startsWith('wsl_csrf='))
      ?.slice(9) || ''
  const response = await fetch(`/api/v1/assistant${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
    headers: {
      'Content-Type': 'application/json',
      'X-Workspace': platform,
      'X-CSRF-Token': csrf,
      'Idempotency-Key': key
    },
    body: JSON.stringify(body)
  })
  const result = await response.json()
  if (!response.ok || result.code !== 0) throw new ApiError(response.status, result.msg)
  return result.data as T
}
export function eventSchema(value: unknown): value is StreamEvent {
  if (!value || typeof value !== 'object') return false
  const v = value as Partial<StreamEvent>
  return (
    typeof v.turn_id === 'string' &&
    typeof v.seq === 'number' &&
    Number.isSafeInteger(v.seq) &&
    v.seq > 0 &&
    v.seq <= 512 &&
    typeof v.kind === 'string' &&
    kinds.has(v.kind) &&
    v.data !== undefined
  )
}
export async function stream(
  id: string,
  after: number,
  signal: AbortSignal,
  emit: (event: StreamEvent) => void,
  platform: 'wsl' | 'lajiaoliao' = 'wsl'
) {
  const response = await fetch(`/api/v1/assistant/turns/${id}/events?after=${after}`, {
    credentials: 'same-origin',
    headers: { 'X-Workspace': platform },
    signal
  })
  if (!response.ok) {
    const result = await response.json()
    throw new ApiError(response.status, result.msg)
  }
  if (!response.headers.get('Content-Type')?.startsWith('text/event-stream') || !response.body)
    throw new Error('事件流格式不正确')
  const reader = response.body.getReader(),
    decoder = new TextDecoder('utf-8', { fatal: true })
  let buffer = '',
    seq = after
  function consume(frame: string) {
    if (frame.includes('event: access_revoked'))
      throw new Error('权限已变化或服务暂不可用，已停止交付')
    const data = frame
      .split('\n')
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trimStart())
      .join('\n')
    if (!data) return
    const value: unknown = JSON.parse(data)
    if (!eventSchema(value) || value.turn_id !== id || value.seq !== seq + 1)
      throw new Error('事件序号不连续，请重新读取当前回合')
    seq = value.seq
    emit(value)
  }
  try {
    for (;;) {
      const { value, done } = await reader.read()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      buffer = buffer.replace(/\r\n/g, '\n')
      let end: number
      while ((end = buffer.indexOf('\n\n')) >= 0) {
        consume(buffer.slice(0, end))
        buffer = buffer.slice(end + 2)
      }
      if (buffer.length > 100000) throw new Error('事件超过大小限制')
      if (done) break
    }
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
export function object(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
export function outcomeSchema(value: unknown): value is TranscriptOutcome {
  return (
    object(value) &&
    typeof value.plan_id === 'string' &&
    !!value.plan_id &&
    (value.kind === 'configuration' || value.kind === 'action') &&
    typeof value.status === 'string' &&
    typeof value.summary === 'string' &&
    !!value.summary.trim() &&
    (value.operation_id === undefined || typeof value.operation_id === 'string')
  )
}
export const toolLabels: Record<string, string> = {
  list_authorized_bots: '机器人状态',
  get_bot_status: '账号状态',
  list_authorized_groups: '授权群组',
  get_group_config: '群配置',
  list_group_operations: '操作记录',
  propose_group_config_change: '配置建议',
  get_group_capabilities: '本群可用能力',
  find_group_members: '选择群成员',
  find_group_messages: '选择撤回消息',
  propose_group_action: '群管动作建议'
}
export function cardSchema(value: unknown): value is Record<string, unknown> {
  if (!object(value) || typeof value.type !== 'string' || !toolLabels[value.type]) return false
  return (
    object(value.scope) &&
    ['overview', 'account', 'group'].includes(String(value.scope.kind)) &&
    typeof value.sampled_at === 'string' &&
    typeof value.verification === 'string' &&
    typeof value.truncated === 'boolean' &&
    (object(value.data) || Array.isArray(value.data))
  )
}
