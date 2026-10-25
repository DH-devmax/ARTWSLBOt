// All writes and account telemetry are intercepted synthetic fixtures.
// prettier-ignore
async (page) => {
  const base = 'http://127.0.0.1:5173'
  if (new URL(page.url()).origin !== base) throw new Error('Local console only')
  const realMe = (await (await page.request.get(base + '/api/v1/me')).json()).data
  const storage = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([k]) => k.startsWith('wsl:'))))
  const id = '60000000-0000-4000-8000-000000000001'
  const account = { id, name: '接收测试机器人', workspace: 'wsl', auth_state: 'authenticated', version: 1, connection_enabled: false, runtime_state: 'connected' }
  let enabled = false, revision = 0
  const writes = []
  const now = new Date().toISOString()
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
    let data = []
    if (request.method() !== 'GET') {
      if (path !== `/accounts/${id}/receiver` || request.method() !== 'PUT') throw new Error('Unexpected receiver write')
      const input = request.postDataJSON()
      if (input.revision !== revision) throw new Error('Receiver revision missing')
      writes.push(input)
      enabled = input.enabled
      data = { enabled, revision: ++revision }
    } else if (path === '/me') data = realMe
    else if (path === '/accounts') data = [account]
    else if (path === '/overview') data = { accounts: 1, groups: 0, limits: { accounts: 10, groups: 100, ai_groups: 100 } }
    else if (path.endsWith('/dashboard')) data = {
      account, sampled_at: now, visibility: 'account', groups: {authorized: 0, management_enabled: 0, ai_enabled: 0, ai_pending: 0},
      operations: {queued: 0, running: 0, unknown: 0, failed_15m: 0},
      inbox: {pending: 0, unresolved: 0, quarantined: 0},
      performance: {window_seconds:60,per_second:0,latency_samples:0,invalid_timing_samples:0,p50_ms:null,p95_ms:null,p99_ms:null,series:[]},
      health: {status:enabled?'healthy':'disabled',reasons:[]},backlog:{status:'empty',reasons:[]},backlog_threshold_seconds:30,runtime_observed:enabled,
      receiver: {enabled,revision,observed:enabled,observed_at:now,snapshot:enabled?{
        state:'connected',heartbeat_at:now,heartbeat_ms:12.3,last_received_at:now,last_ack_at:now,
        frames:102,messages:100,duplicates:2,acks:100,quarantined:0,reconnects:0,queue_depth:0,queue_capacity:32,per_second:1.67,received_60s:100,
        durable_p95_ms:8.4,ack_p95_ms:10.1,latency_samples:100
      }:null}
    }
    await route.fulfill({json:{code:0,data,msg:''}})
  }
  await page.route(pattern, handler)
  try {
    await page.evaluate(() => { for (const k of Object.keys(sessionStorage)) if (k.startsWith('wsl:')) sessionStorage.removeItem(k) })
    await page.setViewportSize({width:1440,height:960})
    await page.goto(base + '/#overview')
    await page.reload()
    await page.getByRole('combobox',{name:'机器人面板',exact:true}).locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
    await page.getByRole('option').filter({hasText:account.name}).click()
    const dashboard=page.getByTestId('account-dashboard')
    const toggle=dashboard.getByRole('switch',{name:'消息接收',exact:true}).locator('xpath=..').locator('.el-switch__core')
    await toggle.waitFor()
    if (writes.length) throw new Error('Selecting account started receiver')
    await toggle.click()
    await dashboard.getByText('接收正常',{exact:true}).waitFor()
    await dashboard.getByText('8.4 ms',{exact:true}).waitFor()
    await dashboard.getByText('10.1 ms',{exact:true}).waitFor()
    await dashboard.getByText('1.67 条/秒',{exact:true}).waitFor()
    for (const viewport of [{width:320,height:740},{width:647,height:779},{width:1440,height:960}]) {
      await page.setViewportSize(viewport)
      if (viewport.width <= 760) {
        if (await page.locator('.sidebar.open').count()) await page.locator('.sidebar-scrim').click()
        await page.waitForFunction(() => document.querySelector('.sidebar').getBoundingClientRect().right <= 1)
      }
      const fits=await dashboard.evaluate((el)=>el.scrollWidth<=el.clientWidth+1)
      if(!fits) throw new Error('Receiver metrics overflow viewport')
      await page.screenshot({path:`/tmp/output/playwright/native-receiver-${viewport.width}.png`,fullPage:true})
    }
    await toggle.click()
    await dashboard.getByText('接入停用',{exact:true}).waitFor()
    if (writes.length!==2||writes[0].enabled!==true||writes[1].enabled!==false) throw new Error('Receiver controls did not preserve independent state')
    return {writes:2,viewports:[320,647,1440],realNativeRequests:0}
  } finally {
    await page.unroute(pattern,handler)
    await page.evaluate((items)=>{
      for(const k of Object.keys(sessionStorage)) if(k.startsWith('wsl:')) sessionStorage.removeItem(k)
      for(const [k,v] of Object.entries(items)) sessionStorage.setItem(k,v)
    },storage)
    await page.setViewportSize({width:1440,height:960})
    await page.goto(base+'/#overview')
    await page.reload()
    await page.getByRole('heading',{name:'工作台',exact:true}).waitFor()
  }
}
