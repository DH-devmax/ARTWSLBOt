// Local UI only: real verification UI, intercepted authentication, no challenge solving.
// playwright-cli expects a bare function expression without statement separators.
// prettier-ignore
async (page) => {
  if (new URL(page.url()).origin !== 'http://127.0.0.1:5173') {
    throw new Error(
      'Captcha layout smoke only supports the local development UI',
    )
  }
  const end = new Date(Date.now() + 600000).toISOString()
  const envelope = (data) => ({ code: 0, data, msg: '' })
  let submitted = 0
  let showError = false
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.endsWith('/native-deployment')) {
      await route.fulfill({
        json: envelope({
          configured: true,
          crypto_ready: true,
          protocol_version: '3.7.4',
          version: 1,
        }),
      })
    } else if (/\/accounts\/[^/]+\/native-auth(?:\/[^/]+)?$/.test(path)) {
      const start = !path.includes('/native-auth/')
      if (!start && request.method() === 'POST') submitted++
      await route.fulfill({
        json: envelope({
          account_id: path.split('/')[4],
          transaction_id: 'layout-only-no-native-request',
          status: request.method() === 'DELETE' ? 'cancelled' : 'pending',
          purpose: 'login',
          error: showError ? '人机验证未通过' : '',
          captcha_id: '37ab6b750c3246af804132808408f398',
          expires_at: end,
        }),
      })
    } else if (request.method() === 'GET') {
      await route.continue()
    } else {
      await route.abort('blockedbyclient')
      throw new Error('Unexpected write blocked by layout smoke')
    }
  }
  await page.route(pattern, handler)
  try {
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.reload()
    await page.getByRole('button', { name: '工作台', exact: true }).click()
    await page.getByRole('heading', { name: '工作台', exact: true }).waitFor()
    const cases = [
      { width: 647, height: 779, label: 'reported', error: true },
      { width: 390, height: 844, label: 'mobile' },
      { width: 320, height: 568, label: 'narrow', error: true },
      { width: 844, height: 390, label: 'landscape', error: true },
      { width: 647, height: 480, label: 'short', error: true },
      { width: 1440, height: 960, label: 'desktop' },
    ]
    const results = []
    const inspect = () =>
      page.evaluate(() => {
        const modal = document.querySelector('.yidun_modal')
        const dialog = document.querySelector('.native-auth-dialog')
        const footer = dialog.querySelector('.el-dialog__footer')
        const body = dialog.querySelector('.el-dialog__body')
        const box = (node) => {
          const rect = node.getBoundingClientRect()
          return {
            x: rect.x,
            y: rect.y,
            width: rect.width,
            height: rect.height,
          }
        }
        return {
          viewport: { width: innerWidth, height: innerHeight },
          modal: box(modal),
          dialog: box(dialog),
          footer: box(footer),
          modalInsideDialog: !!modal.closest('.el-dialog'),
          ownedLayer: !!modal.closest('.native-captcha-layer'),
          horizontalOverflow: document.documentElement.scrollWidth > innerWidth,
          captchaOverflow: modal.scrollWidth > modal.clientWidth,
          bodyScrollable: body.scrollHeight > body.clientHeight,
          bodyOverflowY: getComputedStyle(body).overflowY,
        }
      })
    const assertBounds = (value) => {
      const { viewport } = value
      for (const [name, rect] of Object.entries({
        modal: value.modal,
        dialog: value.dialog,
        footer: value.footer,
      })) {
        if (
          rect.x < -1 ||
          rect.y < -1 ||
          rect.x + rect.width > viewport.width + 1 ||
          rect.y + rect.height > viewport.height + 1
        ) {
          throw new Error(
            `${name} exceeds ${viewport.width}x${viewport.height}: ${JSON.stringify(rect)}`,
          )
        }
      }
      if (
        value.modalInsideDialog ||
        !value.ownedLayer ||
        value.horizontalOverflow ||
        value.captchaOverflow ||
        (value.bodyScrollable && value.bodyOverflowY !== 'auto')
      ) {
        throw new Error(`Invalid captcha containment: ${JSON.stringify(value)}`)
      }
    }
    for (const view of cases) {
      showError = !!view.error
      await page.setViewportSize({ width: view.width, height: view.height })
      await page
        .getByRole('row')
        .filter({ hasText: '小满' })
        .getByRole('button', { name: '注册 / 登录', exact: true })
        .click()
      await page
        .getByLabel('账号 / 手机号', { exact: true })
        .fill('13800000000')
      await page
        .getByLabel('原生账号密码', { exact: true })
        .fill('synthetic-layout-only1')
      if (showError)
        await page.getByText('人机验证未通过', { exact: true }).waitFor()
      await page
        .getByRole('button', { name: '验证并登录', exact: true })
        .click()
      await page.locator('.yidun_modal:visible').waitFor({ timeout: 45000 })
      // Wait for the SDK's own opening transition, not a half-scaled intermediate frame.
      await page.locator('.yidun_modal:visible').evaluate(async (element) => {
        await Promise.all(
          element
            .getAnimations()
            .map((animation) => animation.finished.catch(() => {})),
        )
      })
      const bounds = await inspect()
      assertBounds(bounds)
      results.push({ label: view.label, ...bounds })
      await page.screenshot({
        path: `output/playwright/native-captcha-${view.label}.png`,
      })
      await page.locator('.yidun_modal__close:visible').click()
      if (
        (await page
          .getByLabel('账号 / 手机号', { exact: true })
          .inputValue()) !== '13800000000'
      ) {
        throw new Error('Closing verification lost the login form')
      }
      await page.getByRole('button', { name: '关闭面板', exact: true }).click()
      await page.waitForFunction(
        () => !document.querySelector('.native-captcha-layer .yidun_popup'),
      )
    }
    if (submitted)
      throw new Error(
        'Verification must not submit authentication during layout smoke',
      )
    return { realCaptchaUI: true, nativeRequests: 0, submitted, results }
  } finally {
    await page.unroute(pattern, handler)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.reload()
  }
}
