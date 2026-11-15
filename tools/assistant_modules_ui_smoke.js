// Built product assets; synthetic HTTP only, no model or native platform.
async (page) => {
 const base='http://127.0.0.1:5173', actor='da670000-0000-4000-8000-000000000001'
 const policies={wsl:{platform:'wsl',version:1,capabilities:[]},lajiaoliao:{platform:'lajiaoliao',version:1,capabilities:[]}}
 const catalog=[{id:'blocked_words.query',name:'拦截词',category:'query',implemented:true},{id:'blocked_words.config_propose',name:'拦截词',category:'config_propose',implemented:true}]
 let writes=0,conflict=false
 const screenshots=[], errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 const handler=async route=>{
  const req=route.request(),path=new URL(req.url()).pathname.replace('/api/v1',''),platform=req.headers()['x-workspace']||'wsl'
  let data=[],status=200,msg=''
  if(path==='/me')data={user:{id:actor,role:'superadmin',username:'合成超管'},permissions:['overview:read','accounts:read','groups:read','assistant:use','audit:read']}
  else if(path==='/workspaces')data={items:[{id:'wsl',name:'旺商聊机器人'},{id:'lajiaoliao',name:'辣椒聊机器人'}]}
  else if(path==='/overview')data={accounts:0,groups:0,limits:{accounts:10,groups:300}}
  else if(path==='/assistant/settings')data={enabled:false,tenant_access:false,version:1}
  else if(path==='/assistant/health')data={sampled_at:new Date().toISOString()}
  else if(path==='/assistant/module-exposure'){
   if(req.method()==='PUT'){
    const body=req.postDataJSON();writes++
    if(conflict || body.version!==policies[platform].version){status=409;msg='version_conflict'}
    else policies[platform]={...policies[platform],version:body.version+1,capabilities:body.capabilities}
   }
   data={...policies[platform],catalog,catalog_version:'synthetic-agent-v1'}
  }else if(req.method()!=='GET')throw Error('Unexpected write '+path)
  await route.fulfill({status,json:{code:status===200?0:1,msg,data}})
 }
 await page.route('**/api/v1/**',handler)
 try{
  await page.addInitScript(actor=>{sessionStorage.setItem(`wsl:workspace:${actor}`,'wsl')},actor)
  await page.goto(base+'/?fixture=agent-modules#assistant-admin')
  const region=page.getByRole('region',{name:'助手模块开放'})
  await region.getByRole('button',{name:'编辑开放范围'}).waitFor()
  for(const width of [320,390,768,1440]){
   await page.setViewportSize({width,height:900})
   await region.getByRole('button',{name:'编辑开放范围'}).click()
   const dialog=page.getByRole('dialog',{name:'旺商聊 · 助手模块开放',exact:true})
   await dialog.waitFor()
   await dialog.getByText('拦截词 · 查询',{exact:true}).click()
   const box=await dialog.boundingBox();if(!box || box.x<0 || box.x+box.width>width+1)throw Error('Dialog overflow '+width)
   const file=`output/playwright/assistant-module-policy-${width}.png`;await page.screenshot({path:file,fullPage:true,animations:'disabled'});screenshots.push(file)
   await dialog.getByRole('button',{name:'取消',exact:true}).click()
  }
  if(writes)throw Error('Viewing or closing submitted policy')
  await region.getByRole('button',{name:'编辑开放范围'}).click()
  let dialog=page.getByRole('dialog',{name:'旺商聊 · 助手模块开放',exact:true})
  await dialog.getByText('拦截词 · 查询',{exact:true}).click()
  await dialog.getByRole('button',{name:'保存开放范围'}).click()
  await dialog.waitFor({state:'hidden'})
  if(writes!==1 || policies.wsl.capabilities.join()!=='blocked_words.query' || policies.lajiaoliao.capabilities.length)throw Error('Policy crossed category/platform')
  await region.getByRole('radio',{name:'辣椒聊',exact:true}).locator('..').click()
  await region.getByRole('button',{name:'编辑开放范围'}).click()
  dialog=page.getByRole('dialog',{name:'辣椒聊 · 助手模块开放',exact:true})
  if(await dialog.getByRole('checkbox').first().isChecked())throw Error('Policy draft crossed platforms')
  conflict=true
  await dialog.getByRole('button',{name:'保存开放范围'}).click()
  await dialog.getByRole('alert').filter({hasText:'配置已变化'}).waitFor()
  await dialog.getByRole('button',{name:'取消',exact:true}).click()
  if(errors.length)throw Error(errors.join(';'))
  return {passed:true,synthetic_only:true,viewports:[320,390,768,1440],no_model_calls:true,no_native_actions:true,writes,screenshots}
 }finally{await page.unroute('**/api/v1/**',handler)}
}
