// All deletion APIs are synthetic. Never remove an existing local/native account.
// prettier-ignore
async (page) => {
  const base = 'http://127.0.0.1:5173'
  if (new URL(page.url()).origin !== base) throw new Error('Local console only')
  const realMe = (await (await page.request.get(base + '/api/v1/me')).json()).data
  const original = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:'))))
  const id = (n) => `40000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const group = id(9), owner = realMe.user.id
  const accounts = [
    { id: id(1), name: '待删除机器人', workspace: 'wsl', version: 1 },
    { id: id(2), name: '保留机器人', workspace: 'wsl', version: 1 },
    { id: id(3), name: '独立热群机器人', workspace: 'maibot', version: 1 }
  ].map((a) => ({ ...a, auth_state: 'not_authenticated', runtime_state: 'disconnected' }))
  let conflict = true, blocked = false, tenant = false
  let delayPreview = false, releasePreview, previewStarted
  const previewEntered = new Promise((resolve) => { previewStarted = resolve })
  const previewGate = new Promise((resolve) => { releasePreview = resolve })
  const writes = []
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    const workspace = req.headers()['x-workspace'] || 'wsl', method = req.method()
    const preview = path.match(/^\/accounts\/([^/]+)\/removal-preview$/)
    const removal = path.match(/^\/accounts\/([^/]+)$/)
    let data = []
    if (method === 'GET') {
      if (path === '/me') data = tenant ? { user: { ...realMe.user, role: 'user', tenant_id: id(8) }, permissions: ['overview:read', 'accounts:read', 'groups:read'] } : realMe
      else if (path === '/accounts') data = accounts.filter((a) => a.workspace === workspace)
      else if (path === '/overview') data = { accounts: accounts.filter((a) => a.workspace === workspace).length, limits: { accounts: 10, groups: 100, ai_groups: 100 } }
      else if (preview) {
        const account = accounts.find((a) => a.id === preview[1] && a.workspace === workspace)
        if (!account) throw new Error('Preview scope mismatch')
        data = { ...account, version: conflict ? 1 : 2,
          cleanup: { messages: 21, memberships: 3, management_groups: workspace === 'wsl' ? 1 : 0, shared_test_windows: 1 },
          retained: { group_ownership: 1, ledger_entries: 2, activities: 1 },
          blockers: blocked ? ['account_removal_operations_pending'] : [],
          management_group_ids: workspace === 'wsl' ? [group] : [] }
        if (delayPreview && account.id === id(2)) {
          previewStarted()
          await previewGate
        }
      }
    } else if (method === 'DELETE' && removal) {
      const account = accounts.find((a) => a.id === removal[1] && a.workspace === workspace)
      const body = req.postDataJSON()
      if (!account || body.confirm_name !== account.name) throw new Error('Deletion scope/name mismatch')
      writes.push({ id: account.id, workspace, body })
      if (conflict) {
        conflict = false
        await route.fulfill({ status: 409, json: { code: 1, msg: 'version_conflict' } })
        return
      }
      data = { ...account, management_group_ids: workspace === 'wsl' ? [group] : [] }
      accounts.splice(accounts.indexOf(account), 1)
    } else {
      await route.abort('blockedbyclient')
      throw new Error(`Unexpected write: ${method} ${path}`)
    }
    await route.fulfill({ json: { code: 0, data, msg: '' } })
  }
  const dialog = page.getByRole('dialog', { name: '删除机器人', exact: true })
  const open = async (name) => {
    await page.getByRole('button', { name: `删除机器人 ${name}`, exact: true }).click()
    await dialog.getByLabel('输入机器人名称确认删除', { exact: true }).waitFor()
  }
  await page.route(pattern, handler)
  try {
    await page.evaluate(({ owner, target, keep, chat, group }) => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      sessionStorage.setItem(`wsl:workspace:${owner}`, 'wsl')
      sessionStorage.setItem(`wsl:account:${owner}:wsl`, target)
      sessionStorage.setItem(`wsl:account:${owner}:maibot`, chat)
      sessionStorage.setItem(`wsl:drafts:${owner}`, JSON.stringify({
        [`wsl:platform:${target}:${group}`]: { private: 'delete' },
        [`wsl:platform:${keep}:safe`]: { private: 'keep' },
        [`maibot:settings:${chat}:models`]: { private: 'chat' }
      }))
    }, { owner, target: id(1), keep: id(2), chat: id(3), group })
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
    await open('待删除机器人')
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await dialog.waitFor({ state: 'hidden' })
    if (writes.length) throw new Error('Preview/cancel wrote data')
    const choose = async (name) => {
      await page.getByRole('combobox', { name: '机器人面板', exact: true })
        .locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
      await page.getByRole('option').filter({ hasText: name }).click()
    }
    await choose('工作台总览')
    delayPreview = true
    await page.getByRole('button', { name: '删除机器人 保留机器人', exact: true }).click()
    await previewEntered
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await dialog.waitFor({ state: 'hidden' })
    await choose('待删除机器人')
    await open('待删除机器人')
    releasePreview()
    delayPreview = false
    await dialog.locator('.removal-target').filter({ hasText: '待删除机器人' }).waitFor()
    for (const viewport of [{ width: 320, height: 568 }, { width: 390, height: 844 }, { width: 647, height: 779 }, { width: 1440, height: 960 }]) {
      await page.setViewportSize(viewport)
      const fits = await dialog.evaluate((el) => {
        const box = el.getBoundingClientRect(), footer = el.querySelector('.el-dialog__footer').getBoundingClientRect()
        return box.left >= 0 && box.right <= innerWidth + 1 && box.top >= 0 && box.bottom <= innerHeight + 1 &&
          footer.bottom <= innerHeight && footer.top >= 0 && document.documentElement.scrollWidth <= innerWidth
      })
      if (!fits) throw new Error(`Removal dialog overflow ${viewport.width}`)
      await page.screenshot({ path: `output/playwright/account-removal-${viewport.width}.png`, animations: 'disabled' })
    }
    const input = dialog.getByLabel('输入机器人名称确认删除', { exact: true })
    const confirm = dialog.getByRole('button', { name: '删除并清理', exact: true })
    await input.fill('wrong')
    if (await confirm.isEnabled()) throw new Error('Wrong name enabled removal')
    await input.fill('待删除机器人')
    await confirm.click()
    await dialog.getByText('配置已变化，请刷新后重新编辑', { exact: true }).waitFor()
    if (await input.inputValue() !== '待删除机器人') throw new Error('Conflict lost confirmation input')
    await dialog.getByRole('button', { name: '重新核对', exact: true }).click()
    await input.waitFor()
    await confirm.click()
    await dialog.waitFor({ state: 'hidden' })
    await page.getByRole('button', { name: '保留机器人', exact: true }).waitFor()
    const storage = await page.evaluate(({ owner }) => ({
      drafts: sessionStorage.getItem(`wsl:drafts:${owner}`) || '',
      selected: sessionStorage.getItem(`wsl:account:${owner}:wsl`) || ''
    }), { owner })
    if (storage.selected || storage.drafts.includes(id(1)) || storage.drafts.includes(group) ||
        !storage.drafts.includes(id(2)) || !storage.drafts.includes(id(3))) throw new Error('Scoped draft cleanup failed')
    blocked = true
    await open('保留机器人')
    await dialog.getByLabel('输入机器人名称确认删除', { exact: true }).fill('保留机器人')
    if (await confirm.isEnabled()) throw new Error('Unknown result did not block removal')
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await dialog.waitFor({ state: 'hidden' })
    blocked = false
    await page.getByRole('button', { name: '切换工作台', exact: true }).click()
    await page.locator('.brand-name').filter({ hasText: '热群机器人' }).waitFor()
    await open('独立热群机器人')
    await dialog.getByLabel('输入机器人名称确认删除', { exact: true }).fill('独立热群机器人')
    await confirm.click()
    await dialog.waitFor({ state: 'hidden' })
    if (writes.length !== 3 || writes[2].workspace !== 'maibot' || accounts.length !== 1 || accounts[0].id !== id(2)) throw new Error('Workspace deletion leaked')
    tenant = true
    await page.evaluate((owner) => sessionStorage.setItem(`wsl:workspace:${owner}`, 'wsl'), owner)
    await page.reload()
    await page.getByRole('button', { name: '保留机器人', exact: true }).waitFor()
    if (await page.getByRole('button', { name: /^删除机器人 / }).count()) throw new Error('Tenant can delete a shared robot')
    return { passed: true, realDeletes: 0, syntheticDeletes: 2, versionConflicts: 1, cancelWrites: 0, scopedCleanup: true, latePreviewIgnored: true, blockers: true, tenantDenied: true, viewports: [320, 390, 647, 1440] }
  } finally {
    releasePreview()
    await page.unroute(pattern, handler)
    await page.evaluate((saved) => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      for (const [key, value] of Object.entries(saved)) sessionStorage.setItem(key, value)
    }, original)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
  }
}
