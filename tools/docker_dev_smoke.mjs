// Disposable frontend and synthetic API only; never join the live Compose network.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import vm from 'node:vm'
import { fileURLToPath } from 'node:url'
import { randomBytes } from 'node:crypto'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const docker = (...args) => execFileSync('docker', args, {
  encoding: 'utf8', timeout: 180000, stdio: ['ignore', 'pipe', 'pipe']
}).trim()
const image = 'wsl-web:dev'
const name = `wsl-dev-smoke-${randomBytes(6).toString('hex')}`
const subnet = process.env.WSL_DEV_SMOKE_SUBNET || '10.249.28.0/28'
assert.match(subnet, /^10\.249\.(?:[0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.0\/28$/)
const containers = []
let networkCreated = false, browser
const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'wsl-dev-smoke-'))
const fixture = path.join(temporary, 'style.css')
const appFixture = path.join(temporary, 'App.vue')
const api = `
import { createServer } from 'node:http';
createServer((req, res) => {
  if (req.url === '/healthz') {
    res.setHeader('Content-Type', 'application/json');
    return res.end(JSON.stringify({code:0,data:{status:'ok',native_ready:false,ai_ready:false}}));
  }
  if (req.url === '/api/v1/system/monitor/events') {
    res.writeHead(200, {'Content-Type':'text/event-stream','Cache-Control':'no-cache'});
    res.write('event: update\\ndata: {"synthetic":true}\\n\\n');
    return req.on('close', () => res.end());
  }
  if (req.url === '/api/v1/dev-smoke/echo') {
    res.setHeader('Content-Type', 'application/json');
    return res.end(JSON.stringify({
      host:req.headers.host,origin:req.headers.origin,csrf:req.headers['x-csrf-token'],
      cookie_seen:(req.headers.cookie || '').includes('wsl_dev_smoke=synthetic')
    }));
  }
  res.writeHead(401, {'Content-Type':'application/json'});
  res.end(JSON.stringify({code:401,msg:'authentication_required',data:null}));
}).listen(8080, '0.0.0.0');
`
try {
  docker('image', 'inspect', image, '--format', '{{.Id}}')
  await fs.writeFile(fixture, await fs.readFile(path.join(root, 'web/src/wsl/style.css'), 'utf8') +
    '\n:root { --wsl-hmr-probe: before; }\n')
  const appSource = await fs.readFile(path.join(root, 'web/src/wsl/App.vue'), 'utf8')
  assert.match(appSource, /<p class="muted">[^<]*<\/p>/)
  const appBefore = appSource.replace(/<p class="muted">([^<]*)<\/p>/,
    '<p class="muted" data-dev-hmr="before">$1</p>')
  await fs.writeFile(appFixture, appBefore)
  docker('network', 'create', '--subnet', subnet, '--label', `wsl.dev-smoke=${name}`, name)
  networkCreated = true
  const common = ['--network', name, '--read-only', '--cap-drop', 'ALL',
    '--security-opt', 'no-new-privileges:true', '--label', `wsl.dev-smoke=${name}`]
  containers.push(docker('run', '-d', '--name', `${name}-api`, ...common,
    '--network-alias', 'api', '--entrypoint', 'node', '--memory', '96m',
    image, '--input-type=module', '-e', api))
  const web = docker('run', '-d', '--name', `${name}-web`, ...common,
    '--memory', '512m', '--memory-swap', '512m', '--cpus', '0.75',
    '--env', 'NODE_OPTIONS=--max-old-space-size=384', '--env', 'WSL_DEV_PORT=0',
    '--env', 'NODE_ENV=development', '--env', 'GOMEMLIMIT=128MiB', '--env', 'GOMAXPROCS=2',
    '--mount', `type=bind,source=${fixture},target=/workspace/web/src/wsl/style.css,readonly`,
    '--mount', `type=bind,source=${appFixture},target=/workspace/web/src/wsl/App.vue,readonly`,
    '--mount', 'type=volume,target=/home/node/.cache/wsl-vite',
    '--tmpfs', '/tmp:rw,size=64m,noexec,nosuid',
    '-p', '127.0.0.1::8080', image)
  containers.push(web)
  let binding = ''
  for (let attempt = 0; attempt < 30 && !binding; attempt++) {
    try { binding = docker('port', web, '8080/tcp') } catch {}
    if (!binding) await new Promise(resolve => setTimeout(resolve, 200))
  }
  assert.match(binding, /^127\.0\.0\.1:\d+$/)
  const base = `http://${binding}`
  const deadline = Date.now() + 90000
  for (;;) {
    try {
      const response = await fetch(base, { signal: AbortSignal.timeout(2000) })
      if (response.ok) break
    } catch {}
    if (Date.now() >= deadline) throw new Error('Development frontend startup timed out')
    await new Promise(resolve => setTimeout(resolve, 500))
  }
  const { chromium } = await import(process.env.WSL_PLAYWRIGHT_MODULE || 'playwright')
  browser = await chromium.launch({ headless: true, channel: 'chrome' })
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/*', route => {
    if (new URL(route.request().url()).origin !== base) return route.abort('blockedbyclient')
    return route.continue()
  })
  await page.goto(`${base}/#overview`)
  const input = page.getByRole('textbox', { name: '用户名', exact: true })
  await input.waitFor({ timeout: 90000 })
  await input.fill('synthetic-unsent-draft')
  await page.waitForFunction(() => getComputedStyle(document.documentElement)
    .getPropertyValue('--wsl-hmr-probe').trim() === 'before')
  await page.evaluate(() => {
    window.__wslHmrProbe = 'same-page'
    document.cookie = 'wsl_dev_smoke=synthetic; SameSite=Lax'
  })
  const echo = await page.evaluate(async () => (await fetch('/api/v1/dev-smoke/echo', {
    method: 'POST', headers: { 'X-CSRF-Token': 'synthetic-csrf' }, body: '{}'
  })).json())
  assert.equal(echo.host, binding)
  assert.equal(echo.origin, base)
  assert.equal(echo.csrf, 'synthetic-csrf')
  assert.equal(echo.cookie_seen, true)
  assert.equal(await page.evaluate(() => new Promise((resolve, reject) => {
    const stream = new EventSource('/api/v1/system/monitor/events')
    const timer = setTimeout(() => { stream.close(); reject(new Error('SSE timeout')) }, 5000)
    stream.addEventListener('update', event => {
      clearTimeout(timer); stream.close(); resolve(JSON.parse(event.data).synthetic)
    })
    stream.onerror = () => { clearTimeout(timer); stream.close(); reject(new Error('SSE failed')) }
  })), true)
  await fs.appendFile(fixture, '\n:root { --wsl-hmr-probe: after; }\n')
  await page.waitForFunction(() => getComputedStyle(document.documentElement)
    .getPropertyValue('--wsl-hmr-probe').trim() === 'after', undefined, { timeout: 15000 })
  assert.equal(await input.inputValue(), 'synthetic-unsent-draft')
  assert.equal(await page.evaluate(() => window.__wslHmrProbe), 'same-page')
  await page.locator('[data-dev-hmr="before"]').waitFor()
  await fs.writeFile(appFixture, appBefore.replace('data-dev-hmr="before"', 'data-dev-hmr="after"'))
  await page.locator('[data-dev-hmr="after"]').waitFor({ timeout: 15000 })
  assert.equal(await input.inputValue(), 'synthetic-unsent-draft')
  assert.equal(await page.evaluate(() => window.__wslHmrProbe), 'same-page')
  for (const endpoint of ['/.env', '/.env.local', '/.git/config', '/test.pem',
    '/@fs/etc/passwd', '/src/main.ts']) {
    const response = await page.request.get(base + endpoint)
    assert.ok([403, 404].includes(response.status()), `Unexpected development file exposure: ${endpoint}`)
  }
  const health = await (await page.request.get(`${base}/healthz`)).json()
  assert.equal(health.data.native_ready, false)
  assert.equal(health.data.ai_ready, false)
  await fs.mkdir(path.join(root, 'output/playwright'), { recursive: true })
  for (const width of [320, 390, 647, 1440]) {
    await page.setViewportSize({ width, height: 844 })
    await page.screenshot({ path: path.join(root, `output/playwright/docker-dev-${width}.png`) })
  }
  const productChecks = []
  for (const name of ['native_members_ui_smoke.js', 'message_sending_ui_smoke.js', 'messages_ui_smoke.js']) {
    const source = await fs.readFile(path.join(root, 'tools', name), 'utf8')
    const origin = "const base = 'http://127.0.0.1:5173'"
    assert.equal(source.split(origin).length, 2, `Missing unique synthetic origin: ${name}`)
    const run = vm.runInThisContext(`(${source.replace(origin, `const base = ${JSON.stringify(base)}`)})`, {
      filename: name
    })
    const result = await run(page)
    assert.equal(result.passed, true, name)
    if (name === 'messages_ui_smoke.js') assert.equal(result.read_only, true, name)
    else assert.equal(result.native_actions, 0, name)
    productChecks.push(name)
    console.log(JSON.stringify({ check: name, ...result, serving: 'docker-vite' }))
  }
  assert.deepEqual(errors, [])
  assert.equal(docker('inspect', web, '--format', '{{.State.OOMKilled}}'), 'false')
  let frontendPeakBytes = null
  try {
    frontendPeakBytes = Number(docker('exec', web, 'node', '-e',
      "process.stdout.write(require('node:fs').readFileSync('/sys/fs/cgroup/memory.peak','utf8'))"))
  } catch {}
  console.log(JSON.stringify({ passed: true, synthetic_only: true, hmr_without_reload: true,
    vue_template_hmr_without_reload: true,
    proxy_origin_cookie_csrf: true, sse: true, product_only: true, native_actions: 0,
    product_checks: productChecks,
    frontend_limit_mib: 512, frontend_peak_bytes: frontendPeakBytes,
    viewports: [320, 390, 647, 1440] }))
} catch (error) {
  for (const id of containers) {
    try { console.error(docker('logs', '--tail', '25', id)) } catch {}
  }
  throw error
} finally {
  await browser?.close()
  for (const id of containers.reverse()) {
    try { docker('rm', '-f', '-v', id) } catch {}
  }
  if (networkCreated) {
    try { docker('network', 'rm', name) } catch {}
  }
  await fs.rm(temporary, { recursive: true, force: true })
}
