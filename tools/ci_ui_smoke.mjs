import { chromium } from 'playwright'
import { readFile, mkdir } from 'node:fs/promises'
import { createServer, request } from 'node:http'
import vm from 'node:vm'

const root = '/workspace'
await mkdir('/tmp/output/playwright', { recursive: true })
process.chdir('/tmp')
// Keep browser origin identical to PUBLIC_ORIGIN without host networking.
// The only upstream is the private Compose web service.
const proxy = createServer((incoming, outgoing) => {
  const upstream = request({
    hostname: 'web', port: 8080, method: incoming.method,
    path: incoming.url, headers: incoming.headers
  }, (response) => {
    outgoing.writeHead(response.statusCode, response.headers)
    response.pipe(outgoing)
  })
  upstream.on('error', () => { outgoing.writeHead(502); outgoing.end() })
  incoming.pipe(upstream)
})
await new Promise((resolve) => proxy.listen(5173, '127.0.0.1', resolve))
const env = Object.fromEntries(
  (await readFile(`${root}/.env`, 'utf8'))
    .split('\n')
    .filter((line) => line.includes('=') && !line.startsWith('#'))
    .map((line) => {
      const index = line.indexOf('=')
      return [line.slice(0, index), line.slice(index + 1)]
    })
)
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
})
try {
  const page = await browser.newPage()
  await page.goto('http://127.0.0.1:5173')
  await page.getByRole('textbox', { name: '用户名', exact: true }).fill(env.BOOTSTRAP_USERNAME)
  await page.getByRole('textbox', { name: '密码', exact: true }).fill(env.BOOTSTRAP_PASSWORD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.getByRole('heading', { name: '工作台', exact: true }).waitFor()
  for (const name of ['ui_smoke.js', 'workspaces_ui_smoke.js', 'go_console_live_smoke.js', 'go_console_ui_smoke.js', 'account_removal_ui_smoke.js', 'account_dashboard_ui_smoke.js', 'native_auth_error_ui_smoke.js']) {
    console.log(`Browser check: ${name}`)
    const source = await readFile(`${root}/tools/${name}`, 'utf8')
    const run = vm.runInThisContext(`(${source})`, { filename: name })
    const result = await run(page)
    console.log(JSON.stringify({ check: name, ...result }))
  }
} catch {
  // UI traces can include filled secrets; don't dump browser/source state here.
  console.error('Docker browser acceptance failed; inspect the named check and safe screenshots.')
  process.exitCode = 1
} finally {
  await browser.close()
  proxy.closeAllConnections()
  await new Promise((resolve) => proxy.close(resolve))
}
