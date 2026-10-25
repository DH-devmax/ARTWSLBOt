// Read-only live smoke. Detailed synthetic coverage lives in workspaces_ui_smoke.js.
// prettier-ignore
async (page) => {
  if (new URL(page.url()).origin !== 'http://127.0.0.1:5173') throw new Error('Local development UI only')
  const writes = []
  const track = (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/v1/') && !['GET', 'HEAD'].includes(request.method())) {
      writes.push(`${request.method()} ${new URL(request.url()).pathname}`)
    }
  }
  page.on('request', track)
  try {
    const chooseOverview = async () => {
      await page.getByRole('combobox', { name: '机器人面板', exact: true })
        .locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
      await page.getByRole('option', { name: '工作台总览', exact: true }).click()
    }
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto('http://127.0.0.1:5173/#overview')
    await page.reload()
    const navigation = page.getByRole('navigation', { name: '主导航' })
    await navigation.waitFor()
    await chooseOverview()
    if (await navigation.getByRole('button', { name: '账号接入', exact: true }).count()) {
      throw new Error('Legacy account menu still exposed')
    }
    const selector = page.getByRole('button', { name: '切换工作台', exact: true })
    if (await selector.count()) {
      await selector.click()
      await chooseOverview()
      await page.getByRole('heading', { name: '聊天机器人状态', exact: true }).waitFor()
      if (await navigation.getByRole('button', { name: '财务台账', exact: true }).count()) {
        throw new Error('Chat workspace exposes management navigation')
      }
      await selector.click()
      await chooseOverview()
      await page.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    }
    await page.screenshot({ path: 'output/playwright/live-workspaces-desktop.png', fullPage: true, animations: 'disabled' })
    await page.setViewportSize({ width: 390, height: 844 })
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('Mobile overflow')
    await page.screenshot({ path: 'output/playwright/live-workspaces-mobile.png', fullPage: true, animations: 'disabled' })
    if (writes.length) throw new Error(`Read-only navigation submitted writes: ${writes.join(', ')}`)
    return { passed: true, mode: 'read-only', writes: 0, native_connected: false, ai_connected: false }
  } finally {
    page.off('request', track)
    await page.setViewportSize({ width: 1440, height: 960 })
  }
}
