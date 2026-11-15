// Synthetic API fixtures only; no native, model, login or connection operation is forwarded.
// prettier-ignore
async (page) => {
  const base = new URL(page.url()).origin
  const id = n => `d3000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const now = new Date().toISOString(), actor = id(1)
  const allPermissions = ['overview:read','accounts:read','groups:read','groups:configure','groups:ai',
    'groups:revoke','assistant:use','assistant:action_propose','assistant:action_approve',
    'group_action:send_text','operations:read','audit:read','claims:read','claims:create','users:manage',
    'plans:manage','members:manage','tests:manage','workspaces:maibot']
  let role = 'superadmin', admitted = false, planCount = 0, approvals = 0, forbidden = 0
  let groupQueries = [], delayed = 0, cancellations = 0, targetQueries = 0, platformGroupCount = 150
  let usageMoney={unit:'micro_credit',currency:'USDT',currency_scale:1000000000,actual_micro:12500000,held_micro:3000000}
  const customers = [{id:id(2),name:'已到期客户'},{id:id(3),name:'未开通助手客户'}]
  const ownSpace = {id:id(90),name:'我的演示工作空间',kind:'operator_demo',owner_id:actor}
  const space = () => role==='superadmin'?ownSpace:
    {id:customers[0].id,name:customers[0].name,kind:'tenant_business',owner_id:id(91)}
  const plan = () => role==='superadmin'
    ? {id:'operator_unlimited',name:'超管专属 · 不限额',system_only:true,unlimited:true,permanent:true,
      active:true,group_limit:null,member_limit:null,assistant_limit:null,credit_limit:null,platforms:[]}
    : {id:'',name:'未开通套餐',active:false,system_only:false,unlimited:false,permanent:false,
      group_limit:0,member_limit:0,assistant_limit:0,credit_limit:0,platforms:[],modules:[]}
  const accounts = ['wsl','lajiaoliao'].map((platform,i) => ({
    id:id(10+i),platform,workspace:platform,purpose:'management',name:'号池备注',
    native_nickname:`${platform==='wsl'?'旺商聊':'辣椒聊'}原生机器人`,auth_state:'authenticated'
  }))
  const groups = ['wsl','lajiaoliao'].flatMap((platform,p) => Array.from({length:150},(_,i)=>({
    group_id:id(1000+p*150+i),name:`${platform==='wsl'?'旺商聊':'辣椒聊'}授权群 ${String(i+1).padStart(3,'0')}`,
    platform,active:true,tenant_id:i<149?ownSpace.id:customers[0].id,
    customer_name:i<149?ownSpace.name:customers[0].name,
    workspace:i<149?ownSpace:{id:customers[0].id,name:customers[0].name,kind:'tenant_business',owner_id:id(91)},
    robot_nickname:accounts[p].native_nickname,primary_account_id:accounts[p].id,
    version:1,management_enabled:false,business_enabled:false,
    business_blocked_reason:'group_runtime_disabled',ai_enabled:false,ai_requested:false
  })))
  const visibleGroups = () => groups.filter((_,i)=>i%150<platformGroupCount)
  const actions = () => [
    {id:'send_text',name:'发送群文本'},
    {id:'mute_member',name:'成员禁言'}
  ].map(action=>({...action,implemented:true,sources:['manual'],
    production_admitted:admitted,...(!admitted?{block_reason:'production_not_admitted'}:{})}))
  const plans = new Map(), errors = []
  const listener = e => errors.push(e.message)
  page.on('pageerror',listener)
  const handler = async route => {
    const req=route.request(), url=new URL(req.url()), path=url.pathname.replace('/api/v1','')
    const platform=req.headers()['x-workspace'] || 'wsl'
    const permissions=role==='superadmin'?allPermissions:['overview:read','groups:read','assistant:use','operations:read']
    let data=[]
    if(req.method()!=='GET'){
      if(!/^\/groups\/[^/]+\/action-plans(?:\/[^/]+\/(?:approve|cancel))?$/.test(path)){
        forbidden++;throw Error('Unexpected write '+path)
      }
      const group=groups.find(g=>g.group_id===path.split('/')[2])
      if(!group||group.platform!==platform)throw Error('Manual action crossed platforms')
      const body=req.postDataJSON()
      if(path.endsWith('/action-plans')){
        if(!['send_text','mute_member'].includes(body.action)||body.execution_mode||body.tenant_id||body.actor_id)
          throw Error('Client supplied trusted manual identity')
        if(body.action==='mute_member'&&(body.target_token!==`scoped:${group.group_id}:member-b`||body.minutes!==1))
          throw Error('Member action did not use the selected scoped target')
        planCount++
        const plan={id:id(3000+planCount),scope:{platform,kind:'group',account_id:group.primary_account_id,group_id:group.group_id},
          name:actions().find(a=>a.id===body.action).name,action:body.action,text:body.text,
          ...(body.action==='mute_member'?{minutes:body.minutes,target:{id:'member-b',name:'测试成员',role:'member'}}:{}),
          customer_name:group.customer_name,group_name:group.name,
          robot_nickname:group.robot_nickname,execution_mode:'operator_demo',status:admitted?'pending_approval':'blocked',
          production_admitted:admitted,digest:`server-immutable-digest-${planCount}`,version:1,
          ...(!admitted?{result_code:'production_not_admitted'}:{})}
        plans.set(plan.id,plan);data=plan
      }else{
        const plan=plans.get(path.split('/')[4])
        if(!plan)throw Error('Unknown manual plan')
        if(path.endsWith('/approve')){
          if(!admitted||!plan.production_admitted)throw Error('Closed gate approved')
          if(body.digest!==plan.digest||body.version!==plan.version||!body.key||
            body.key!==req.headers()['idempotency-key'])throw Error('Confirmation did not use immutable plan')
          approvals++;plan.status='unknown';plan.operation_id=id(4000);data=plan
        }else{cancellations++;plan.status='cancelled';plan.version++;data=plan}
      }
    }else if(path==='/me')data={user:{id:actor,username:'synthetic-operator',role,...(role==='user'?{tenant_id:customers[0].id,tenant_role:'member'}:{})},permissions}
    else if(path==='/workspaces')data={items:[
      {id:'user',name:'用户中心'},
      {id:'wsl',name:'旺商聊机器人',platform:'wsl'},
      {id:'lajiaoliao',name:'辣椒聊机器人',platform:'lajiaoliao'},
      ...(role==='superadmin'?[{id:'maibot',name:'热群机器人'}]:[])
    ]}
    else if(path==='/accounts')data=accounts.filter(a=>a.platform===platform)
    else if(path==='/groups')data=groups.filter(g=>g.platform===platform).slice(0,2)
    else if(path==='/overview')data={groups:150,accounts:1,limits:{accounts:10}}
    else if(path==='/user-center')data={
      platform_admin:role==='superadmin',permissions,subscription:null,
      workspace:space(),effective_plan:plan(),billing_source:role==='superadmin'?'platform_demo':'tenant_wallet',
      quotas:{groups:plan().group_limit,members:plan().member_limit,assistant:plan().assistant_limit,credits:plan().credit_limit},
      entitlement:{mode:role==='superadmin'?'operator_demo':'tenant_business',
        name:role==='superadmin'?'演示测试套餐':'未开通套餐',unlimited:role==='superadmin',
        billing_source:role==='superadmin'?'platform_demo':'tenant_wallet',
        group_limit:role==='superadmin'?null:1,member_limit:role==='superadmin'?null:1,
        assistant_limit:role==='superadmin'?null:0,credit_limit:role==='superadmin'?null:0,
        permissions,active:role==='superadmin'},
      groups:visibleGroups().slice(0,1),group_count:platformGroupCount*2,personas:[],claims:[],operations:[]
    }
    else if(path==='/user-center/groups'){
      const limit=Number(url.searchParams.get('page_size')||url.searchParams.get('limit')||20)
      const current=Number(url.searchParams.get('page')||1),search=url.searchParams.get('search')||''
      const selected=url.searchParams.get('platform'),tenant=url.searchParams.get('tenant_id')
      const workspace=url.searchParams.get('workspace')
      if(limit>50||limit!==20||!['wsl','lajiaoliao'].includes(selected))throw Error('Unbounded group query')
      if(role==='superadmin'&&!['mine','all'].includes(workspace))throw Error('Missing explicit workspace query')
      groupQueries.push({platform:selected,search,tenant,workspace,page:current,limit})
      if(search==='迟到'){delayed++;await new Promise(resolve=>setTimeout(resolve,400))}
      const matches=visibleGroups().filter(g=>g.platform===selected&&
        (role==='superadmin'?(tenant?g.tenant_id===tenant:workspace==='all'||g.tenant_id===ownSpace.id):g.tenant_id===customers[0].id)&&
        (!search||g.name.includes(search)))
      data={workspace:space(),effective_plan:plan(),items:matches.slice((current-1)*limit,current*limit),total:matches.length,
        offset:(current-1)*limit,limit,customers,sampled_at:now}
    }
    else if(path==='/user-center/usage')data={workspace:space(),effective_plan:plan(),billing_source:role==='superadmin'?'platform_demo':'tenant_wallet',
      execution_mode:role==='superadmin'?'operator_demo':'tenant_business',call_count:7,...usageMoney,
      sampled_at:now,month:'2026-11',
      unlimited:role==='superadmin',limit:role==='superadmin'?null:0}
    else if(/\/groups\/[^/]+\/capabilities$/.test(path))data={manual_actions:actions(),actions:[],execution_mode:'operator_demo',customer_runtime:{enabled:false}}
    else if(/\/groups\/[^/]+\/action-targets$/.test(path)){
      if(url.searchParams.get('action')!=='mute_member'||url.searchParams.get('search')!=='测试成员')
        throw Error('Target lookup was unbounded or lost action scope')
      targetQueries++
      const group=path.split('/')[2]
      data={items:['member-a','member-b'].map(id=>({id,name:'测试成员',role:'member',token:`scoped:${group}:${id}`}))}
    }
    else if(/\/groups\/[^/]+\/action-plans\/[^/]+$/.test(path)){
      data=plans.get(path.split('/')[4])
      data.production_admitted=admitted
    }
    else if(/\/groups\/[^/]+\/features$/.test(path)){
      const group=groups.find(g=>g.group_id===path.split('/')[2])
      data={group_id:group.group_id,account_id:group.primary_account_id,platform:group.platform,version:group.version,items:[]}
    }
    else if(path==='/features/catalog')data={platform,items:[],native_capabilities:[],native_details:[]}
    else if(path==='/assistant/context'){
      const selected=groups.find(g=>g.group_id===url.searchParams.get('group_id'))
      data={platform,groups:groups.filter(g=>g.platform===platform).slice(0,30),total:150,next_offset:30,permissions,actions:[],
        ...(selected?{scope:{platform,kind:'group',group_id:selected.group_id,account_id:selected.primary_account_id}}:{})}
    }
    else if(path==='/assistant/sessions')data=[]
    else if(path==='/assistant/usage')data={unit:'micro_credit',used_or_reserved:0,unlimited:true,limit:null,timezone:'Asia/Shanghai'}
    await route.fulfill({json:{code:0,msg:'ok',data}})
  }
  const openNav = async () => {
    const toggle=page.getByRole('button',{name:'展开导航',exact:true})
    if(await toggle.isVisible()&&await toggle.getAttribute('aria-expanded')!=='true')await toggle.click()
  }
  const clickNav = async name => { await openNav();await page.locator('#primary-sidebar nav').getByRole('button',{name,exact:true}).click() }
  const switchConsole=async name=>{await openNav();await page.getByRole('button',{name:`切换到${name}`,exact:true}).click()}
  const closeAction=async()=>{await page.getByRole('dialog',{name:'单次群管操作',exact:true}).getByRole('button',{name:'关闭',exact:true}).click()}
  const firstAction=()=>page.locator('.uc-groups').getByRole('button',{name:'单次群管',exact:true}).first()
  await page.route('**/api/v1/**',handler)
  try{
    await page.evaluate(()=>sessionStorage.clear())
    await page.setViewportSize({width:1440,height:960})
    await page.goto(base+'/#overview');await page.reload()
    await page.getByRole('button',{name:'切换到用户中心',exact:true}).waitFor()
    if(await page.locator('#primary-sidebar').getByRole('button',{name:'群管助手',exact:true}).count())throw Error('Business assistant remained in governance nav')
    await switchConsole('用户中心')
    await page.locator('.user-center').getByText('超管专属 · 不限额',{exact:true}).waitFor()
    await page.getByRole('region',{name:'工作台接入指引',exact:true}).getByRole('button',{name:'绑定群组',exact:true}).waitFor()
    await clickNav('套餐与费用')
    const meter=page.getByRole('region',{name:'我的实际用量'})
    await meter.getByText('0.0125 USDT',{exact:true}).waitFor()
    await meter.getByText('0.003 USDT',{exact:true}).waitFor()
    const checkMoney=async (next,actual,held)=>{
      usageMoney=next
      await meter.getByRole('button',{name:'刷新实际用量',exact:true}).click()
      await meter.getByText(actual,{exact:true}).waitFor()
      await meter.getByText(held,{exact:true}).waitFor()
    }
    await checkMoney({unit:'micro_credit',currency:'USDT',currency_scale:1000000000,actual_micro:1,held_micro:0},'0.000000001 USDT','0 USDT')
    await checkMoney({unit:'micro_credit',currency:'USDT',currency_scale:1000000,actual_micro:12500000,held_micro:3000000},'12.5 USDT','3 USDT')
    await checkMoney({unit:'micro_credit',actual_micro:12500000,held_micro:3000000},'0.0125 USDT','0.003 USDT')
    await checkMoney({unit:'micro_usd',actual_micro:12500,held_micro:3000},'0.0125 USD','0.003 USD')
    await checkMoney({unit:'micro_usdt',actual_micro:12500,held_micro:3000},'0.0125 USDT','0.003 USDT')
    usageMoney={unit:'micro_credit',currency:'USDT',currency_scale:1000000000,actual_micro:12500000,held_micro:3000000}
    await meter.getByRole('button',{name:'刷新实际用量',exact:true}).click()
    await clickNav('授权群组')
    const dataRows=()=>page.locator('.uc-groups .el-table__body-wrapper tbody tr')
    await page.waitForFunction(()=>document.querySelectorAll('.uc-groups .el-table__body-wrapper tbody tr').length===20)
    if(groupQueries.at(-1).page!==1)throw Error('Did not start server pagination at page one')
    if(groupQueries.at(-1).workspace!=='mine'||groupQueries.at(-1).tenant||
      await page.getByRole('combobox',{name:'筛选所属客户',exact:true}).count())throw Error('Default mine scope exposed customer selection')
    await page.locator('.uc-pagination .btn-next').click()
    await page.getByText('旺商聊授权群 021',{exact:true}).first().waitFor()
    if(groupQueries.at(-1).page!==2||await dataRows().count()!==20)throw Error('Pagination loaded a local/unbounded list')
    await page.getByRole('textbox',{name:'搜索我的群组',exact:true}).fill('149')
    await page.getByText('旺商聊授权群 149',{exact:true}).first().waitFor()
    if(groupQueries.at(-1).search!=='149'||groupQueries.at(-1).page!==1)throw Error('Search did not reset server page')
    await page.getByRole('textbox',{name:'搜索我的群组',exact:true}).fill('')
    await page.waitForFunction(()=>document.querySelectorAll('.uc-groups .el-table__body-wrapper tbody tr').length===20)
    await page.locator('.uc-workspace-filter label').filter({hasText:'全部工作空间'}).click()
    await page.getByRole('combobox',{name:'筛选所属客户',exact:true}).click()
    await page.getByRole('option',{name:'已到期客户',exact:true}).click()
    await page.getByText('旺商聊授权群 150',{exact:true}).first().waitFor()
    if(groupQueries.at(-1).workspace!=='all'||groupQueries.at(-1).tenant!==customers[0].id||
      await dataRows().count()!==1)throw Error('Explicit customer filter escaped its workspace')
    await page.locator('.uc-workspace-filter label').filter({hasText:'我的工作空间'}).click()
    await page.waitForFunction(()=>document.querySelectorAll('.uc-groups .el-table__body-wrapper tbody tr').length===20)
    await firstAction().click()
    let dialog=page.getByRole('dialog',{name:'单次群管操作',exact:true})
    await dialog.getByRole('textbox',{name:'消息内容',exact:true}).fill('合成演示文本，不发送真实群')
    await dialog.getByRole('button',{name:'核对操作计划',exact:true}).click()
    await dialog.getByText('未通过准入',{exact:true}).waitFor()
    if(await dialog.getByRole('button',{name:'确认发送',exact:true}).count())throw Error('Closed gate exposed confirmation')
    for(const width of [320,390,768,1440]){
      await page.setViewportSize({width,height:900});await page.waitForTimeout(100)
      const bounds=await dialog.boundingBox()
      if(!bounds||bounds.x<0||bounds.x+bounds.width>width+1)throw Error('Manual dialog overflow '+width)
      await page.screenshot({path:`output/playwright/operator-action-${width}.png`,animations:'disabled'})
    }
    await page.setViewportSize({width:1440,height:960})
    admitted=true
    await dialog.getByRole('button',{name:'查询原计划',exact:true}).click()
    await dialog.getByText('等待本人确认',{exact:true}).waitFor()
    await dialog.getByRole('button',{name:'确认发送',exact:true}).waitFor()
    if(planCount!==1||plans.get(id(3001)).status!=='blocked')throw Error('Opening dynamic gate replaced the original plan')
    admitted=false
    await dialog.getByRole('button',{name:'查询原计划',exact:true}).click()
    await dialog.getByText('未通过准入',{exact:true}).waitFor()
    await dialog.getByRole('button',{name:'确认发送',exact:true}).waitFor({state:'hidden'})
    if(await dialog.getByRole('button',{name:'确认发送',exact:true}).count())throw Error('Closing dynamic gate retained confirmation')
    admitted=true
    await dialog.getByRole('button',{name:'查询原计划',exact:true}).click()
    await dialog.getByRole('button',{name:'确认发送',exact:true}).click()
    await dialog.getByText('结果未知',{exact:true}).waitFor()
    await dialog.getByRole('button',{name:'查询原计划',exact:true}).click()
    if(approvals!==1||await dialog.getByRole('button',{name:'确认发送',exact:true}).count())throw Error('Unknown result could be resent')
    await closeAction()
    await firstAction().click()
    dialog=page.getByRole('dialog',{name:'单次群管操作',exact:true})
    await dialog.getByText('结果未知',{exact:true}).waitFor()
    if(planCount!==1||approvals!==1)throw Error('Opening history duplicated an action')
    await closeAction()
    await page.locator('.uc-groups').getByRole('button',{name:'单次群管',exact:true}).nth(1).click()
    dialog=page.getByRole('dialog',{name:'单次群管操作',exact:true})
    await dialog.getByRole('combobox',{name:'群管动作',exact:true}).press('ArrowDown')
    await page.getByRole('option',{name:'成员禁言',exact:true}).click()
    if(!(await dialog.getByRole('button',{name:'核对操作计划',exact:true}).isDisabled()))
      throw Error('Member action could proceed without a scoped candidate')
    await dialog.getByRole('combobox',{name:'群管目标',exact:true}).fill('测试成员')
    await page.getByRole('option',{name:'测试成员 · member-b',exact:true}).click()
    await dialog.getByRole('button',{name:'核对操作计划',exact:true}).click()
    await dialog.getByText('测试成员 · member-b',{exact:true}).waitFor()
    await dialog.getByRole('button',{name:'确认禁言',exact:true}).waitFor()
    if(approvals!==1)throw Error('Plan creation submitted a native action')
    await dialog.getByRole('button',{name:'取消计划',exact:true}).click()
    await dialog.getByText('已取消',{exact:true}).waitFor()
    await closeAction()
    await page.locator('.uc-groups').getByRole('button',{name:'群管助手',exact:true}).first().click()
    await page.getByRole('region',{name:'助手会话工作台'}).waitFor()
    if(await page.evaluate(()=>sessionStorage.getItem('wsl:console:d3000000-0000-4000-8000-000000000001'))!=='user')throw Error('Assistant jumped back to governance console')
    await clickNav('授权群组')
    await page.getByRole('heading',{name:'授权群组',exact:true}).waitFor()
    await page.getByRole('button',{name:'切换工作台',exact:true}).click()
    await page.getByRole('dialog',{name:'选择平台',exact:true}).getByRole('button',{name:'辣椒聊机器人',exact:true}).click()
    await page.getByText('辣椒聊授权群 001',{exact:true}).first().waitFor()
    if(groupQueries.at(-1).platform!=='lajiaoliao')throw Error('Platform scope did not follow switch')
    if(!page.url().endsWith('#groups'))throw Error('Platform switch lost the business page')
    platformGroupCount=50
    await page.getByRole('button',{name:'刷新用户中心',exact:true}).click()
    await page.getByText('50 个群组 · 我的演示工作空间',{exact:true}).waitFor()
    await page.waitForFunction(()=>document.querySelectorAll('.uc-groups .el-table__body-wrapper tbody tr').length===20)
    if(await dataRows().count()!==20)throw Error('100-group matrix stopped server pagination')
    for(const width of [320,390,768,1440]){
      await page.setViewportSize({width,height:900});await page.waitForTimeout(100)
      if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('User center overflow '+width)
      await openNav()
      await page.getByRole('button',{name:'切换到管理后台',exact:true}).waitFor()
      await page.screenshot({path:`output/playwright/operator-user-${width}.png`,animations:'disabled'})
      const close=page.getByRole('button',{name:'收起导航',exact:true})
      if(await close.isVisible())await close.click()
      await page.screenshot({path:`output/playwright/operator-groups-${width}.png`,animations:'disabled'})
    }
    await page.setViewportSize({width:1440,height:960})
    await page.getByRole('textbox',{name:'搜索我的群组',exact:true}).fill('迟到')
    await page.waitForTimeout(290)
    await switchConsole('管理后台')
    await page.waitForTimeout(500)
    if(await page.locator('.user-center').count())throw Error('Late user response mounted into governance')
    role='user'
    platformGroupCount=150
    await page.evaluate(()=>sessionStorage.setItem('wsl:console:d3000000-0000-4000-8000-000000000001','admin'))
    await page.goto(base+'/#overview');await page.reload()
    await page.getByText('未开通套餐',{exact:true}).waitFor()
    if(await page.getByRole('button',{name:'切换到管理后台',exact:true}).count())throw Error('Customer obtained governance switch')
    if(await page.locator('.user-center').getByText('超管专属 · 不限额',{exact:true}).count())throw Error('Customer obtained operator demo entitlement')
    if(!(await page.locator('.uc-groups').getByRole('button',{name:'群管助手',exact:true}).first().isDisabled()))throw Error('Missing customer package allowed assistant')
    if(errors.length)throw Error('Browser runtime error: '+errors[0])
    return {passed:true,synthetic_only:true,groups:[100,300],page_size:20,group_queries:groupQueries.length,
      platforms:['wsl','lajiaoliao'],viewports:[320,390,768,1440],closed_gate:true,dynamic_gate:true,
      usage_currency:'USDT',usage_currency_scale:1000000000,legacy_money_units:['micro_credit','micro_usd','micro_usdt'],
      plans:planCount,approvals,cancellations,target_queries:targetQueries,
      late_response:delayed>0,forbidden_writes:forbidden,native_writes:0,model_calls:0}
  }finally{page.off('pageerror',listener);await page.unroute('**/api/v1/**',handler)}
}
