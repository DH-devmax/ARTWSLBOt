interface QuoteMessage {
  id?: string
  content?: {
    server_id?: string
    message?: {
      client_id?: string
      format?: number
      text?: string
      quote?: { client_id?: string; server_id?: string; text?: string }
    }
  }
}

// The caller supplies only the selected account/conversation's loaded records.
// No text matching, remote retrieval or another scope's cache is used.
export function messageQuotePreview(item: QuoteMessage, records: QuoteMessage[]): string {
  const quote = item.content?.message?.quote
  if (!quote) return ''
  if (quote.text?.trim()) return quote.text
  const source = quote.client_id && quote.server_id
    ? records.find((record) => record.id !== item.id &&
      record.content?.server_id === quote.server_id &&
      record.content?.message?.client_id === quote.client_id)
    : undefined
  const message = source?.content?.message
  if (!message) return '引用消息 · 原文未加载'
  if (message.text?.trim()) return message.text.slice(0, 1024)
  return ({ 1: '图片', 2: '语音', 3: '视频', 6: '文件', 9: '聊天记录', 13: '联系人名片' } as Record<number, string>)[
    message.format ?? -1
  ] || '引用消息'
}
