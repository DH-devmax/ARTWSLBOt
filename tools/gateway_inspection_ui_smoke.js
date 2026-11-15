// All upstream/model and administrative requests are synthetic.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `bd000000-0000-4000-8000-${String(n).padStart(12,'0')}`
  const upstream = {rate_id:id(1),checked_at:'2026-10-30T08:00:00Z',usage_status:'ok',budget_kind:'wallet',currency:'USD',remaining:'58.2',
    usd_usdt_parity:true,credit_ceiling_micro:58200000000,
    models_status:'ok',model_available:true,pricing_status:'ok',pricing_currency:'CNY',mapping_status:'model_price_missing'}
  let mode='admin', inspected=false, parity=false
  const writes=[], screenshots=[]
  const handler=async route=>{
    const req=route.request(),path=new URL(req.url()).pathname.replace('/api/v1','')
    let data=[],status=200
    if(req.method()!=='GET'){
      writes.push({path,body:req.postDataJSON()})
      if(path==='/ai-gateway/inspect'){data=upstream;inspected=true}
      else if(path==='/ai-gateway/test'){data={id:id(6),status:'unknown',charged_micro:0};}
      else if(path==='/ai-gateway'&&req.method()==='PUT'){
        const body=req.postDataJSON()
        if(body.settings.enabled||body.settings.platform_limit_micro||body.settings.test_limit_micro||body.route.enabled||body.route.verified)
          throw new Error('currency confirmation expanded financial scope')
        parity=body.route.usd_usdt_parity===true
        data={}
      }
      else throw new Error(`unexpected mutation ${path}`)
    }else if(path==='/me')data={user:{id:id(2),role:mode==='admin'?'superadmin':'user',tenant_id:mode==='admin'?undefined:id(3),
      ...(mode==='admin'?{}:{tenant_role:'owner',owner_id:id(2)})},
      permissions:mode==='admin'?['overview:read','accounts:read','groups:read','members:manage','plans:manage','workspaces:maibot']:['overview:read','groups:read','members:manage','assistant:use']}
    else if(path==='/workspaces')data={items:[{id:'wsl',name:'旺商聊机器人'},{id:'user',name:'用户中心'}]}
    else if(path==='/ai-gateway')data={settings:{enabled:false,platform_limit_micro:0,test_limit_micro:0,version:1},
      provider:{base_url:'https://synthetic.example/v1',model:'synthetic',has_key:true,usd_usdt_parity:parity},
      runtime:{routes:[{rate_id:id(1),enabled:false,verified:false,rates:{input:0,output:0,cache_read:0,cache_write:0}}],calls:[],upstream:inspected?upstream:null}}
    else if(path==='/member-roles')data={tenant_id:id(3),roles:[],
      capability_ceiling:['assistant:use','assistant:propose','assistant:approve'],
      permissions:['assistant:use','assistant:propose','assistant:approve'].map(code=>({code,label:code,module:'management_assistant'})),
      assistant_templates:[{code:'assistant_reader',name:'助手只读',version:1,permissions:['assistant:use']}]}
    else if(path==='/user-center')data={groups:[],subscription:{modules:[]},claims:[],operations:[]}
    else if(path==='/overview')data={accounts:0,groups:0,limits:{accounts:10,groups:300}}
    await route.fulfill({status,json:{code:0,msg:'',data}})
  }
  await page.route('**/api/v1/**',handler)
  const navigate=async hash=>{await page.goto(base+'/#'+hash);await page.reload();await page.waitForTimeout(150)}
  try{
    await page.goto(base);await page.evaluate(()=>sessionStorage.clear())
    for(const width of [320,390,647,1440]){
      await page.setViewportSize({width,height:1000})
      await navigate('gateway')
      await page.getByRole('heading',{name:'统一 AI 网关',exact:true}).waitFor()
      const before=writes.length
      await page.getByRole('button',{name:'查询上游',exact:true}).click()
      await page.getByText('58.2 USD',{exact:true}).waitFor()
      await page.getByText('1 USD = 1 USDT',{exact:true}).waitFor()
      if(writes.length!==before+1||writes.at(-1).path!=='/ai-gateway/inspect')throw new Error('read-only inspection made extra calls')
      if(await page.getByLabel('模型 HTTPS 来源',{exact:true}).inputValue()!=='https://synthetic.example/v1')throw new Error('inspection modified route')
      if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw new Error('gateway overflow '+width)
      const shot=`output/playwright/gateway-upstream-${width}.png`
      await page.screenshot({path:shot,fullPage:true});screenshots.push(shot)
    }
    await page.getByText('USD 按 1:1 USDT 记账',{exact:true}).click()
    await page.getByRole('button',{name:'保存网关配置',exact:true}).click()
    await page.waitForTimeout(150)
    if(!parity||!await page.getByRole('checkbox',{name:'USD 按 1:1 USDT 记账',exact:true}).isChecked())
      throw new Error('currency policy was not persisted and restored')
    await page.getByRole('button',{name:'单次测试',exact:true}).click()
    const dialog=page.getByRole('dialog',{name:'单次网关测试',exact:true})
    await dialog.getByRole('button',{name:'执行测试',exact:true}).click()
    await dialog.getByRole('alert').filter({hasText:'未知 / 冻结'}).waitFor()
    const first=writes.at(-1)
    await dialog.getByRole('button',{name:'执行测试',exact:true}).click()
    await page.waitForTimeout(150)
    const second=writes.at(-1)
    if(first.body.idempotency_key!==second.body.idempotency_key || first.body.max_cost_micro!==1000000 ||
      Object.keys(first.body).sort().join(',')!=='idempotency_key,max_cost_micro,rate_id')throw new Error('unknown call replay key/scope changed')
    await dialog.getByRole('button',{name:'关闭',exact:true}).click()
    mode='owner'
    await page.evaluate(()=>sessionStorage.clear())
    await navigate('members')
    await page.getByRole('button',{name:'角色权限',exact:true}).click()
    const role=page.getByRole('dialog',{name:'角色权限',exact:true})
    await role.locator('.el-form-item').filter({hasText:'助手权限模板'}).locator('.el-select__wrapper').click()
    await page.getByRole('option',{name:'助手只读',exact:true}).click()
    if(await role.getByLabel('标识',{exact:true}).inputValue()!=='assistant_reader')throw new Error('template not applied to draft')
    if(await role.getByLabel('名称',{exact:true}).inputValue()!=='助手只读')throw new Error('template label')
    if(writes.some(x=>x.path.startsWith('/member-roles')))throw new Error('template selection silently persisted role')
    await role.getByRole('button',{name:'关闭',exact:true}).click()
    const before=writes.length
    await navigate('gateway')
    await page.waitForTimeout(200)
    if(await page.getByRole('heading',{name:'统一 AI 网关',exact:true}).count() || writes.length!==before)throw new Error('tenant received superadmin gateway')
    return {passed:true,synthetic_only:true,real_model_calls:0,real_native_actions:0,viewports:[320,390,647,1440],currency_policy_persisted:true,unknown_key_preserved:true,template_draft_only:true,screenshots}
  }finally{await page.unroute('**/api/v1/**',handler)}
}
