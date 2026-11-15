// Built product assets, synthetic identities and API responses only.
async (page) => {
  const base='http://127.0.0.1:5173'
  const id=n=>`bc000000-0000-4000-8000-${String(n).padStart(12,'0')}`
  let mode='owner',conflict=false
  const writes=[],screenshots=[]
  const grants=['overview:read','accounts:read','groups:read','members:manage','billing:read']
  const owner={id:id(1),tenant_id:id(2),username:'tenant-owner',role:'user',tenant_role:'owner'}
  const group={group_id:id(3),name:'已授权群',platform:'wsl',active:true,version:1}
  const plans=[{id:id(4),code:'PLUS',version:1,enabled:true,price_micro_usdt:29000000,credits_micro:0,group_limit:1,member_limit:3},
    {id:id(5),code:'PRO',version:1,enabled:true,price_micro_usdt:49000000,credits_micro:10000000000,group_limit:3,member_limit:10}]
  const members=[{...owner,disabled:false,access_version:1,groups:[],overrides:[]},
    {id:id(6),username:'reader',tenant_role:'viewer',disabled:false,access_version:1,groups:[],overrides:[]}]
  const handler=async route=>{
    const req=route.request(),path=new URL(req.url()).pathname.replace('/api/v1','')
    let data=[],status=200
    if(req.method()!=='GET'){
      if(!['/members',`/members/${id(6)}`].includes(path))throw new Error(`Unexpected commercial UI write ${path}`)
      const body=req.postDataJSON()
      if('tenant_id'in body||'user_id'in body)throw new Error('Client attempted trusted member scope')
      writes.push({path,body})
      if(conflict)return route.fulfill({status:409,json:{code:1,msg:'version_conflict'}})
      data={id:id(7),version:1};status=path==='/members'?201:200
    }else if(path==='/me')data={user:mode==='admin'?{id:id(8),username:'admin',role:'superadmin'}:mode==='member'?{...members[1],role:'user',tenant_id:id(2)}:owner,
      permissions:mode==='admin'?[...grants,'users:manage','plans:manage','workspaces:maibot']:mode==='member'?['overview:read','groups:read']:grants}
    else if(path==='/workspaces')data={items:mode==='admin'?[{id:'wsl',name:'旺商聊机器人'},{id:'user',name:'用户中心'}]:[{id:'user',name:'用户中心'}]}
    else if(path==='/members')data=members
    else if(path==='/member-roles')data={tenant_id:id(2),capability_ceiling:grants,groups:[group],
      roles:[{code:'owner',name:'主账号',permissions:grants,version:1},{code:'viewer',name:'只读',permissions:['groups:read'],version:1}],
      permissions:grants.map(code=>({code,label:code}))}
    else if(path==='/user-center')data={subscription:{name:'PRO',group_limit:3,ai_group_limit:3,member_limit:10,active:true,modules:[],billing_mode:'credits'},
      groups:[group],personas:[],permissions:grants,claims:[],operations:[],credit_wallet:mode==='owner'?{available_micro:9000000000}:null}
    else if(path==='/user-center/groups')data={items:[group],total:1,page:1,page_size:20}
    else if(path==='/user-center/usage')data={call_count:3,actual_micro:500000,held_micro:500000,sampled_at:new Date().toISOString()}
    else if(path==='/billing/plans')data=plans
    else if(path==='/billing')data={full_bill:mode!=='member',wallet:mode==='member'?null:{granted_micro:10000000000,available_micro:9000000000,spent_micro:500000000,held_micro:500000000},
      periods:[{id:id(9),code:'PRO',state:'active',starts_at:'2026-10-29T00:00:00Z',ends_at:'2026-11-28T00:00:00Z'}],
      limits:[],calls:[{id:id(10),purpose:'heatbot',status:'unknown',created_at:'2026-10-29T01:00:00Z',reserved_micro:500000000}],
      entries:[{kind:'grant',amount_micro:10000000000,created_at:'2026-10-29T00:00:00Z'}]}
    else if(path==='/ai-gateway')data={settings:{enabled:false,platform_limit_micro:0,test_limit_micro:0,version:0},runtime:{routes:[],calls:[]}}
    else if(path==='/overview')data={accounts:0,groups:0,limits:{accounts:10,groups:300}}
    else if(path==='/users')data=[{...owner,tenant_name:'合成租户'}]
    await route.fulfill({status,json:{code:0,msg:'',data}})
  }
  await page.route('**/api/v1/**',handler)
  let navigationSerial=0
  const navigate=async key=>{
    await page.evaluate(({actor,admin})=>{
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:workspace:${actor}`,'wsl')
      sessionStorage.setItem(`wsl:console:${actor}`,admin?'admin':'user')
    },{actor:mode==='admin'?id(8):mode==='member'?id(6):owner.id,admin:mode==='admin'})
    // A new document lets restore capture this hash before asynchronous identity reads.
    await page.goto(`${base}/?synthetic-navigation=${++navigationSerial}#${key}`)
    await page.locator('.platform-shell').waitFor()
  }
  const fits=async(label,width)=>{
    const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1)
    if(overflow)throw new Error(`Commercial page overflow ${label}/${width}`)
    const path=`output/playwright/commerce-${label}-${width}.png`
    await page.screenshot({path,fullPage:true});screenshots.push(path)
  }
  try{
    await page.goto(base);await page.evaluate(()=>sessionStorage.clear())
    for(const width of [320,390,647,1440]){
      await page.setViewportSize({width,height:900})
      await navigate('members')
      const panel=page.locator('.tenant-members')
      await panel.getByRole('button',{name:'添加成员',exact:true}).waitFor()
      await fits('members',width)
      await panel.getByRole('button',{name:'添加成员',exact:true}).click()
      const drawer=page.getByRole('dialog',{name:'添加成员',exact:true})
      await drawer.getByLabel('用户名',{exact:true}).fill('synthetic-new-member')
      await drawer.getByLabel('网站密码',{exact:true}).fill('synthetic-password')
      await page.waitForTimeout(350)
      await drawer.evaluate(async element=>{await Promise.all(element.getAnimations({subtree:true}).map(animation=>animation.finished.catch(()=>{})))})
      const box=await drawer.locator('.el-dialog__body').boundingBox()
      if(!box||box.x< -1||box.x+box.width>width+1)throw new Error(`Member drawer overflow ${width}: ${JSON.stringify(box)}`)
      conflict=true
      await drawer.getByRole('button',{name:'保存成员',exact:true}).click()
      await drawer.getByRole('alert').waitFor()
      if(await drawer.getByLabel('用户名',{exact:true}).inputValue()!=='synthetic-new-member')throw new Error('Failed save lost member input')
      if(writes.at(-1).body.groups.length||writes.at(-1).body.role!=='viewer')throw new Error('New member got implicit group/admin')
      await drawer.getByRole('button',{name:'关闭',exact:true}).click()
      conflict=false
      await navigate('billing')
      await page.getByRole('heading',{name:'我的使用量',exact:true}).waitFor()
      await page.getByText('冻结费用',{exact:true}).waitFor()
      await fits('billing',width)
    }
    mode='member'
    await page.evaluate(()=>sessionStorage.clear())
    await navigate('billing')
    await page.getByRole('heading',{name:'我的使用量',exact:true}).waitFor()
    if(await page.getByText('冻结 / 待结算',{exact:true}).count()||await page.getByRole('button',{name:'消费上限',exact:true}).count())throw new Error('Member received tenant bill controls')
    if(await page.locator('.nav-item').filter({hasText:'成员与权限'}).count())throw new Error('Member management entry exposed')
    mode='admin'
    await page.evaluate(()=>sessionStorage.clear())
    for(const width of [320,390,647,1440]){
      await page.setViewportSize({width,height:900})
      await navigate('gateway')
      await page.getByRole('heading',{name:'统一 AI 网关',exact:true}).waitFor()
      await fits('gateway',width)
    }
    return{passed:true,synthetic_only:true,paid_calls:0,native_actions:0,viewports:[320,390,647,1440],screenshots}
  }finally{await page.unroute('**/api/v1/**',handler)}
}
