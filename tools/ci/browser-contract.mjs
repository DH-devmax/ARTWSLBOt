import path from 'node:path'

const pageTimeouts = Object.freeze({
  'tools/assistant_ownership_ui_smoke.js': 120,
  'tools/customer_onboarding_ui_smoke.js': 180,
  'tools/friend_delete_ui_smoke.js': 180,
  'tools/onboarding_incoming_ui_smoke.js': 180,
  'tools/web_captcha_ui_smoke.js': 120
})

export function browserCaseSeconds(name) {
  return Object.hasOwn(pageTimeouts, name) ? pageTimeouts[name] : 90
}

export function browserContracts(value) {
  if (!value || Object.keys(value).join() !== 'native_auth_registration' ||
      !['manual_only', 'retired'].includes(value.native_auth_registration)) {
    throw Error('Missing or invalid explicit browser contract')
  }
  return Object.freeze({ native_auth_registration: value.native_auth_registration })
}

export function assetPath(root, rawURL) {
  const url = new URL(rawURL)
  if (url.origin !== 'http://127.0.0.1:5173' || url.pathname.startsWith('/api/')) return null
  const target = path.resolve(root, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname))
  if (!target.startsWith(path.resolve(root) + path.sep)) return null
  return target
}

export function validateCases(expected, actual, sha) {
  if (!/^[a-f0-9]{40}$/.test(sha) || !expected.length ||
      new Set(expected).size !== expected.length ||
      new Set(actual.map(row => row.id)).size !== actual.length) throw Error('Invalid test manifest')
  if (expected.length !== actual.length ||
      expected.some(id => !actual.some(row => row.id === id))) throw Error('Missing browser check')
  if (actual.some(row => !['passed', 'failed'].includes(row.status))) throw Error('Invalid check status')
  return {
    schema: 1, sha, profile: 'browser',
    status: actual.some(row => row.status === 'failed') ? 'failed' : 'passed',
    counts: {
      passed: actual.filter(row => row.status === 'passed').length,
      failed: actual.filter(row => row.status === 'failed').length, skipped: 0
    },
    cases: actual
  }
}

export function progressReport(expected, actual, sha, active = null) {
  if (actual.length === expected.length) return validateCases(expected, actual, sha)
  if (!/^[a-f0-9]{40}$/.test(sha) || !expected.length ||
      new Set(expected).size !== expected.length ||
      new Set(actual.map(row => row.id)).size !== actual.length ||
      actual.some(row => !expected.includes(row.id) || !['passed', 'failed'].includes(row.status)) ||
      (active !== null && (!expected.includes(active) || actual.some(row => row.id === active)))) {
    throw Error('Invalid browser progress')
  }
  return {
    schema: 1, sha, profile: 'browser', status: 'running', active,
    counts: {
      passed: actual.filter(row => row.status === 'passed').length,
      failed: actual.filter(row => row.status === 'failed').length,
      skipped: 0, remaining: expected.length - actual.length
    },
    cases: actual
  }
}

export function cancelledReport(expected, actual, sha, reason) {
  progressReport(expected, actual, sha)
  if (!['SIGINT', 'SIGTERM'].includes(reason?.signal) ||
      (reason.active !== null && !expected.includes(reason.active))) {
    throw Error('Invalid browser cancellation')
  }
  const finished = new Map(actual.map(row => [row.id, row]))
  const cases = expected.map(id => {
    if (id === reason.active) return { ...finished.get(id), id, status: 'failed', code: 'cancelled' }
    return finished.get(id) || { id, status: 'failed', code: 'not_started_after_cancellation' }
  })
  return {
    ...validateCases(expected, cases, sha), status: 'failed',
    cancellation: {
      signal: reason.signal, active: reason.active,
      completed: actual.filter(row => row.id !== reason.active).length,
      cancelled: cases.filter(row => row.code === 'cancelled').length,
      not_started: cases.filter(row => row.code === 'not_started_after_cancellation').length
    }
  }
}

export function createRunCancellation() {
  let reason = null, active = null, notify
  const resources = new Set()
  const whenCancelled = new Promise(resolve => { notify = resolve })
  const close = entry => {
    if (!entry.promise) {
      entry.state = 'closing'
      entry.promise = Promise.resolve().then(entry.dispose).then(() => {
        entry.state = 'closed'
      }, error => {
        entry.state = 'failed'
        entry.error = error
      })
    }
    return entry.promise
  }
  return {
    get reason() { return reason },
    whenCancelled,
    startCase(name) {
      if (reason) return false
      if (active !== null) throw Error('A browser check is already active')
      active = name
      return true
    },
    finishCase(name) {
      if (active !== name) throw Error('Unexpected browser check completion')
      active = null
    },
    cancel(signal) {
      if (!['SIGINT', 'SIGTERM'].includes(signal)) throw Error('Invalid cancellation signal')
      if (reason) return false
      reason = Object.freeze({ signal, active })
      notify(reason)
      for (const entry of resources) void close(entry)
      return true
    },
    track(kind, dispose) {
      const entry = { kind, dispose, state: 'active', promise: null }
      resources.add(entry)
      if (reason) void close(entry)
      return () => close(entry)
    },
    async cleanup() {
      // New contexts can finish opening after cancellation and join this set.
      do {
        await Promise.all([...resources].map(close))
      } while ([...resources].some(entry => entry.state === 'active' || entry.state === 'closing'))
      const entries = [...resources]
      const failures = entries.filter(entry => entry.state === 'failed').map(entry => ({
        resource: entry.kind, ...failureDetails(entry.error, '')
      }))
      return {
        registered: entries.length,
        closed: entries.filter(entry => entry.state === 'closed').length,
        active: entries.filter(entry => entry.state === 'active' || entry.state === 'closing').length,
        failures, complete: failures.length === 0
      }
    }
  }
}

export function failureDetails(error, name) {
  const message = String(error?.message || 'Synthetic check failed').split('\n')[0]
    .replace(/\u001b\[[0-9;]*m/g, '').replace(/[\u0000-\u001f\u007f]/g, '')
    .replace(/https?:\/\/\S+/g, '[url]')
    .replace(/((?:password|token|authorization|secret)\s*[:=]\s*)\S+/gi, '$1[redacted]')
    .slice(0, 240)
  const source = String(error?.stack || '').split('\n').find(line => line.includes(`${name}:`))
  const location = source?.match(/:(\d+):(\d+)\)?$/)
  return { message, ...(location ? { line: Number(location[1]), column: Number(location[2]) } : {}) }
}

export function childEvidence(stdout, name) {
  const parse = text => {
    try { return JSON.parse(text) } catch { return null }
  }
  return [parse(stdout), ...stdout.split('\n').map(parse)].find(value =>
    value && typeof value.passed === 'boolean' && typeof value.check === 'string' &&
    (value.check === path.basename(name) || value.check.startsWith(path.basename(name) + ' ')))
}

export function isolatePage(page, me, onError = () => {}) {
  // APIRequestContext does not participate in Playwright route interception.
  // Replace it instead of ever connecting a synthetic test to a live localhost.
  const get = async input => {
    const url = new URL(input)
    if (url.origin !== 'http://127.0.0.1:5173' || url.pathname !== '/api/v1/me') {
      throw Error('Unregistered direct request')
    }
    return { json: async () => ({ code: 0, data: me }), ok: () => true, status: () => 200 }
  }
  const reject = async () => { throw Error('Direct mutations are forbidden') }
  Object.defineProperty(page, 'request', {
    value: { get, post: reject, put: reject, patch: reject, delete: reject, fetch: reject }
  })
  const originalRoute = page.route.bind(page)
  const originalUnroute = page.unroute.bind(page)
  const wrappers = new WeakMap()
  page.route = async (pattern, handler, options) => {
    let wrapped = wrappers.get(handler)
    if (!wrapped) {
      wrapped = async route => {
        try {
          await handler(new Proxy(route, {
            get(target, key) {
              if (key === 'continue') return options => target.fallback(options)
              if (key === 'fetch') return async () => { throw Error('Direct route fetch is forbidden') }
              const value = target[key]
              return typeof value === 'function' ? value.bind(target) : value
            }
          }))
        } catch (error) {
          onError(error)
          await route.abort('blockedbyclient').catch(() => {})
        }
      }
      wrappers.set(handler, wrapped)
    }
    return originalRoute(pattern, wrapped, options)
  }
  page.unroute = (pattern, handler) =>
    originalUnroute(pattern, handler ? wrappers.get(handler) || handler : undefined)
}
