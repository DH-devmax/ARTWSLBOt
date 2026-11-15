import assert from 'node:assert/strict'
import { messageQuotePreview } from '../web/src/wsl/messageQuote.ts'

const original = { id: '1', content: { server_id: '100', message: { client_id: 'video-client', format: 3 } } }
const reply = { id: '2', content: { message: { quote: { client_id: 'video-client', server_id: '100', text: '' } } } }
assert.equal(messageQuotePreview(reply, [reply, original]), '视频')
assert.equal(messageQuotePreview(reply, []), '引用消息 · 原文未加载')
assert.equal(messageQuotePreview(reply, [{ ...original, content: { ...original.content, server_id: '101' } }]), '引用消息 · 原文未加载')
assert.equal(messageQuotePreview(reply, [{ ...original, content: { ...original.content, message: { client_id: 'other', format: 1 } } }]), '引用消息 · 原文未加载')
assert.equal(messageQuotePreview({ id: '3', content: { message: { quote: { text: 'direct text' } } } }, []), 'direct text')
assert.equal(messageQuotePreview(reply, [{ ...original, content: { ...original.content, message: { ...original.content.message, format: 0, text: 'retained text' } } }]), 'retained text')
assert.equal(messageQuotePreview({}, [original]), '')
console.log(JSON.stringify({ passed: true, quote_video: true, exact_ids: true, absent_source: true, no_remote_reads: true }))
