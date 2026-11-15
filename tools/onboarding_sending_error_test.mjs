// Execute the actual composer script with synthetic state and in-memory requests.
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import vm from 'node:vm'
import { stripTypeScriptTypes } from 'node:module'
import * as apiModule from '../web/src/wsl/api.ts'
import * as sendingModule from '../web/src/wsl/messageSending.ts'

const privateText = '<img src=x onerror=alert(1)> SYNTHETIC_PRIVATE_PROVIDER'
const unavailable = apiModule.errorText('native_sending_context_unavailable')
const invalidPrivate = apiModule.errorText('native_private_message_invalid')
const unknown = {
  id: 'synthetic-old-unknown', status: 'unknown', kind: 'send_text',
  result_code: 'native_text_result_unknown', test_window_id: 'synthetic-old-window'
}
const makeDraft = operation => ({
  text: 'synthetic current proof', key: 'synthetic-original-key',
  submittedText: 'synthetic original text', operation
})
const response = (data, msg = '', status = 200) => ({
  ok: status === 200, status,
  json: async () => ({ code: status === 200 ? 0 : status, msg, data })
})
const denied = code => response({ raw: privateText }, code, 409)
const enabled = (recent = []) => ({
  enabled: true, reason: '', max_text_bytes: 3000, recent,
  scope: { account_epoch: 1, binding_epoch: 1 },
  test_windows: [{
    id: 'synthetic-new-proof', remaining: 1, actions: ['send_private_text'],
    proof_kind: 'private'
  }]
})
const deferred = () => {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}
const settle = () => new Promise(resolve => setImmediate(resolve))
globalThis.document = { cookie: '' }
globalThis.fetch = async () => { throw Error('No real requests permitted') }

for (const msg of ['native_private_message_invalid', '', undefined, null,
  { message: privateText }, privateText, '__proto__', 'constructor']) {
  globalThis.fetch = async () => denied(msg)
  await assert.rejects(apiModule.api('/accounts/synthetic/conversations/synthetic/sending'), failure => {
    const known = msg === 'native_private_message_invalid'
    assert.equal(failure.code, known ? msg : 'native_sending_context_unavailable')
    assert.equal(failure.message, known ? invalidPrivate : unavailable)
    assert.equal(apiModule.sendingAdmissionErrorText(failure), failure.message)
    assert.ok(!failure.message.includes(privateText))
    return true
  })
}
for (const cause of [Error(''), Error(privateText), new apiModule.ApiError(409, ''),
  new apiModule.ApiError(409, privateText), { message: privateText }, null]) {
  assert.equal(apiModule.sendingAdmissionErrorText(cause), unavailable)
}
assert.ok(!invalidPrivate.includes('仅支持纯文本'))
assert.equal(apiModule.sendingResultErrorText(privateText, invalidPrivate), invalidPrivate)
assert.equal(apiModule.sendingResultErrorText(privateText),
  apiModule.errorText('native_sending_result_unavailable'))
assert.equal(apiModule.sendingResultErrorText(
  apiModule.errorText('lajiaoliao_evidence_sequences_rejected'), invalidPrivate),
  apiModule.errorText('lajiaoliao_evidence_sequences_rejected'))

const source = await fs.readFile(new URL('../web/src/wsl/MessageComposer.vue', import.meta.url), 'utf8')
const startTag = '<script setup lang="ts">'
const start = source.indexOf(startTag)
const end = source.indexOf('</script>', start)
assert.ok(start >= 0 && end > start)
assert.ok(source.includes('<p v-if="visibleError" class="composer-error" role="alert">{{ visibleError }}</p>'))
const script = stripTypeScriptTypes(source.slice(start + startTag.length, end), { mode: 'transform' }) + `
export const state = {
  scope, draft, context, visibleError, blocked, loading, canSend, pending, selectedWindow,
  retainedUnknowns, independentProof, proofRecoveryWindow, retainedProofID,
  load, sendText, continueDispatch, startIndependentProof, inspect
};
`

async function composer(reply, { operation = null, workspace = 'wsl' } = {}) {
  const session = { user: { id: 'synthetic-actor', tenant_id: 'synthetic-tenant' } }
  const props = {
    account: 'synthetic-account-a', workspace, conversation: 'synthetic-conversation-a',
    scene: 'private', title: 'synthetic target', drafts: new Map()
  }
  const initialScope = `${session.user.id}:${session.user.tenant_id}:${workspace}:${props.account}:${props.conversation}`
  const initialDraft = makeDraft(operation)
  props.drafts.set(initialScope, initialDraft)
  const calls = [], unmounts = []
  let notify, confirmations = 0, confirm = async () => {}
  globalThis.fetch = async (url, options) => {
    const call = {
      url, method: options.method, workspace: options.headers['X-Workspace'], signal: options.signal
    }
    calls.push(call)
    assert.equal(call.method, 'GET', 'Admission failure attempted a mutation')
    return reply(call)
  }
  const context = vm.createContext({
    AbortController, TextEncoder, crypto, defineProps: () => props
  })
  const modules = {
    vue: {
      ref: value => ({ value }),
      computed: read => ({ get value() { return read() } }),
      watch: (_source, callback, options) => {
        notify = callback
        if (options?.immediate) callback()
      },
      onUnmounted: callback => unmounts.push(callback)
    },
    'element-plus': { ElMessageBox: { confirm: () => { confirmations++; return confirm() } } },
    '@element-plus/icons-vue': { Promotion: {}, Refresh: {} },
    './api': apiModule,
    './store': { useSession: () => session },
    './MediaComposer.vue': { default: {} },
    './messageSending': sendingModule
  }
  const component = new vm.SourceTextModule(script, { context })
  await component.link(name => {
    const values = modules[name]
    assert.ok(values, `Unexpected component dependency: ${name}`)
    return new vm.SyntheticModule(Object.keys(values), function () {
      for (const [key, value] of Object.entries(values)) this.setExport(key, value)
    }, { context })
  })
  await component.evaluate()
  await settle()
  return {
    state: component.namespace.state, props, session, calls, initialDraft, notify,
    setReply: value => { reply = value },
    setConfirm: value => { confirm = value },
    confirmations: () => confirmations,
    close: () => { for (const callback of unmounts) callback() }
  }
}

// Current failure stays visible through repeated refreshes without touching the old operation.
{
  const old = structuredClone(unknown)
  const h = await composer(() => denied('native_private_message_invalid'), { operation: old })
  const before = structuredClone(h.initialDraft)
  const identity = [h.props.account, h.props.conversation, h.props.workspace, h.session.user.id]
  assert.equal(h.state.context.value, null)
  assert.equal(h.state.visibleError.value, invalidPrivate)
  assert.equal(h.state.loading.value, false)
  assert.equal(h.state.canSend.value, false)
  await h.state.sendText()
  await h.state.continueDispatch()
  await h.state.load()
  await h.state.load()
  assert.equal(h.confirmations(), 0)
  assert.deepEqual(h.initialDraft, before)
  assert.deepEqual([h.props.account, h.props.conversation, h.props.workspace, h.session.user.id], identity)
  assert.equal(h.state.visibleError.value, invalidPrivate)
  h.setReply(call => call.url.endsWith('/sending')
    ? denied('native_private_message_invalid') : response(structuredClone(old)))
  await h.state.inspect()
  assert.equal(h.state.visibleError.value, invalidPrivate)
  assert.deepEqual(h.initialDraft, before)
  h.setReply(async () => { throw Error(privateText) })
  await h.state.inspect()
  assert.equal(h.state.visibleError.value, invalidPrivate)
  assert.deepEqual(h.initialDraft, before)
  assert.equal(h.calls.length, 5)
  assert.equal(h.calls.filter(call => call.url.endsWith('/sending')).length, 3)
  h.close()
}

// A fresh fixed result diagnosis takes precedence without exposing arbitrary provider text.
{
  const old = structuredClone(unknown)
  const h = await composer(() => denied('native_private_message_invalid'), { operation: old })
  const before = structuredClone(h.initialDraft)
  h.setReply(() => denied('lajiaoliao_evidence_sequences_rejected'))
  await h.state.inspect()
  assert.equal(h.state.visibleError.value,
    apiModule.errorText('lajiaoliao_evidence_sequences_rejected'))
  h.setReply(async () => { throw Error(privateText) })
  await h.state.inspect()
  assert.equal(h.state.visibleError.value, invalidPrivate)
  assert.equal(h.state.canSend.value, false)
  assert.deepEqual(h.initialDraft, before)
  assert.ok(h.calls.every(call => call.method === 'GET'))
  h.close()
}

// An empty provider error is visible even while a separate read-only recovery is pending.
{
  const recovery = deferred()
  const h = await composer(call => call.url.endsWith('/sending') ? denied('') : recovery.promise)
  assert.equal(h.state.context.value, null)
  assert.equal(h.state.visibleError.value, unavailable)
  assert.equal(h.state.canSend.value, false)
  assert.equal(h.calls[0].signal, h.calls[1].signal)
  recovery.resolve(response({ items: [structuredClone(unknown)] }))
  await settle()
  assert.equal(h.state.visibleError.value, unavailable)
  assert.equal(h.state.loading.value, false)
  assert.equal(h.state.draft.value.operation.id, unknown.id)
  assert.equal(h.state.draft.value.key, 'synthetic-original-key')
  await h.state.sendText()
  assert.equal(h.confirmations(), 0)
  h.close()
}

// Empty exceptions and failed recovery never replace the current admission error with raw text.
for (const failure of [Error(''), Error(privateText)]) {
  const h = await composer(async call => {
    if (call.url.endsWith('/sending')) throw failure
    throw Error(privateText)
  })
  assert.equal(h.state.visibleError.value, unavailable)
  assert.equal(h.state.loading.value, false)
  assert.equal(h.state.context.value, null)
  h.close()
}

// An empty recovery response cannot reset an unknown learned while that read was pending.
{
  const recovery = deferred()
  const h = await composer(call => call.url.endsWith('/sending')
    ? denied('native_private_message_invalid') : recovery.promise)
  h.initialDraft.operation = structuredClone(unknown)
  recovery.resolve(response({ items: [] }))
  await settle()
  assert.equal(h.state.draft.value.operation.id, unknown.id)
  assert.equal(h.state.draft.value.operation.status, 'unknown')
  assert.equal(h.state.visibleError.value, invalidPrivate)
  assert.equal(h.state.canSend.value, false)
  h.close()
}

// Scope changes are checked directly, including before Vue's scope watcher runs.
for (const outcome of ['success', 'failure']) {
  const held = deferred()
  const h = await composer(() => held.promise)
  h.props.account = 'synthetic-account-b'
  h.props.conversation = 'synthetic-conversation-b'
  h.session.user.id = 'synthetic-actor-b'
  held.resolve(outcome === 'success' ? response(enabled([unknown])) : denied(privateText))
  await settle()
  assert.equal(h.state.context.value, null)
  assert.equal(h.state.visibleError.value, '')
  assert.equal(h.state.draft.value.operation, null)
  h.setReply(() => denied('native_text_test_window_invalid'))
  h.notify()
  await settle()
  assert.equal(h.state.visibleError.value, apiModule.errorText('native_text_test_window_invalid'))
  assert.equal(h.state.draft.value.operation, null)
  assert.ok(h.calls.slice(1).every(call => call.url.includes('/synthetic-account-b/')))
  h.close()
}

// A superseded controller cannot erase the newer failure or start another recovery.
{
  const first = deferred(), second = deferred()
  let reads = 0
  const h = await composer(call => {
    if (!call.url.endsWith('/sending')) return response({ items: [] })
    return ++reads === 1 ? first.promise : second.promise
  })
  const refreshing = h.state.load()
  second.resolve(denied('native_private_message_invalid'))
  await refreshing
  assert.equal(h.calls[0].signal.aborted, true)
  assert.equal(h.calls[1].signal.aborted, false)
  first.resolve(denied(''))
  await settle()
  assert.equal(h.state.visibleError.value, invalidPrivate)
  assert.equal(h.state.loading.value, false)
  assert.equal(h.calls.length, 3)
  h.close()
}

// A late recovery from the old account must not overwrite the new account's unknown.
{
  const held = deferred()
  const currentUnknown = { ...unknown, id: 'synthetic-current-unknown' }
  const h = await composer(call => call.url.endsWith('/sending')
    ? denied('native_private_message_invalid') : held.promise)
  h.props.account = 'synthetic-account-b'
  h.props.conversation = 'synthetic-conversation-b'
  h.setReply(call => call.url.endsWith('/sending')
    ? denied('native_private_target_unavailable') : response({ items: [currentUnknown] }))
  h.notify()
  await settle()
  held.resolve(response({ items: [unknown] }))
  await settle()
  assert.equal(h.state.draft.value.operation.id, currentUnknown.id)
  assert.equal(h.initialDraft.operation, null)
  assert.equal(h.state.visibleError.value, apiModule.errorText('native_private_target_unavailable'))
  assert.ok(h.calls.at(-1).url.includes('conversation=synthetic-conversation-b'))
  h.close()
}

// Proof expiry during human confirmation blocks stage/dispatch and retains the draft identity.
{
  const h = await composer(() => response(enabled()))
  assert.equal(h.state.canSend.value, true)
  const confirmation = deferred()
  h.setConfirm(() => confirmation.promise)
  const sending = h.state.sendText()
  assert.equal(h.confirmations(), 1)
  h.setReply(call => call.url.endsWith('/sending')
    ? denied('onboarding_controlled_proof_window_invalid') : response({ items: [] }))
  await h.state.load()
  confirmation.resolve()
  await sending
  assert.equal(h.state.canSend.value, false)
  assert.equal(h.state.context.value, null)
  assert.equal(h.state.visibleError.value, apiModule.errorText('onboarding_controlled_proof_window_invalid'))
  assert.equal(h.state.draft.value.operation, null)
  assert.equal(h.state.draft.value.key, 'synthetic-original-key')
  assert.equal(h.state.draft.value.text, 'synthetic current proof')
  assert.ok(h.calls.every(call => call.method === 'GET'))
  h.close()
}

// The explicitly retained older unknown remains visible when the new proof's context fails.
{
  const old = structuredClone(unknown)
  const h = await composer(() => response(enabled([old])), { operation: old, workspace: 'lajiaoliao' })
  assert.ok(h.state.independentProof.value)
  h.state.startIndependentProof()
  assert.equal(h.state.retainedProofID.value, unknown.id)
  h.setReply(call => call.url.endsWith('/sending')
    ? denied('native_private_message_invalid') : response({ items: [] }))
  await h.state.load()
  assert.equal(h.state.context.value, null)
  assert.equal(h.state.retainedUnknowns.value[0].id, unknown.id)
  assert.equal(h.state.retainedUnknowns.value[0].status, 'unknown')
  assert.equal(old.status, 'unknown')
  assert.equal(h.state.canSend.value, false)
  assert.equal(h.state.visibleError.value, invalidPrivate)
  h.close()
}

// A successful read with disabled/expired admission also stays closed and filters unknown prose.
for (const reason of ['onboarding_controlled_proof_window_invalid', privateText, '']) {
  const h = await composer(() => response({ ...enabled(), enabled: false, reason, test_windows: [] }))
  assert.equal(h.state.canSend.value, false)
  assert.equal(h.state.blocked.value, reason === 'onboarding_controlled_proof_window_invalid'
    ? apiModule.errorText(reason) : unavailable)
  await h.state.sendText()
  assert.equal(h.confirmations(), 0)
  assert.ok(h.calls.every(call => call.method === 'GET'))
  h.close()
}

globalThis.fetch = async () => { throw Error('No real requests permitted') }
console.log(JSON.stringify({
  passed: true, bounded_admission_errors: true, actual_composer_script: true,
  unknown_retained: true, expired_proof_no_send: true, scope_and_controller_isolation: true,
  refresh_identity_unchanged: true, no_network: true
}))
