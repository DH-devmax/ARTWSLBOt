// All API traffic is synthetic; no native account or business action is used.
async (page) => {
  const base = new URL(page.url()).origin
  const a = 'd2000000-0000-4000-8000-000000000001', b = 'd2000000-0000-4000-8000-000000000002'
  const group = 'd3000000-0000-4000-8000-000000000001'
  const now = new Date().toISOString()
  const accounts = [a,b].map((id,i)=>({id,name:`监控合成${i+1}`,workspace:'maibot',auth_state:'authenticated'}))
  const distribution = {available:true,samples:1000,sample_limit:1000,window_seconds:86400,sampled_at:now,
    p50_ms:0.4,p95_ms:1.7,p99_ms:5.1,histogram:[700,200,70,20,8,2]}
  const receiver = {enabled:true,observed:true,snapshot:{state:'connected',sampled_at:now,synced_at:now,heartbeat_at:now,
    per_second:3,unique_per_second:2.9,durable_p95_ms:2,ack_p95_ms:3,latency_samples:90,ack_samples:90,queue_depth:1,queue_bytes:1024}}
  const snapshot = {workspace:'maibot',monitor_scope:'platform_workspace',updated_at:now,
    database_available:true,database_sampled_at:now,
    decode_latency:distribution,
    api:{available:true,stale:false,sampled_at:now,memory_bytes:64*1024**2,memory_limit_bytes:384*1024**2,process_rss_bytes:48*1024**2,cpu_quota_percent:5,cpu_quota_cores:0.5,go_heap_bytes:30*1024**2,go_goroutines:22,go_gc_cycles:4,go_gc_pause_total_ns:1000000},
    services:{},
    database_stats:{available:true,stale:false,sampled_at:now,size_bytes:1024**3,connections:8,commits:1200,rollbacks:3,deadlocks:0,query_ms:0.4},
    components:{api:'observed',postgres:'observed',heatbot:'observed',web:'observed'},
    database_pools:{platform:{acquired:1,total:2,maximum:8,empty_acquires:0,acquire_wait_total_ms:1}},
    accounts:accounts.map((account,i)=>({...account,bound_groups:10-i,group_limit:30,over_capacity:false,receiver,
      http:{observed:true,sampled_at:now,queue_depth:i,running:false,http_per_second:0.1,last_wait_ms:2,cooldown_until:null},
      backlog_available:true,backlog_stale:false,backlog_sampled_at:now,
      decode_pending:i,decode_failed:i?3:0,decode_latency:{...distribution,p95_ms:1},decode_samples:20,decode_p95_ms:1,operations_queued:0,operations_unknown:0,sampled_at:now,reasons:i?['decode_failed']:[]}))}
  snapshot.services = {
    api:{available:true,stale:false,sampled_at:now,resources:snapshot.api},
    heatbot:{available:true,stale:false,sampled_at:now,resources:{...snapshot.api,source:'heatbot_self',memory_bytes:32*1024**2,process_rss_bytes:16*1024**2,go_heap_bytes:8*1024**2}},
    web:{available:true,stale:false,sampled_at:now},
  }
  snapshot.read_caches = ['wsl','lajiaoliao'].flatMap(platform => ['management','chat'].map(purpose => ({
    platform,purpose,hits:80,misses:20,coalesced:2,evictions:1,expired:0,entries:8,bytes:4096,in_flight:0
  })))
  snapshot.heat_chat = {candidates:10,model_calls:1,confirmed_sends:1,blocked:2,active:0}
  const requests = []
  let posts = 0, historyRequests = 0, incidentRequests = 0, historyFailure = false, incidentFailure = false
  function freshen() {
    const at = new Date().toISOString()
    snapshot.updated_at = at
    snapshot.database_sampled_at = at
    snapshot.api.sampled_at = at
    snapshot.database_stats.sampled_at = at
    Object.values(snapshot.services).forEach(s => { s.sampled_at = at; if(s.resources)s.resources.sampled_at=at })
    snapshot.accounts.forEach(a => {
      a.sampled_at = at; a.receiver.snapshot.sampled_at = at; a.http.sampled_at = at
    })
  }
  const handler = async route => {
    const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1','')
    if (request.method() !== 'GET') {
      if (path !== `/groups/${group}/assignment`) throw new Error('Unexpected native or business write')
      requests.push({key:request.headers()['idempotency-key'],body:request.postDataJSON()})
      posts++
      await route.fulfill({status:posts===1?503:200,json:posts===1?{code:503,msg:'assignment_commit_unknown'}:{code:0,data:{state:'binding_saved',version:2}}})
      return
    }
    if (path === '/system/monitor/events') {
      const older = JSON.parse(JSON.stringify(snapshot))
      older.updated_at = new Date(Date.parse(snapshot.updated_at)-1000).toISOString()
      older.accounts.forEach(a => a.name = '过期响应不应显示')
      await route.fulfill({contentType:'text/event-stream',body:`id: 1\nevent: snapshot\ndata: ${JSON.stringify(snapshot)}\n\nid: 2\nevent: snapshot\ndata: ${JSON.stringify(older)}\n\n`})
      return
    }
    let data = []
    if (path === '/me') data = {user:{id:'d1000000-0000-4000-8000-000000000001',role:'superadmin',username:'monitor-fixture'},permissions:['overview:read','groups:read','accounts:read','workspaces:maibot']}
    else if (path === '/workspaces') data = {items:[{id:'user',name:'用户中心'},{id:'wsl',name:'旺商聊机器人'},{id:'maibot',name:'热群机器人'}]}
    else if (path === '/accounts') data = accounts
    else if (path === '/overview') data = {accounts:2,groups:1,limits:{accounts:10,groups:100,ai_groups:100}}
    else if (path === '/groups') data = [{group_id:group,name:'合成分配群',account_name:accounts[0].name,primary_account_id:a,active:true,version:1}]
    else if (path === '/system/monitor') data = snapshot
    else if (path === '/system/monitor/history') {
      historyRequests++
      if (historyFailure) return route.fulfill({status:503,json:{code:503,msg:'database_unavailable'}})
      data = [4,3,1,0].map(offset=>{
        const minute=new Date(Date.now()-offset*60000).toISOString()
        const services=JSON.parse(JSON.stringify(snapshot.services))
        Object.values(services).forEach(s=>{s.sampled_at=minute;if(s.resources)s.resources.sampled_at=minute})
        // Compatibility-only environment history must never become a chart.
        return {minute,sample:{api:{...snapshot.api,sampled_at:minute},services,
          environment:{available:true,stale:false,used_memory_bytes:3*1024**3}}}
      })
    }
    else if (path === '/system/monitor/incidents') {
      incidentRequests++
      if (incidentFailure) return route.fulfill({status:503,json:{code:503,msg:'database_unavailable'}})
      data = [{id:1,account_id:a,component:'http',reason:'http_rate_limited',state:'opened',created_at:now},
        {id:2,component:'simon',reason:'simon_unavailable',state:'opened',created_at:now}]
    }
    else if (path === `/groups/${group}/assignment`) data = {group_id:group,source_account_id:a,source_epoch:1,version:1,binding_epoch:1,sampled_at:now,recommended_account_id:b,
      candidates:accounts.map((account,i)=>({...account,epoch:1,bound_groups:10-i,eligible:true,reasons:[],native_role:'admin',http_wait_ms:2,receive_per_second:3}))}
    await route.fulfill({json:{code:0,msg:'ok',data}})
  }
  await page.route('**/api/v1/**',handler)
  const errors = []
  const listener = e => errors.push(e.message)
  page.on('pageerror',listener)
  try {
    await page.evaluate(()=>{
      sessionStorage.clear()
      sessionStorage.setItem('wsl:workspace:d1000000-0000-4000-8000-000000000001','maibot')
    })
    await page.goto(base+'/#system-monitor')
    await page.reload()
    const monitor = page.getByTestId('runtime-monitor')
    await monitor.getByRole('tab',{name:'运行总览',exact:true}).waitFor()
    for (const width of [320,390,768,1440]) {
      freshen()
      await page.setViewportSize({width,height:900})
      await page.reload()
      await monitor.getByRole('tab',{name:'运行总览',exact:true}).waitFor()
      await monitor.getByTestId('monitor-total-p95').getByText('1.7 ms',{exact:true}).waitFor()
      if (await monitor.getByText('过期响应不应显示',{exact:true}).count()) throw new Error('Late SSE snapshot replaced newer data')
      await monitor.getByText('解码失败 3 条 · 近 7 天',{exact:true}).waitFor()
      const chain = monitor.locator('.monitor-kpis > div').last()
      if ((await chain.locator('strong').innerText()) !== '0') throw new Error('Historical failures were counted as current connection faults')
      await page.waitForFunction(() => [...document.querySelectorAll('[data-testid="runtime-monitor"] .monitor-header .el-tag, [data-testid="runtime-monitor"] .el-tab-pane:not([style*="display: none"]) .el-tag')].every(tag => Number(getComputedStyle(tag).opacity) === 1))
      for (const name of ['运行总览','账号链路','资源趋势','故障事件']) {
        await monitor.getByRole('tab',{name,exact:true}).click()
        if(name==='账号链路' && await monitor.locator('.monitor-cache-diagnostics').count()) {
          const details=monitor.locator('.monitor-cache-diagnostics')
          await details.locator('summary').click()
          if(await details.locator('.el-table__body tbody tr').count()!==4)
            throw new Error('Cache partition diagnostics missing or merged')
          await details.getByText('确认发送 1',{exact:false}).waitFor()
          await details.locator('summary').click()
        }
        if (name==='资源趋势') {
          await monitor.getByRole('radio',{name:'热群引擎',exact:true}).locator('xpath=ancestor::label').click()
          await monitor.getByRole('heading',{name:'热群引擎 自身',exact:true}).waitFor()
          if ((await monitor.locator('.resource-columns').innerText()).includes('可见环境'))
            throw new Error('Retired Simon resources remain visible')
          await monitor.locator('.resource-chart canvas').waitFor()
          await page.waitForFunction(()=>{
            const canvas=document.querySelector('[data-testid="runtime-monitor"] .resource-chart canvas')
            if(!canvas)return false
            const {data}=canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height)
            let top=0,bottom=0
            for(let y=25;y<canvas.height-30;y++)for(let x=50;x<canvas.width;x++){
              const i=(y*canvas.width+x)*4
              if(data[i+3]>100 && ((Math.abs(data[i]-22)<8&&Math.abs(data[i+1]-124)<8&&Math.abs(data[i+2]-128)<8)||
                 (Math.abs(data[i]-63)<8&&Math.abs(data[i+1]-111)<8&&Math.abs(data[i+2]-209)<8))){
                if(y<canvas.height/2)top++;else bottom++
              }
            }
            return top>5&&bottom>5
          })
          await monitor.getByRole('radio',{name:'API',exact:true}).locator('xpath=ancestor::label').click()
        }
        await page.screenshot({path:`output/playwright/runtime-${width}-${name}.png`,fullPage:true})
        if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2))throw new Error(`Page overflow ${width}/${name}`)
      }
    }
    snapshot.services.heatbot.available = false
    snapshot.database_stats.available = false
    await page.reload()
    await monitor.getByRole('tab',{name:'资源趋势',exact:true}).click()
    await monitor.getByRole('radio',{name:'热群引擎',exact:true}).locator('xpath=ancestor::label').click()
    if (!(await monitor.locator('.resource-columns').innerText()).includes('未采集 / 已过期'))
      throw new Error('Failed collection displayed as fresh')
    snapshot.services.heatbot.available = true
    snapshot.database_stats.available = true
    await page.reload()
    freshen()
    await monitor.getByRole('tab',{name:'资源趋势',exact:true}).click()
    historyFailure = true
    await monitor.getByRole('button',{name:'刷新当前视图',exact:true}).click()
    await monitor.getByText('趋势加载失败，请刷新重试',{exact:true}).waitFor()
    await page.waitForTimeout(1200)
    await monitor.getByText('趋势加载失败，请刷新重试',{exact:true}).waitFor()
    const beforeHistory = historyRequests
    historyFailure = false
    await monitor.getByRole('button',{name:'刷新当前视图',exact:true}).click()
    await page.waitForFunction(()=>!document.querySelector('.chart-empty'))
    if (historyRequests <= beforeHistory) throw new Error('Refresh did not reload visible history')
    await monitor.getByRole('tab',{name:'故障事件',exact:true}).click()
    incidentFailure = true
    await monitor.getByRole('button',{name:'刷新当前视图',exact:true}).click()
    await monitor.getByRole('alert').filter({hasText:'数据库'}).first().waitFor()
    const beforeIncidents = incidentRequests
    incidentFailure = false
    await monitor.getByRole('button',{name:'刷新当前视图',exact:true}).click()
    await page.waitForFunction(()=>!document.querySelector('.monitor-tabs .el-alert--error'))
    if (incidentRequests <= beforeIncidents) throw new Error('Refresh did not reload incidents')
    freshen()
    snapshot.accounts[0].backlog_available = false
    snapshot.accounts[0].backlog_stale = true
    snapshot.accounts[1].backlog_stale = true
    await page.goto(base+'/#system-monitor')
    await page.reload()
    await monitor.getByRole('tab',{name:'运行总览',exact:true}).waitFor()
    if (!await monitor.locator('.monitor-kpis').innerText().then(text=>text.includes('未采集')))
      throw new Error('Unavailable historical backlog was shown as zero')
    await monitor.getByRole('tab',{name:'账号链路',exact:true}).click()
    await monitor.getByText('历史采样 · 最久 未知', { exact:false }).first().waitFor()
    snapshot.database_available = false
    await page.reload()
    await monitor.getByRole('tab',{name:'运行总览',exact:true}).waitFor()
    await page.waitForFunction(()=>[...document.querySelectorAll('.monitor-kpis strong')].every(el=>el.textContent.includes('未采集')))
    snapshot.database_available = true
    freshen()
    await page.evaluate(()=>sessionStorage.setItem('wsl:workspace:d1000000-0000-4000-8000-000000000001','wsl'))
    await page.goto(base+'/#groups')
    await page.reload()
    await page.getByRole('button',{name:'分配账号',exact:true}).click()
    const dialog=page.getByRole('dialog',{name:'群账号分配'})
    await dialog.getByRole('radio',{name:'分配给 监控合成2'}).locator('xpath=ancestor::label').click()
    await dialog.getByRole('button',{name:'确认分配',exact:true}).click()
    await dialog.getByRole('button',{name:'核对同一提交',exact:true}).waitFor()
    await dialog.getByRole('button',{name:'核对同一提交',exact:true}).click()
    await dialog.waitFor({state:'hidden'})
    if(requests.length!==2||requests[0].key!==requests[1].key||JSON.stringify(requests[0].body)!==JSON.stringify(requests[1].body))throw new Error('Unknown assignment created a different request')
    snapshot.database_sampled_at = '0001-01-01T00:00:00Z'
    snapshot.database_available = false
    snapshot.available = false
    snapshot.accounts = []
    await page.setViewportSize({width:390,height:900})
    await page.evaluate(()=>sessionStorage.setItem('wsl:workspace:d1000000-0000-4000-8000-000000000001','maibot'))
    await page.goto(base+'/#system-monitor')
    await page.reload()
    await page.getByTestId('runtime-monitor').getByRole('tab',{name:'运行总览',exact:true}).waitFor()
    await page.waitForFunction(()=>document.querySelector('.monitor-kpis')?.textContent.includes('未采集'))
    const missing = await page.locator('.monitor-kpis strong').allTextContents()
    if(missing.some(value=>!value.includes('未采集')))throw new Error('Missing database sample was displayed as zero')
    if(errors.length)throw new Error(`Browser runtime errors: ${errors.join(',')}`)
    return {widths:[320,390,768,1440],views:4,canvas:'service CPU and memory nonblank',services:'API and heatbot',retired_source:'history only',missing:'not zero',stale_backlog:true,total_p95:'server aggregate',late_sse:'ignored',historical_failures:'separate',refresh:'history and incidents',assignment:'same-key reconciliation',tags:'no transition flicker',native_actions:0}
  } finally {
    await page.unroute('**/api/v1/**',handler)
    page.off('pageerror',listener)
  }
}
