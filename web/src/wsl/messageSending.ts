export interface TextEvidenceCheck {
  state:
    | 'conversation_not_returned'
    | 'history_empty'
    | 'message_not_found'
    | 'scan_truncated'
    | 'matched'
  durable_records: number
  sequence_heads: number
  conversations: number
  requested: number
  returned: number
  sampled_at: string
}

export interface TextOperation {
  id: string
  status: string
  result_code: string
  kind?: string
  test_window_id?: string
  evidence_check?: TextEvidenceCheck
}

// Project only known states and bounded scalar diagnostics, never upstream prose.
export function textEvidenceDiagnostic(value: unknown) {
  if (!value || typeof value !== 'object') return null
  const check = value as Record<string, unknown>
  let message: string
  switch (check.state) {
    case 'conversation_not_returned':
      message = '本次回显查询未返回目标会话，尚未取得原操作的送达证据。'
      break
    case 'history_empty':
      message = '本次查询返回的目标会话历史为空，尚未取得原操作的送达证据。'
      break
    case 'message_not_found':
      message = '本次有界查询未匹配到原操作的回显，尚未取得送达证据。'
      break
    case 'scan_truncated':
      message = '本次回显查询已达扫描上限，结果不完整，尚未取得原操作的送达证据。'
      break
    case 'matched':
      message = '本次查询匹配到原操作的回显记录，仍未取得对方送达或已读的证据。'
      break
    default:
      return null
  }
  const fields = [
    ['durable_records', '持久记录数', 200],
    ['sequence_heads', '会话水位数', 10000],
    ['conversations', '返回会话数', 2],
    ['requested', '请求记录数', 200],
    ['returned', '返回记录数', 200]
  ] as const
  const counts = fields.flatMap(([key, label, limit]) => {
    const count = check[key]
    return typeof count === 'number' && Number.isSafeInteger(count) && count >= 0 && count <= limit
      ? [{ key, label, value: count }]
      : []
  })
  const sampledAt =
    typeof check.sampled_at === 'string' &&
    check.sampled_at.length <= 40 &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(check.sampled_at) &&
    Number.isFinite(Date.parse(check.sampled_at))
      ? new Date(check.sampled_at).toISOString()
      : ''
  return { message, counts, sampledAt }
}

export interface SendingDraft {
  text: string
  key: string
  submittedText: string
  operation: TextOperation | null
  chiliMode?: 'text' | 'quote' | 'mention'
  chiliQuote?: string
  chiliMention?: string
}

export interface SendingScope {
  account_epoch: number
  binding_epoch: number
  scene?: string
}

export interface SendingWindow {
  id: string
  remaining: number
  actions?: string[]
  binding_epoch?: number
  group_name?: string
  proof_kind?: 'private' | 'group'
}

// Return only version fields; never echo server-only scope metadata in writes.
export function sendingVersions(
  scope: SendingScope | undefined,
  window: SendingWindow | undefined
) {
  return {
    account_epoch: scope?.account_epoch,
    binding_epoch: window?.binding_epoch ?? scope?.binding_epoch
  }
}
