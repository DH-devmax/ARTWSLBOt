import assert from 'node:assert/strict'
import { ApiError, api, errorText } from '../web/src/wsl/api.ts'

const code = 'group_binding_outbound_target_inconsistent'
const privateText = 'synthetic-private-identity'
const base = {
  version: 1, platform: 'wsl', stage: 'friend_info', category: 'response_invalid',
  observed_at: '2026-11-13T08:00:00Z'
}
const labels = {
  friend_info_uid_missing: '好友资料未返回 UID',
  friend_info_uid_null: '好友资料 UID 为 null',
  friend_info_uid_zero: '好友资料 UID 为默认零',
  friend_info_uid_invalid: '好友资料 UID 格式无效',
  friend_info_uid_mismatch: '好友资料 UID 与固定目标不一致',
  friend_info_account_missing: '好友资料未返回 accountId',
  friend_info_account_null: '好友资料 accountId 为 null',
  friend_info_account_zero: '好友资料 accountId 为默认零',
  friend_info_account_invalid: '好友资料 accountId 格式无效',
  friend_info_account_mismatch: '好友资料 accountId 与固定账号不一致'
}

globalThis.fetch = async () => { throw Error('Unexpected request') }
globalThis.document = { cookie: '' }

for (const [reason, label] of Object.entries(labels)) {
  const failure = new ApiError(409, code, {
    ...base, reason, uid: privateText, accountId: privateText, nickName: privateText, raw: privateText
  })
  assert.equal(failure.status, 409)
  assert.equal(failure.code, code)
  assert.equal(failure.lookupDiagnostic.reason, reason)
  assert.equal(failure.message, `${errorText(code)}；${label}`)
  assert.ok(!JSON.stringify(failure).includes(privateText))
}
for (const reason of [privateText, 'friend_info_uid_zero ' + privateText, '__proto__', 'constructor', 'toString']) {
  const failure = new ApiError(409, code, { ...base, reason })
  assert.equal(failure.lookupDiagnostic.reason, undefined)
  assert.equal(failure.message, errorText(code))
}
for (const changed of [{ stage: 'search' }, { category: 'upstream_rejected' }, { platform: 'lajiaoliao' }]) {
  const failure = new ApiError(409, code, { ...base, reason: 'friend_info_uid_zero', ...changed })
  assert.equal(failure.lookupDiagnostic?.reason, undefined)
  assert.equal(failure.message, errorText(code))
}
assert.equal(new ApiError(409, 'group_binding_outbound_rejected',
  { ...base, reason: 'friend_info_uid_zero' }).message, errorText('group_binding_outbound_rejected'))

let requests = 0
globalThis.fetch = async () => {
  requests++
  return {
    ok: false, status: 409,
    json: async () => ({ code: 409, msg: code, data: {
      lookup_diagnostic: { ...base, reason: 'friend_info_account_zero', uid: privateText, raw: privateText }
    } })
  }
}
for (const path of [
  '/group-binding/challenges/synthetic/friend-target',
  '/test-tools/onboarding-friend/challenges/synthetic/friend-target'
]) {
  await assert.rejects(api(path, 'POST', { account: privateText }), failure => {
    assert.equal(failure.code, code)
    assert.equal(failure.lookupDiagnostic.reason, 'friend_info_account_zero')
    assert.ok(failure.message.includes(labels.friend_info_account_zero))
    assert.ok(!JSON.stringify(failure).includes(privateText))
    return true
  })
}
for (const [path, method, workspace] of [
  ['/test-tools/onboarding-friend/challenges/synthetic/friend-send', 'POST', 'wsl'],
  ['/test-tools/onboarding-friend/challenges/synthetic/friend-target', 'GET', 'wsl'],
  ['/test-tools/onboarding-friend/challenges/synthetic/friend-target/extra', 'POST', 'wsl'],
  ['/test-tools/onboarding-friend/challenges/synthetic/friend-target', 'POST', 'lajiaoliao']
]) {
  await assert.rejects(api(path, method, undefined, undefined, workspace), failure => {
    assert.equal(failure.lookupDiagnostic, undefined)
    assert.equal(failure.message, errorText(code))
    return true
  })
}
assert.equal(requests, 6)
console.log(JSON.stringify({ passed: true, identity_reasons: 10, controlled_lookup: true, no_network: true }))
