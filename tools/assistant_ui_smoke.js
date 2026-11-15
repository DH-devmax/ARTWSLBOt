// Synthetic identities/events only; the runner blocks every external origin.
// prettier-ignore
async (page) => {
  const base = new URL(page.url()).origin
  const id = n => `a1000000-0000-4000-8000-${String(n).padStart(12,'0')}`
  const [admin,aid,bid,ga,gb,pid] = [1,2,3,4,5,6].map(id)
  const accounts = [{id:aid,name:'助手测试甲',native_nickname:'原生甲',workspace:'wsl'},{id:bid,name:'助手测试乙',native_nickname:'原生乙',workspace:'wsl'}]
  const chatAccount={id:id(7),name:'热群备注',native_nickname:'原生热群号',native_uid:'987',workspace:'maibot',platform:'wsl',purpose:'chat'}
  const groups = [
    {group_id:ga,name:'隔离测试甲群',primary_account_id:aid,ai_account_id:chatAccount.id,ai_account_name:chatAccount.native_nickname,active:true,version:1,config:{}},
    {group_id:gb,name:'隔离测试乙群',primary_account_id:bid,active:true,version:1,config:{}}
  ]
  const grants = ['overview:read','accounts:read','groups:read','groups:configure','groups:ai','assistant:use','assistant:propose','assistant:approve','personas:read','personas:manage','workspaces:maibot']
  let permitted=true, turnPosts=0, approvals=0, sessionPosts=0, eventReads=0, identityReads=0
  let delayHistory=false, releaseHistory, historyRequested
  const sessions=[], turns=new Map(), events=new Map()
  const now=new Date().toISOString()
  const plan={id:pid,account_id:aid,group_id:ga,base_version:1,version:1,digest:'synthetic-digest',
    expires_at:new Date(Date.now()+600000).toISOString(),status:'pending_approval',applied:false,
    before:{blocked_words:{words:[]}},changes:{blocked_words:{words:['测试词']}},message:'仅为建议；尚未保存配置'}
  const malicious='仅为建议 <img src=x onerror="window.__assistantXSS=1">'
  const card=(type,data,scope)=>({type,data,scope,sampled_at:now,verification:'suggestion_only',truncated:false})
  const handler=async route=>{
    const req=route.request(),u=new URL(req.url()),path=u.pathname.replace('/api/v1','')
    let data=[],status=200
    if(req.method()!=='GET'){
      if(!path.startsWith('/assistant/'))throw new Error('Assistant attempted unrelated/native write')
      const body=req.postDataJSON()
      if(path==='/assistant/sessions'){
        sessionPosts++
        let item=sessions.find(s=>s.is_empty&&JSON.stringify(s.scope)===JSON.stringify(body))
        if(!item){
          item={id:id(100+sessionPosts),scope:body,is_empty:true,title:'',created_at:new Date(Date.now()+sessionPosts*1000).toISOString()}
          sessions.unshift(item)
        }
        data=item;status=201
      }else if(/^\/assistant\/sessions\/[^/]+\/turns$/.test(path)){
        if(!req.headers()['idempotency-key'])throw new Error('Missing idempotency key')
        turnPosts++
        const sid=path.split('/')[3],session=sessions.find(s=>s.id===sid)
        if(!session)throw new Error('Turn lost its session scope')
        if(session.is_empty){session.title=body.input;session.is_empty=false}
        const item={id:id(200+turnPosts),session_id:sid,status:'succeeded',result_code:'',input:body.input,created_at:now}
        turns.set(item.id,item)
        const es=[
          {kind:'turn_started',data:{status:'running'}},
          {kind:'text_delta',data:{text:turnPosts===1?malicious:`答复 ${body.input}`}}
        ]
        if(turnPosts===1){
          plan.session_id=sid
          es.push({kind:'tool_result',data:card('propose_group_config_change',plan,session.scope)})
          es.push({kind:'tool_result',data:card('list_authorized_groups',[
            {id:gb,name:'隔离测试乙群',account_id:bid},
            {id:id(999),name:'越权伪造群',account_id:bid}
          ],session.scope)})
        }
        es.push({kind:'turn_finished',data:{status:'succeeded',code:''}})
        events.set(item.id,es.map((e,i)=>({...e,turn_id:item.id,seq:i+1})))
        data={...item,status:'queued'};status=202
      }else if(path===`/assistant/plans/${pid}/approve`){
        approvals++
        if(body.digest!==plan.digest||body.version!==plan.version)throw new Error('Approval lost server binding')
        plan.status='committed';plan.version=2;plan.execution_id='execution-fixture'
        plan.message='配置已保存，实际生效尚未验证';data=plan
      }else throw new Error('Unexpected assistant write '+path)
    }else if(path==='/me'){
      identityReads++
      data={user:{id:admin,username:'synthetic-user',role:'superadmin'},permissions:permitted?grants:grants.filter(p=>!p.startsWith('assistant:'))}
    }
    else if(path==='/workspaces')data={items:[{id:'user',name:'用户中心'},{id:'wsl',name:'旺商聊机器人'},{id:'maibot',name:'热群机器人'}]}
    else if(path==='/accounts')data=req.headers()['x-workspace']==='maibot'?[chatAccount]:accounts
    else if(path==='/groups')data=groups
    else if(path==='/personas')data=[{account_id:chatAccount.id,account_name:chatAccount.name,native_nickname:chatAccount.native_nickname,nickname:'人设测试',personality:'合成设定',version:1}]
    else if(path==='/overview')data={accounts:2,groups:2,limits:{accounts:10,groups:300,ai_groups:300}}
    else if(path==='/assistant/context')data={platform:'wsl',groups:groups.map(g=>({...g,platform:'wsl',robot_nickname:accounts.find(a=>a.id===g.primary_account_id)?.native_nickname,native_id:g.group_id})),total:2,next_offset:-1,permissions:grants,actions:[],
      ...(u.searchParams.get('group_id')?{scope:{platform:'wsl',kind:'group',group_id:u.searchParams.get('group_id'),account_id:groups.find(g=>g.group_id===u.searchParams.get('group_id'))?.primary_account_id}}:{})}
    else if(path==='/assistant/usage')data={month:'2026-10',used_or_reserved:30,timezone:'Asia/Shanghai'}
    else if(path==='/assistant/sessions')data=sessions.filter(s=>Object.entries(s.scope).every(([k,v])=>u.searchParams.get(k)===v))
    else if(/^\/assistant\/sessions\/[^/]+\/transcript$/.test(path)){
      if(delayHistory){
        delayHistory=false
        historyRequested()
        await new Promise(resolve=>{releaseHistory=resolve})
      }
      data=[...turns.values()].filter(t=>t.session_id===path.split('/')[3]).slice(-10).map(t=>({
        ...t,answer:events.get(t.id).filter(e=>e.kind==='text_delta').map(e=>e.data.text).join(''),
        cards:events.get(t.id).filter(e=>e.kind==='tool_result').map(e=>e.data),cursor:events.get(t.id).at(-1).seq
      }))
    }else if(path===`/assistant/plans/${pid}`)data=plan
    else if(/^\/assistant\/turns\/[^/]+\/events$/.test(path)){
      eventReads++
      const es=events.get(path.split('/')[3])||[],after=Number(u.searchParams.get('after')||0)
      await route.fulfill({contentType:'text/event-stream; charset=utf-8',body:es.filter(e=>e.seq>after).map(e=>`id: ${e.seq}\nevent: ${e.kind}\ndata: ${JSON.stringify(e)}\n\n`).join('')})
      return
    }else if(/^\/assistant\/turns\/[^/]+$/.test(path))data=turns.get(path.split('/')[3])
    await route.fulfill({status,json:{code:0,msg:'ok',data}})
  }
  const choose=async name=>{
    await page.getByRole('combobox',{name:'机器人面板',exact:true}).locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option').filter({hasText:name}).click()
  }
  const close=async()=>{
    await page.keyboard.press('Escape')
    await page.locator('.assistant-drawer').waitFor({state:'hidden'})
  }
  const saved=await page.evaluate(()=>Object.fromEntries(Object.entries(sessionStorage).filter(([k])=>k.startsWith('wsl:'))))
  await page.route('**/api/v1/**',handler)
  try{
    await page.evaluate(()=>sessionStorage.clear())
    await page.setViewportSize({width:1440,height:960})
    await page.goto(base+'/#groups');await page.reload();await choose('助手测试甲')
    await page.locator('.topbar-actions').getByRole('button',{name:'群管助手',exact:true}).click()
    const drawer=page.locator('.assistant-drawer'),input=drawer.getByRole('textbox',{name:'助手问题',exact:true})
    await input.fill('甲账号草稿')
    await close();await choose('助手测试乙')
    await page.locator('.topbar-actions').getByRole('button',{name:'群管助手',exact:true}).click()
    if(await input.inputValue()!=='')throw new Error('Account draft leaked')
    await close();await choose('助手测试甲')
    await page.locator('.topbar-actions').getByRole('button',{name:'群管助手',exact:true}).click()
    if(await input.inputValue()!=='甲账号草稿')throw new Error('Account draft lost')
    await drawer.locator('.assistant-scope').getByRole('button',{name:/选择群组 隔离测试甲群/}).click()
    await input.fill('刷新期间保留草稿')
    const previousIdentityReads=identityReads
    await page.waitForTimeout(15500)
    if(identityReads<=previousIdentityReads)throw new Error('Periodic identity refresh was not exercised')
    if(await input.inputValue()!=='刷新期间保留草稿'||await input.getAttribute('placeholder')!=='管理「隔离测试甲群」…')
      throw new Error('Unchanged identity refresh cleared assistant scope or draft')
    await input.fill('检查本群配置')
    await drawer.getByRole('button',{name:'发送',exact:true}).click()
    await drawer.getByText(malicious,{exact:true}).waitFor()
    await page.waitForFunction(()=>!document.querySelector('.assistant-drawer .assistant-progress'))
    if(await page.evaluate(()=>window.__assistantXSS===1)||await drawer.locator('img[src=x]').count())throw new Error('Model markup executed')
    if(!await drawer.locator('.elx-bubble').count())throw new Error('Element Plus X bubbles missing')
    if(await drawer.getByRole('button',{name:/越权伪造群/}).count())throw new Error('Untrusted group card became a target')
    const screenshots=[]
    for(const width of [320,390,647,1440]){
      await page.setViewportSize({width,height:900});await page.waitForTimeout(150)
      const box=await input.boundingBox(),send=await drawer.getByRole('button',{name:'发送',exact:true}).boundingBox()
      if(!box||!send||box.x<0||box.x+box.width>width+1||send.y+send.height>900)throw new Error(`Drawer overflow ${width} ${JSON.stringify({box,send})}`)
      const path=`output/playwright/assistant-${width}.png`;await page.screenshot({path});screenshots.push(path)
    }
    await drawer.getByRole('button',{name:'查看配置差异',exact:true}).click()
    const approval=page.getByRole('dialog',{name:'配置建议审批',exact:true})
    await approval.getByRole('button',{name:'确认保存配置',exact:true}).click()
    await approval.getByText('配置已保存，实际生效尚未验证',{exact:true}).waitFor()
    await page.keyboard.press('Escape')
    await drawer.locator('.assistant-result').getByRole('button',{name:/选择群组 隔离测试乙群/}).click()
    await drawer.locator('.assistant-welcome').waitFor()
    if(sessionPosts!==1||turnPosts!==1)throw new Error('Group click created a paid turn/session')
    await input.fill('乙群草稿')
    await page.setViewportSize({width:1440,height:960})
    await drawer.locator('.assistant-scope').getByRole('button',{name:/选择群组 隔离测试甲群/}).click()
    await drawer.getByText(malicious,{exact:true}).waitFor()
    if(await input.inputValue()!=='')throw new Error('Group draft leaked')
    if(eventReads!==1)throw new Error('Completed history replayed stream')
    await drawer.getByRole('button',{name:'新对话',exact:true}).click()
    await drawer.locator('.assistant-conversation-title').getByText('新对话',{exact:true}).waitFor()
    const emptyID=sessions.find(s=>s.is_empty)?.id
    if(!emptyID||sessions.length!==2)throw new Error('New conversation was not materialized')
    await input.fill('空白会话草稿')
    for(let i=0;i<3;i++){
      await drawer.getByRole('button',{name:'新对话',exact:true}).click()
      await page.waitForFunction(()=>!document.querySelector('.assistant-drawer .el-skeleton'))
    }
    if(sessions.length!==2||sessions.find(s=>s.is_empty)?.id!==emptyID)throw new Error('Empty conversations accumulated')
    if(await input.inputValue()!=='空白会话草稿')throw new Error('Reusing an empty conversation erased its draft')
    for(let n=0;n<11;n++){
      await input.fill('连续问答 '+n)
      await input.press('Enter')
      await drawer.getByText('答复 连续问答 '+n,{exact:true}).waitFor()
      await page.waitForFunction(()=>!document.querySelector('.assistant-drawer .assistant-progress'))
    }
    if(await drawer.locator('.assistant-turn').count()!==10||await drawer.locator('.assistant-thread').getByText('连续问答 0',{exact:true}).count())throw new Error('Rolling UI exceeded ten rounds')
    await drawer.getByRole('button',{name:'历史对话',exact:true}).click()
    await page.locator('.assistant-history-drawer .assistant-history-select').first().waitFor()
    if(!await page.locator('.assistant-history-drawer .assistant-history-select').first().innerText().then(value=>value.startsWith('连续问答 0')))throw new Error('History title did not retain the first input')
    await page.locator('.assistant-history-drawer .assistant-history-select').first().click()
    await drawer.getByText('答复 连续问答 10',{exact:true}).waitFor()
    if(turnPosts!==12||approvals!==1)throw new Error('History regenerated or reapplied')
    await drawer.locator('.assistant-scope').getByRole('button',{name:/选择群组 隔离测试乙群/}).click()
    await drawer.locator('.assistant-welcome').waitFor()
    const requested=new Promise(resolve=>{historyRequested=resolve})
    delayHistory=true
    await drawer.locator('.assistant-scope').getByRole('button',{name:/选择群组 隔离测试甲群/}).click()
    await requested
    await drawer.locator('.assistant-scope').getByRole('button',{name:/选择群组 隔离测试乙群/}).click()
    releaseHistory()
    await drawer.locator('.assistant-welcome').waitFor()
    if(await drawer.getByText('答复 连续问答 10',{exact:true}).count())throw new Error('Late transcript leaked across groups')
    await close()
    await page.goto(base+'/#assistant')
    if(await page.locator('.page-heading').count())throw new Error('Duplicate assistant page heading remains')
    const embedded=page.getByRole('region',{name:'助手会话工作台'})
    await embedded.locator('.assistant-scope').getByRole('button',{name:/选择群组 隔离测试甲群/}).click()
    await embedded.getByText('答复 连续问答 10',{exact:true}).waitFor()
    for(const width of [320,390,768,1440]){
      await page.setViewportSize({width,height:1000});await page.waitForTimeout(100)
      if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw new Error('Embedded overflow '+width)
      const path=`output/playwright/assistant-workspace-${width}.png`
      await page.screenshot({path,fullPage:true});screenshots.push(path)
    }
    if(!await embedded.locator('.assistant-scope small').filter({hasText:'原生甲 · 助手测试甲'}).count())throw new Error('Native nickname mapping missing')
    await embedded.getByRole('button',{name:'新对话',exact:true}).click()
    await embedded.locator('.assistant-conversation-title').getByText('新对话',{exact:true}).waitFor()
    const blank=sessions.find(s=>s.is_empty&&s.scope.group_id===ga)
    if(!blank||sessions.length!==3)throw new Error('New conversation did not leave populated history')
    await embedded.getByRole('button',{name:'历史对话',exact:true}).click()
    await page.locator('.assistant-history-drawer .assistant-history-select').filter({hasText:'连续问答 0'}).last().click()
    await embedded.getByText('答复 连续问答 10',{exact:true}).waitFor()
    await embedded.getByRole('button',{name:'新对话',exact:true}).click()
    await embedded.locator('.assistant-conversation-title').getByText('新对话',{exact:true}).waitFor()
    if(sessions.length!==3||sessions.find(s=>s.is_empty&&s.scope.group_id===ga)?.id!==blank.id)throw new Error('New button did not select existing empty conversation')
    await page.goto(base+'/#groups')
    await page.getByRole('row').filter({hasText:'隔离测试甲群'}).getByRole('button',{name:'群配置',exact:true}).click()
    const groupDialog=page.getByRole('dialog',{name:'隔离测试甲群',exact:true})
    await groupDialog.getByText('群内热群机器人',{exact:true}).waitFor()
    await groupDialog.getByText('原生热群号',{exact:true}).waitFor()
    if(await groupDialog.getByText('群 AI',{exact:true}).count())throw new Error('Ambiguous group AI label remains')
    await page.keyboard.press('Escape')
    await page.getByRole('button',{name:'切换工作台',exact:true}).click()
    await page.getByRole('dialog',{name:'选择面板',exact:true}).getByRole('button',{name:'热群机器人',exact:true}).click()
    await page.goto(base+'/#personas')
    await page.getByRole('button',{name:'编辑',exact:true}).click()
    const personaDialog=page.getByRole('dialog',{name:'原生热群号 · 热群备注 · 热群人设',exact:true})
    await personaDialog.getByText('旺商聊 · 987',{exact:true}).waitFor()
    await personaDialog.getByText('群内热群机器人',{exact:true}).waitFor()
    await page.keyboard.press('Escape')
    await page.getByRole('button',{name:'切换工作台',exact:true}).click()
    await page.getByRole('dialog',{name:'选择面板',exact:true}).getByRole('button',{name:'旺商聊机器人',exact:true}).click()
    permitted=false;await page.reload()
    if(await page.getByRole('button',{name:'群管助手',exact:true}).count())throw new Error('Unentitled entry exposed')
    return {passed:true,synthetic_only:true,model_calls:0,native_writes:0,turn_posts:turnPosts,approvals,
      cross_account_buttons:true,history_no_replay:true,ten_round_window:true,late_history_isolated:true,
      empty_session_reused:true,first_input_title:true,native_nickname:true,assistant_heatbot_boundaries:true,
      persona_native_identity:true,duplicate_heading_removed:true,periodic_identity_refresh_preserved:true,viewports:[320,390,647,1440],screenshots}
  }catch(error){
    await page.screenshot({path:'output/playwright/assistant-failure.png',fullPage:true})
    console.error(await page.locator('.assistant-drawer').evaluate(el=>({
      bounds:el.getBoundingClientRect().toJSON(),
      body:el.querySelector('.el-drawer__body')?.getBoundingClientRect().toJSON(),
      footer:el.querySelector('.el-drawer__footer')?.getBoundingClientRect().toJSON(),
      input:el.querySelector('textarea')?.getBoundingClientRect().toJSON()
    })).catch(()=>({})))
    throw error
  }finally{
    await page.unroute('**/api/v1/**',handler)
    await page.evaluate(values=>{
      for(const key of Object.keys(sessionStorage))if(key.startsWith('wsl:'))sessionStorage.removeItem(key)
      for(const[key,value]of Object.entries(values))sessionStorage.setItem(key,value)
    },saved)
  }
}
