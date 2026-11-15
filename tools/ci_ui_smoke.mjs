// A fresh wsl_test website session only; detailed regressions use the CI registry.
import { chromium } from 'playwright'
import { readFile, stat } from 'node:fs/promises'
import { createServer, request } from 'node:http'

const root = '/workspace'
if (process.env.CI !== 'true') throw Error('Isolated CI stack required')
const sessionFile = `${root}/var/ci-website-session.json`
if (((await stat(sessionFile)).mode & 0o777) !== 0o600) throw Error('Private CI session required')
const session = JSON.parse(await readFile(sessionFile, 'utf8'))
if (session.username !== 'synthetic-smoke-admin' ||
    !/^[A-Za-z0-9_-]{43}$/.test(session.session_token) ||
    !/^[A-Za-z0-9_-]{43}$/.test(session.csrf_token) ||
    Date.parse(session.expires_at) <= Date.now()) throw Error('Invalid synthetic CI session')
const proxy = createServer((incoming, outgoing) => {
  // This check reads website state. It never forwards a mutation.
  if (!['GET', 'HEAD'].includes(incoming.method)) {
    outgoing.writeHead(405)
    return outgoing.end()
  }
  const upstream = request({
    hostname: 'web', port: 8080, method: incoming.method, path: incoming.url,
    headers: incoming.headers
  }, response => {
    outgoing.writeHead(response.statusCode, response.headers)
    response.pipe(outgoing)
  })
  upstream.on('error', () => { outgoing.writeHead(502); outgoing.end() })
  incoming.pipe(upstream)
})
await new Promise(resolve => proxy.listen(5173, '127.0.0.1', resolve))
const browser = await chromium.launch({
  headless: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
})
try {
  const page = await browser.newPage()
  await page.context().addCookies([
    { name: 'wsl_session', value: session.session_token, url: 'http://127.0.0.1:5173', httpOnly: true, sameSite: 'Strict' },
    { name: 'wsl_csrf', value: session.csrf_token, url: 'http://127.0.0.1:5173', sameSite: 'Strict' }
  ])
  const response = await page.request.get('http://127.0.0.1:5173/api/v1/me')
  const principal = (await response.json()).data?.user
  if (!response.ok() || principal?.id !== session.user_id ||
      principal?.role !== 'superadmin' || principal?.username !== session.username) {
    throw Error('Synthetic session was not authenticated')
  }
  await page.goto('http://127.0.0.1:5173')
  await page.getByRole('heading', { name: '工作台', exact: true }).waitFor()
  console.log(JSON.stringify({
    check: 'authenticated_container_ui', passed: true, database: 'wsl_test',
    synthetic_only: true, native_actions: 0, model_calls: 0
  }))
} catch {
  console.error('Isolated authenticated browser check failed.')
  process.exitCode = 1
} finally {
  await browser.close()
  proxy.closeAllConnections()
  await new Promise(resolve => proxy.close(resolve))
}
