// Built product assets and synthetic API only; no live sessions or native calls.
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const assets = path.join(root, 'web/dist')
const output = path.join(root, 'output/playwright/native-usage')
await fs.mkdir(output, { recursive: true })
const { chromium } = await import(process.env.WSL_PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ headless: true, channel: 'chrome' })
const parent = (id, name) => ({
  id, name, category: '测试目录', kind: 'action', support: 'confirmed',
  integration: 'partial', role: '仅合成', summary: '合成目录边界', evidence: 'synthetic'
})
const native = [
  parent('account_signup', '新账号注册'), parent('account_login', '密码 / 短信登录'),
  parent('account_settings', '账号资料与隐私设置'), parent('approve_friend', '好友申请审批'),
  parent('read_voice', '语音消息解析与播放'), parent('read_file', '文件消息解析与下载'),
  parent('mute_member', '成员禁言'), parent('send_text', '发送群文本'), parent('send_media', '语音 / 视频发送')
]
const children = [
  ['account_signup', 'signup_sms', '手动申请注册短信'], ['account_signup', 'signup_submit', '提交注册资料'],
  ['account_settings', 'privacy_search', '账号 / 手机号搜索设置'],
  ['account_settings', 'privacy_friend', '好友申请隐私策略'],
  ['account_settings', 'privacy_invitation', '群邀请隐私策略'],
  ['account_settings', 'change_password', '修改账号密码'], ['account_settings', 'change_phone', '换绑手机号'],
  ['approve_friend', 'accept_friend', '人工同意好友申请'], ['approve_friend', 'reject_friend', '人工拒绝好友申请'],
  ['send_media', 'send_voice', '发送群语音'], ['send_media', 'send_video', '发送群视频'],
  ['send_media', 'send_file', '发送群文件']
].map(([parent_id, id, name]) => ({ ...parent(id, name), parent_id }))
const manualIDs = new Set(['account_signup', 'account_login', 'account_settings', 'approve_friend', 'read_voice', 'read_file'])
const usage = Object.fromEntries([
  ...native.map(item => [item.id, manualIDs.has(item.id) ? 'manual' : 'foundation']),
  ...children.map(item => [item.id, item.id === 'send_file' ? 'excluded' : manualIDs.has(item.parent_id) ? 'manual' : 'foundation'])
])
const forbidden = ['手动申请注册短信', '提交注册资料', '密码 / 短信登录', '账号 / 手机号搜索设置',
  '好友申请隐私策略', '群邀请隐私策略', '修改账号密码', '换绑手机号', '人工同意好友申请',
  '人工拒绝好友申请', '语音消息解析与播放', '文件消息解析与下载']
const sample = { verified_at: '2026-11-03T07:00:00Z', scope: '合成移动端',
  result: '合成样本核验', remaining: '本站真实发送仍未验收', evidence: 'synthetic' }
try {
  for (const platform of ['wsl', 'lajiaoliao']) {
    const context = await browser.newContext()
    const page = await context.newPage()
    let viewer = false, writes = 0
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.addInitScript(({ platform }) => {
      sessionStorage.setItem('wsl:workspace:synthetic-actor', platform)
    }, { platform })
    await page.route('**/*', async route => {
      const request = route.request(), url = new URL(request.url())
      if (url.origin !== 'http://127.0.0.1:5173') return route.abort('blockedbyclient')
      if (url.pathname.startsWith('/api/')) {
        if (request.method() !== 'GET') {
          writes++
          return route.abort('blockedbyclient')
        }
        const endpoint = url.pathname.replace('/api/v1', '')
        let data = []
        if (endpoint === '/me') data = {
          user: { id: 'synthetic-actor', role: viewer ? 'user' : 'superadmin' },
          permissions: ['overview:read', 'groups:read', 'accounts:read']
        }
        else if (endpoint === '/workspaces') data = { items: [
          { id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }
        ] }
        else if (endpoint === '/features/catalog') data = {
          platform, items: [], native_capabilities: native, native_details: children,
          native_usage: usage, native_acceptance: {}, native_ready: false,
          native_demonstrations: platform === 'wsl' ? { send_text: sample, send_voice: sample, send_video: sample } : {}
        }
        return route.fulfill({ json: { code: 0, msg: 'ok', data } })
      }
      const target = path.resolve(assets, '.' + (url.pathname === '/' ? '/index.html' : url.pathname))
      if (!target.startsWith(assets + path.sep)) return route.abort('blockedbyclient')
      try {
        return route.fulfill({ body: await fs.readFile(target),
          contentType: { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html',
            '.png': 'image/png', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' }[path.extname(target)] || 'application/octet-stream' })
      } catch { return route.fulfill({ status: 404, body: '' }) }
    })
    await page.goto('http://127.0.0.1:5173/#features')
    const catalog = page.getByTestId('native-capabilities')
    for (const width of [320, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await page.getByRole('tab', { name: '原生功能表', exact: true }).click()
      await catalog.getByRole('button', { name: '查看成员禁言', exact: true }).waitFor()
      for (const name of forbidden) {
        assert.equal(await catalog.getByRole('button', { name: '查看' + name, exact: true }).count(), 0)
      }
      assert.equal(await catalog.getByTestId('native-checklist-count').innerText(), '4')
      assert.equal(await catalog.getByRole('button', { name: '查看发送群文件', exact: true }).count(), 0)
      const sampleRow = catalog.getByRole('row').filter({ hasText: '发送群文本' })
      assert.equal(await sampleRow.getByText('移动端样本已核验', { exact: true }).count(), platform === 'wsl' ? 1 : 0)
      assert.equal(await catalog.getByTestId('native-controlled-count').locator('strong').innerText(), '0')
      assert.equal(await catalog.getByTestId('native-production-count').locator('strong').innerText(), '0')
      if (platform === 'wsl') {
        await sampleRow.getByRole('button', { name: '查看发送群文本', exact: true }).click()
        const sampleDialog = page.getByRole('dialog', { name: '发送群文本', exact: true })
        await sampleDialog.getByText(sample.remaining, { exact: true }).waitFor()
        await sampleDialog.getByRole('button', { name: '关闭详情', exact: true }).click()
        await sampleDialog.waitFor({ state: 'hidden' })
      }
      await page.getByRole('tab', { name: '账号与人工操作', exact: true }).click()
      for (const name of forbidden) {
        await catalog.getByRole('button', { name: '查看' + name, exact: true }).waitFor()
      }
      assert.equal(await catalog.getByTestId('native-checklist-count').count(), 0)
      assert.equal(await catalog.getByRole('columnheader', { name: '关联业务模块' }).count(), 0)
      await catalog.getByRole('button', { name: '查看语音消息解析与播放', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: '语音消息解析与播放', exact: true })
      await dialog.getByText('人工操作 · 操作', { exact: true }).waitFor()
      await dialog.getByRole('button', { name: '关闭详情', exact: true }).click()
      await page.getByRole('dialog').waitFor({ state: 'hidden' })
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
      if (width === 390) {
        await catalog.locator('.native-table-wrap').evaluate(element => { element.scrollLeft = 0 })
        await catalog.locator('.el-table__body-wrapper .el-scrollbar__wrap').evaluate(element => { element.scrollLeft = 0 })
        await page.evaluate(() => scrollTo(0, 0))
        await page.screenshot({ path: path.join(output, platform + '-manual-390.png'), animations: 'disabled' })
      }
    }
    viewer = true
    await page.reload()
    await page.getByRole('tab', { name: '原生功能表', exact: true }).waitFor()
    assert.equal(await page.getByRole('tab', { name: '账号与人工操作', exact: true }).count(), 0)
    assert.equal(writes, 0)
    assert.deepEqual(errors, [])
    console.log(JSON.stringify({ platform, passed: true, manual_items: forbidden.length,
      viewports: [320, 390, 768, 1440], viewer_manual_tab: false, real_native_requests: 0 }))
    await context.close()
  }
} finally {
  await browser.close()
}
