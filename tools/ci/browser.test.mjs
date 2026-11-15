import assert from 'node:assert/strict'
import test from 'node:test'
import { assetPath, browserContracts, cancelledReport, childEvidence, createRunCancellation, failureDetails, isolatePage, progressReport, validateCases } from './browser-contract.mjs'

const sha = 'a'.repeat(40)
test('registration requires an explicit fixed browser contract', () => {
  assert.throws(() => browserContracts(undefined))
  assert.throws(() => browserContracts({ native_auth_registration: 'auto' }))
  assert.throws(() => browserContracts({ native_auth_registration: 'manual_only', fallback: true }))
  assert.deepEqual(browserContracts({ native_auth_registration: 'manual_only' }), {
    native_auth_registration: 'manual_only'
  })
  assert.deepEqual(browserContracts({ native_auth_registration: 'retired' }), {
    native_auth_registration: 'retired'
  })
})
test('assets never resolve an API, foreign origin or traversal', () => {
  assert.equal(assetPath('/tmp/assets', 'http://example.com/'), null)
  assert.equal(assetPath('/tmp/assets', 'http://127.0.0.1:5173/api/v1/me'), null)
  assert.equal(assetPath('/tmp/assets', 'http://127.0.0.1:5173/%2e%2e%2fsecret'), null)
  assert.equal(assetPath('/tmp/assets', 'http://127.0.0.1:5173/'), '/tmp/assets/index.html')
})
test('missing, duplicated, stale and skipped browser checks cannot pass', () => {
  assert.throws(() => validateCases(['x'], [], sha))
  assert.throws(() => validateCases(['x'], [{ id: 'x', status: 'passed' }, { id: 'x', status: 'passed' }], sha))
  assert.throws(() => validateCases(['x'], [{ id: 'x', status: 'passed' }], 'old'))
  assert.throws(() => validateCases(['x'], [{ id: 'x', status: 'skipped' }], sha))
})
test('a failed child remains a failure', () => {
  assert.equal(validateCases(['x'], [{ id: 'x', status: 'failed' }], sha).status, 'failed')
})
test('child evidence supports multiline JSON without accepting another case', () => {
  const evidence = { check: 'rules_ui_smoke.js', passed: true }
  assert.deepEqual(childEvidence(JSON.stringify(evidence, null, 2), 'tools/rules_ui_smoke.js'), evidence)
  assert.equal(childEvidence('{"check":"other.js","passed":true}', 'tools/rules_ui_smoke.js'), undefined)
  assert.equal(childEvidence('{"check":"rules_ui_smoke.js","passed":true', 'tools/rules_ui_smoke.js'), undefined)
  assert.equal(childEvidence('{"check":"rules_ui_smoke.js","passed":false}', 'tools/rules_ui_smoke.js').passed, false)
})
test('partial progress cannot claim a passing full run', () => {
  const progress = progressReport(['x', 'y'], [{ id: 'x', status: 'passed' }], sha, 'y')
  assert.equal(progress.status, 'running')
  assert.equal(progress.counts.remaining, 1)
  assert.equal(progress.active, 'y')
  assert.throws(() => progressReport(['x', 'y'], [{ id: 'x', status: 'passed' }], sha, 'x'))
  assert.equal(progressReport(['x'], [{ id: 'x', status: 'failed' }], sha).status, 'failed')
})
test('cancellation stops subsequent checks and cleans each resource exactly once', async () => {
  const run = createRunCancellation()
  const started = [], closed = []
  for (const name of ['current', 'next']) {
    if (!run.startCase(name)) break
    started.push(name)
    const closeContext = run.track('context', async () => { closed.push('context') })
    const closeChild = run.track('child', async () => { closed.push('child') })
    assert.equal(run.cancel('SIGTERM'), true)
    assert.equal(run.cancel('SIGINT'), false)
    assert.deepEqual(await run.whenCancelled, { signal: 'SIGTERM', active: 'current' })
    await Promise.all([closeContext(), closeChild()])
    run.finishCase(name)
  }
  assert.deepEqual(started, ['current'])
  assert.deepEqual(closed.sort(), ['child', 'context'])
  assert.deepEqual(await run.cleanup(), {
    registered: 2, closed: 2, active: 0, failures: [], complete: true
  })
})
test('a context acquired after cancellation is also closed', async () => {
  const run = createRunCancellation()
  let closed = 0
  run.cancel('SIGINT')
  const close = run.track('late context', async () => { closed++ })
  await close()
  assert.equal(run.startCase('never'), false)
  assert.equal(closed, 1)
  assert.equal((await run.cleanup()).complete, true)
})
test('cleanup failures cannot claim a clean cancellation', async () => {
  const run = createRunCancellation()
  run.track('context', async () => { throw Error('Synthetic cleanup failure') })
  run.cancel('SIGTERM')
  assert.deepEqual(await run.cleanup(), {
    registered: 1, closed: 0, active: 0,
    failures: [{ resource: 'context', message: 'Synthetic cleanup failure' }],
    complete: false
  })
})
test('cancelled reports retain failures and terminate every manifest entry', () => {
  const report = cancelledReport(['failed', 'current', 'next'], [
    { id: 'failed', status: 'failed', code: 'assertion_or_timeout' }
  ], sha, { signal: 'SIGINT', active: 'current' })
  assert.equal(report.status, 'failed')
  assert.deepEqual(report.counts, { passed: 0, failed: 3, skipped: 0 })
  assert.deepEqual(report.cases.map(row => row.code), [
    'assertion_or_timeout', 'cancelled', 'not_started_after_cancellation'
  ])
  assert.deepEqual(report.cancellation, {
    signal: 'SIGINT', active: 'current', completed: 1, cancelled: 1, not_started: 1
  })
  const lateSuccess = cancelledReport(['current', 'next'], [
    { id: 'current', status: 'passed', code: 'ok' }
  ], sha, { signal: 'SIGTERM', active: 'current' })
  assert.equal(lateSuccess.cases[0].status, 'failed')
  assert.equal(lateSuccess.cases[0].code, 'cancelled')
  assert.equal(lateSuccess.cases[1].code, 'not_started_after_cancellation')
  assert.throws(() => cancelledReport(['x'], [], sha, { signal: 'SIGKILL', active: 'x' }))
  assert.throws(() => cancelledReport(['x'], [], sha, { signal: 'SIGTERM', active: 'foreign' }))
  assert.throws(() => cancelledReport(['x'], [{ id: 'foreign', status: 'passed' }], sha,
    { signal: 'SIGTERM', active: null }))
})
test('cancellation between cases or during final cleanup never reports success', () => {
  const between = cancelledReport(['done', 'next'], [{ id: 'done', status: 'passed' }], sha,
    { signal: 'SIGTERM', active: null })
  assert.equal(between.status, 'failed')
  assert.equal(between.cases[0].status, 'passed')
  assert.equal(between.cases[1].code, 'not_started_after_cancellation')
  const cleanup = cancelledReport(['done'], [{ id: 'done', status: 'passed' }], sha,
    { signal: 'SIGTERM', active: null })
  assert.equal(cleanup.status, 'failed')
  assert.equal(cleanup.cancellation.not_started, 0)
})
test('failure metadata excludes raw URLs and secrets', () => {
  const error = new Error('token=synthetic-private https://example.invalid/private\nCall log: private')
  error.stack = 'Error\n at run (tools/example_ui_smoke.js:42:9)'
  assert.deepEqual(failureDetails(error, 'tools/example_ui_smoke.js'), {
    message: 'token=[redacted] [url]', line: 42, column: 9
  })
})
test('direct API requests cannot reach the real local service', async () => {
  const page = { route: async () => {}, unroute: async () => {} }
  isolatePage(page, { user: { id: 'synthetic' } })
  assert.equal((await (await page.request.get('http://127.0.0.1:5173/api/v1/me')).json()).data.user.id, 'synthetic')
  await assert.rejects(page.request.get('http://127.0.0.1:5173/api/v1/accounts'))
  await assert.rejects(page.request.post('http://127.0.0.1:5173/api/v1/me'))
  await assert.rejects(page.request.get('https://example.com/'))
})
test('fixture continue falls back into the synthetic deny handler', async () => {
  let wrapped, removed, fallback = 0
  const page = {
    route: async (pattern, handler) => { wrapped = handler },
    unroute: async (pattern, handler) => { removed = handler }
  }
  isolatePage(page, {})
  const handler = route => route.continue()
  await page.route('**/*', handler)
  await wrapped({ continue: () => { throw Error('Network attempted') }, fallback: () => { fallback++ } })
  assert.equal(fallback, 1)
  await page.unroute('**/*', handler)
  assert.equal(removed, wrapped)
})
test('fixture route errors are captured and direct fetch is denied', async () => {
  let wrapped, aborted = 0
  const errors = []
  const page = { route: async (pattern, handler) => { wrapped = handler }, unroute: async () => {} }
  isolatePage(page, {}, error => errors.push(error))
  await page.route('**/*', route => route.fetch())
  await wrapped({
    fetch: () => { throw Error('Network attempted') },
    abort: async () => { aborted++ }
  })
  assert.equal(errors[0].message, 'Direct route fetch is forbidden')
  assert.equal(aborted, 1)
})
