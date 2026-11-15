// Built product assets and synthetic fixtures only. No live API proxy exists.
import fs from 'node:fs/promises'
import path from 'node:path'
import vm from 'node:vm'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { execFileSync, spawn } from 'node:child_process'
import { assetPath, browserContracts, cancelledReport, childEvidence, createRunCancellation, failureDetails, isolatePage, progressReport, validateCases } from './browser-contract.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
process.chdir(root)
const registry = JSON.parse(await fs.readFile(path.join(root, 'tools/ci/registry.json'), 'utf8'))
const contracts = browserContracts(registry.browser_contracts)
execFileSync('python3', ['tools/ci/registry.py'], { stdio: 'pipe' })
const require = createRequire(new URL('./browser/package.json', import.meta.url))
const playwrightModule = process.env.WSL_PLAYWRIGHT_MODULE ||
  path.join(path.dirname(require.resolve('playwright')), 'index.mjs')
const playwright = await import(playwrightModule)
const { chromium } = playwright.default || playwright
const sha = execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
const output = path.resolve(process.env.CI_BROWSER_OUTPUT || 'output/ci/browser/synthetic')
const assets = path.resolve(process.env.WSL_UI_ASSET_ROOT || 'web/dist')
const all = [...registry.browser, ...registry.browser_component]
const only = process.argv.slice(2)
if (only.length && process.env.GITHUB_ACTIONS === 'true') throw Error('Partial browser CI is forbidden')
if (only.some(name => !all.includes(name))) throw Error('Unregistered browser check')
const expected = only.length ? only : all
await fs.access(path.join(assets, 'index.html'))
await fs.mkdir(output, { recursive: true })
await fs.mkdir('output/playwright', { recursive: true })
const cases = []
const cancellation = createRunCancellation()
const signalHandlers = new Map(['SIGINT', 'SIGTERM'].map(signal => [
  signal, () => cancellation.cancel(signal)
]))
for (const [signal, handler] of signalHandlers) process.on(signal, handler)
async function writeReport(report) {
  for (const file of ['report.json', 'progress.json']) {
    const target = path.join(output, file)
    await fs.writeFile(target + '.tmp', JSON.stringify(report, null, 2) + '\n')
    await fs.rename(target + '.tmp', target)
  }
}
async function writeProgress(active = null) {
  await writeReport({ ...progressReport(expected, cases, sha, active), contracts })
}
function assertRunning() {
  if (cancellation.reason) throw Error('Synthetic run cancelled')
}
let browser, runnerFailure, cleanup
const fixtureMe = {
  user: { id: 'ae000000-0000-4000-8000-000000000001', role: 'superadmin' },
  permissions: ['overview:read', 'accounts:read', 'groups:read', 'operations:read', 'workspaces:maibot']
}

function componentCheck(name) {
  return new Promise((resolve) => {
    if (cancellation.reason) return resolve({ status: 'failed', code: 'cancelled' })
    const child = spawn(process.execPath, [name, ...(registry.component_arguments[name] || [])], {
      cwd: root, detached: true,
      env: {
        ...process.env,
        WSL_UI_NODE_MODULES: path.join(root, 'web/node_modules'),
        WSL_PLAYWRIGHT_MODULE: playwrightModule,
        WSL_UI_ASSET_ROOT: assets,
        WSL_UI_SCREENSHOT_DIR: path.join(root, 'output/playwright'),
        WSL_BROWSER_CHANNEL: 'chromium',
        PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || chromium.executablePath()
      },
      stdio: ['ignore', 'pipe', 'pipe']
    })
    let stdout = '', stderr = '', pendingLine = '', lastStage = '', invalid = false, code = 'child_failed'
    let exited = false, notifyClosed
    const closed = new Promise(resolve => { notifyClosed = resolve })
    const kill = (signal = 'SIGKILL') => {
      if (!child.pid) return
      try { process.kill(-child.pid, signal) } catch (error) {
        if (error.code !== 'ESRCH') throw error
      }
    }
    const closeChild = cancellation.track('child', async () => {
      if (exited) return
      // Playwright handles SIGTERM by closing its Chromium before exiting.
      kill('SIGTERM')
      const force = setTimeout(() => kill(), 3000)
      try { await closed } finally { clearTimeout(force) }
    })
    const timer = setTimeout(() => { invalid = true; code = 'child_timeout'; kill() }, 180000)
    child.stdout.on('data', data => {
      if (stdout.length + data.length > 1024 * 1024) { invalid = true; code = 'child_output_limit'; kill() } else stdout += data
      const lines = (pendingLine + data).split('\n')
      pendingLine = lines.pop()
      for (const line of lines) {
        let value
        try { value = JSON.parse(line) } catch { continue }
        if (value.check === path.basename(name) && /^[A-Za-z0-9/_-]{1,160}$/.test(value.stage || '')) {
          lastStage = value.stage
          console.log(JSON.stringify({ id: name, stage: lastStage }))
        }
      }
    })
    child.stderr.on('data', data => { if (stderr.length < 16384) stderr += data.toString().slice(0, 16384 - stderr.length) })
    child.on('error', () => { invalid = true; code = 'child_start_failed' })
    child.on('close', exit => {
      exited = true
      notifyClosed()
      void closeChild()
      clearTimeout(timer)
      const evidence = childEvidence(stdout, name)
      const passed = !cancellation.reason && !invalid && exit === 0 && evidence?.passed === true
      const failure = stderr.split('\n').find(line =>
        /^(?:Error|TimeoutError|AssertionError)(?: \[[A-Z_]+\])?:/.test(line))
      const layouts = Array.isArray(evidence?.candidate_layout_failures)
        ? evidence.candidate_layout_failures.slice(0, 16).map(value => ({
          platform: ['wsl', 'lajiaoliao'].includes(value.platform) ? value.platform : 'invalid',
          count: value.count, width: value.width, rect: value.rect,
          list_height: value.list_height, content_height: value.content_height
        })) : []
      resolve({ status: passed ? 'passed' : 'failed',
        code: cancellation.reason ? 'cancelled' : passed ? 'ok' : invalid || exit !== 0 ? code : 'missing_child_evidence',
        ...(!passed && lastStage ? { last_stage: lastStage } : {}),
        ...(layouts.length ? { layout_failures: layouts } : {}),
        ...(!passed && failure ? { failure: failureDetails(new Error(failure), name) } : {}) })
    })
  })
}

async function pageCheck(name) {
  let context, closeContext, page, timer
  const errors = [], fixtureErrors = []
  try {
    assertRunning()
    context = await browser.newContext({ serviceWorkers: 'block' })
    closeContext = cancellation.track('context', () => context.close())
    assertRunning()
    page = await context.newPage()
    assertRunning()
    isolatePage(page, fixtureMe, error => fixtureErrors.push(error))
    page.setDefaultTimeout(8000)
    page.on('pageerror', error => errors.push(error))
    // Context routing also denies popups and pages created by a fixture.
    await context.route('**/*', async route => {
      const url = new URL(route.request().url())
      if (url.origin !== 'http://127.0.0.1:5173') return route.abort('blockedbyclient')
      if (url.pathname.startsWith('/api/')) return route.fulfill({
        status: 401, json: { code: 1, msg: 'synthetic_unmocked_api' }
      })
      let target
      try { target = assetPath(assets, url.href) } catch {}
      if (!target) return route.abort('blockedbyclient')
      try {
        await route.fulfill({
          body: await fs.readFile(target),
          contentType: {
            '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html',
            '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2'
          }[path.extname(target)] || 'application/octet-stream'
        })
      } catch { await route.fulfill({ status: 404, body: 'missing_asset' }) }
    })
    assertRunning()
    const task = (async () => {
      await page.goto('http://127.0.0.1:5173')
      assertRunning()
      const source = await fs.readFile(path.join(root, name), 'utf8')
      assertRunning()
      const run = vm.runInThisContext(
        `((console) => (${source}))({log() {}, warn() {}, error() {}})`, { filename: name })
      if (typeof run !== 'function') throw Error('Invalid registered browser function')
      const value = await run(page, fixtureMe, contracts)
      if (fixtureErrors.length) throw fixtureErrors[0]
      if (errors.length) throw errors[0]
      if (!value || value.passed === false) throw Error('Synthetic check failed')
    })()
    const seconds = name === 'tools/onboarding_incoming_ui_smoke.js' ? 180 : 90
    await Promise.race([task, new Promise((resolve, reject) => {
      timer = setTimeout(() => reject(Error(`Synthetic case exceeded ${seconds} seconds`)), seconds * 1000)
    }), cancellation.whenCancelled.then(() => { throw Error('Synthetic run cancelled') })])
    assertRunning()
    return { status: 'passed', code: 'ok' }
  } catch (error) {
    const result = { status: 'failed',
      code: cancellation.reason ? 'cancelled' : fixtureErrors.length ? 'fixture_error'
        : errors.length ? 'browser_runtime_error' : 'assertion_or_timeout',
      failure: failureDetails(fixtureErrors[0] || errors[0] || error, name) }
    // Screenshots contain synthetic fixtures only; no traces, HTML or cookies.
    if (page && !cancellation.reason) {
      await page.screenshot({ path: `output/playwright/${path.basename(name)}-failure.png`, fullPage: true }).catch(() => {})
    }
    return result
  } finally {
    clearTimeout(timer)
    if (closeContext) await closeContext()
  }
}

try {
  await writeProgress()
  assertRunning()
  browser = await chromium.launch({
    headless: true,
    handleSIGINT: false, handleSIGTERM: false,
    ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {})
  })
  for (const name of expected) {
    if (!cancellation.startCase(name)) break
    const started = performance.now()
    console.log(`Synthetic browser check: ${name}`)
    await writeProgress(name)
    const result = cancellation.reason ? { status: 'failed', code: 'cancelled' }
      : registry.browser_component.includes(name) ? await componentCheck(name) : await pageCheck(name)
    cases.push({ id: name, ...result, duration_seconds: Math.round(performance.now() - started) / 1000 })
    cancellation.finishCase(name)
    if (cancellation.reason) break
    await writeProgress()
    console.log(JSON.stringify({ id: name, status: result.status, code: result.code }))
  }
} catch (error) {
  if (!cancellation.reason) runnerFailure = failureDetails(error, 'tools/ci/browser.mjs')
} finally {
  cleanup = await cancellation.cleanup()
  let browserClosed = !browser
  try {
    if (browser) await browser.close()
    browserClosed = true
  } catch (error) {
    cleanup.failures.push({ resource: 'browser', ...failureDetails(error, '') })
  }
  cleanup = { ...cleanup, browser_started: Boolean(browser), browser_closed: browserClosed,
    complete: cleanup.complete && browserClosed }
}
if (runnerFailure) {
  for (const id of expected) {
    if (!cases.some(row => row.id === id)) {
      cases.push({ id, status: 'failed', code: 'not_completed_after_runner_failure' })
    }
  }
}
let result
do {
  result = {
    ...(cancellation.reason ? cancelledReport(expected, cases, sha, cancellation.reason)
      : validateCases(expected, cases, sha)),
    contracts, cleanup,
    ...(runnerFailure ? { status: 'failed', runner_failure: runnerFailure } : {}),
    ...(!cleanup.complete ? { status: 'failed' } : {})
  }
  await writeReport(result)
} while (result.cancellation?.signal !== cancellation.reason?.signal)
for (const [signal, handler] of signalHandlers) process.removeListener(signal, handler)
console.log(JSON.stringify({ profile: 'browser', status: result.status, counts: result.counts,
  ...(result.cancellation ? { cancellation: result.cancellation } : {}), cleanup }))
process.exitCode = cancellation.reason ? cancellation.reason.signal === 'SIGINT' ? 130 : 143
  : result.status === 'passed' ? 0 : 1
