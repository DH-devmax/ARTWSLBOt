// Synthetic platform, group and tool fixtures. All external origins are blocked.
// prettier-ignore
async (page) => {
  const base = new URL(page.url()).origin
  const id = n => `b2000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), now = new Date().toISOString(), planID = id(999)
  const permissions = ['overview:read','accounts:read','groups:read','assistant:use',
    'assistant:action_propose','assistant:action_approve','group_action:mute_member']
  const accounts = ['wsl','lajiaoliao'].flatMap((platform,i) => Array.from({length:5},(_,n)=>({
    id:id(2+i*5+n),platform,workspace:platform,purpose:'management',
    name:`共享号池备注 ${n+1}`,native_nickname:`${platform === 'wsl' ? '旺商聊' : '辣椒聊'}原生机器人 ${n+1}`
  })))
  const groupList = platform => Array.from({length:150},(_,i) => ({
    group_id:id((platform === 'wsl' ? 1000 : 2000)+i),name:i === 0 ? '同名测试群' : `授权群 ${String(i).padStart(3,'0')}`,
    native_id:`native-${platform}-${i}`,platform,active:true,
    primary_account_id:accounts.filter(a=>a.platform===platform)[Math.floor(i/30)].id,
    robot_nickname:accounts.filter(a=>a.platform===platform)[Math.floor(i/30)].native_nickname
  }))
  const sessions = [], turns = new Map(), events = new Map()
  const browserErrors = []
  const onPageError = error => browserErrors.push(error.message)
  page.on('pageerror', onPageError)
  let writes=0, turnsCreated=0, chosen=0, reads=0, platformExpected='wsl', plan
  const handler = async route => {
    const req=route.request(), url=new URL(req.url()), path=url.pathname.replace('/api/v1','')
    const platform=req.headers()['x-workspace'] || 'wsl'
    let data=[]
    if(req.method()!=='GET'){
      if(!path.startsWith('/assistant/'))throw new Error('Unexpected native or unrelated write')
      writes++
      const body=req.postDataJSON()
      if(path==='/assistant/sessions'){
        if(body.platform!==platformExpected || platform!==platformExpected)throw new Error('Session platform leak')
        if(!groupList(platform).some(g=>g.group_id===body.group_id&&g.primary_account_id===body.account_id))throw new Error('Client changed account binding')
        data={id:id(100+sessions.length),scope:body,is_empty:true,title:'',created_at:now};sessions.push(data)
      }else if(/\/sessions\/[^/]+\/turns$/.test(path)){
        turnsCreated++
        const session=sessions.find(s=>s.id===path.split('/')[3])
        session.title=body.input;session.is_empty=false
        const t={id:id(200+turnsCreated),session_id:session.id,status:'succeeded',input:body.input,created_at:now}
        turns.set(t.id,t)
        plan={id:planID,session_id:session.id,scope:session.scope,action:'mute_member',name:'成员禁言',
          target:{id:'member-string-b',name:'同名成员'},minutes:1,version:1,base_version:1,digest:'safe-server-digest',
          expires_at:new Date(Date.now()+600000).toISOString(),status:'blocked',production_admitted:false,result_code:'production_acceptance_required'}
        const tool=(type,data)=>({kind:'tool_result',data:{type,data,scope:session.scope,sampled_at:now,verification:'suggestion_only',truncated:false}})
        const list=[
          {kind:'turn_started',data:{status:'running'}},
          {kind:'text_delta',data:{text:'请先核对成员与当前群组。'}},
          tool('find_group_members',[{id:'member-string-a',name:'同名成员',role:'member',token:'sealed-choice-a'},
            {id:'member-string-b',name:'同名成员',role:'member',token:'sealed-choice-b'}]),
          tool('find_group_messages',[{id:'71',name:'本群近期消息摘要',role:'message',token:'sealed-message-choice'}]),
          tool('propose_group_action',plan),
          {kind:'turn_finished',data:{status:'succeeded',code:''}}
        ]
        events.set(t.id,list.map((e,i)=>({...e,turn_id:t.id,seq:i+1})))
        data={...t,status:'queued'}
      }else if(/\/sessions\/[^/]+\/target$/.test(path)){
        if(!['sealed-choice-b','sealed-message-choice'].includes(body.token))throw new Error('Candidate choice changed')
        chosen++;data=body.token==='sealed-message-choice'
          ? {id:'71',name:'本群近期消息摘要',role:'message',token:body.token}
          : {id:'member-string-b',name:'同名成员',role:'member',token:body.token}
      }else throw new Error('Blocked plan was submitted or unexpected assistant write: '+path)
    }else if(path==='/me')data={user:{id:actor,username:'synthetic-member',role:'superadmin'},permissions}
    else if(path==='/workspaces')data={items:[{id:'wsl',name:'旺商聊机器人'},{id:'lajiaoliao',name:'辣椒聊机器人'}]}
    else if(path==='/accounts')data=accounts.filter(a=>a.platform===platform)
    else if(path==='/groups')data=groupList(platform).slice(0,2)
    else if(path==='/assistant/context'){
      reads++
      const offset=Number(url.searchParams.get('offset')||0),limit=Number(url.searchParams.get('limit')||30),search=url.searchParams.get('search')||''
      if(limit>50)throw new Error('Unbounded context query')
      const all=groupList(platform).filter(g=>!search||g.name.includes(search)), selected=groupList(platform).find(g=>g.group_id===url.searchParams.get('group_id'))
      data={platform,groups:all.slice(offset,offset+limit),total:all.length,next_offset:offset+limit<all.length?offset+limit:-1,
        permissions,actions:[],...(selected?{scope:{platform,kind:'group',account_id:selected.primary_account_id,group_id:selected.group_id}}:{})}
    }else if(path==='/assistant/usage')data={unit:'micro_credit',used_or_reserved:0,timezone:'Asia/Shanghai'}
    else if(path==='/assistant/sessions')data=sessions.filter(s=>s.scope.platform===platform&&s.scope.group_id===url.searchParams.get('group_id'))
    else if(/\/transcript$/.test(path))data=[...turns.values()]
      .filter(turn=>turn.session_id===path.split('/')[3]).slice(-10).map(turn=>{
        const list=events.get(turn.id)||[]
        return {...turn,answer:list.filter(event=>event.kind==='text_delta').map(event=>event.data.text).join(''),
          cards:list.filter(event=>event.kind==='tool_result').map(event=>event.data),
          cursor:list.at(-1)?.seq||0}
      })
    else if(path===`/assistant/action-plans/${planID}`)data=plan
    else if(/\/turns\/[^/]+\/events$/.test(path)){
      if(platform!==platformExpected)throw new Error('SSE crossed platforms')
      const list=events.get(path.split('/')[3])||[]
      await route.fulfill({contentType:'text/event-stream',body:list.map(e=>`id: ${e.seq}\nevent: ${e.kind}\ndata: ${JSON.stringify(e)}\n\n`).join('')})
      return
    }else if(/\/turns\/[^/]+$/.test(path))data=turns.get(path.split('/')[3])
    await route.fulfill({json:{code:0,msg:'ok',data}})
  }
  await page.route('**/api/v1/**',handler)
  const openGroups = async region => {
    const button=region.locator('.assistant-mobile-groups')
    if(await button.isVisible()&&await button.getAttribute('aria-expanded')!=='true')await button.click()
  }
  try{
    await page.evaluate(actor=>{
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`,'user')
    },actor)
    await page.setViewportSize({width:1440,height:960})
    await page.goto(base+'/#assistant');await page.reload()
    let region=page.getByRole('region',{name:'助手会话工作台'})
    const send=()=>region.getByRole('button',{name:'发送',exact:true})
    if(!(await send().isDisabled())||writes!==0)throw new Error('Unselected group started work')
    await region.getByRole('button',{name:'加载更多群组',exact:true}).click()
    if(reads<2||writes!==0)throw new Error('Pagination created a conversation')
    await region.getByRole('textbox',{name:'搜索授权群组',exact:true}).fill('授权群 149')
    const last=region.getByRole('button',{name:/选择群组 授权群 149/})
    await last.waitFor();await last.click()
    await region.getByRole('textbox',{name:'助手问题',exact:true}).fill('旺商聊草稿')
    // The assistant changes platform in place, without opening authorized groups.
    platformExpected='lajiaoliao'
    await region.getByRole('radiogroup',{name:'助手平台',exact:true}).locator('label').filter({hasText:'辣椒聊'}).click()
    await page.waitForFunction(()=>sessionStorage.getItem(`wsl:workspace:b2000000-0000-4000-8000-000000000001`)==='lajiaoliao'&&location.hash==='#assistant')
    region=page.getByRole('region',{name:'助手会话工作台'})
    await region.getByRole('textbox',{name:'搜索授权群组',exact:true}).fill('同名测试群')
    await region.getByRole('button',{name:/选择群组 同名测试群/}).waitFor()
    if(await region.getByRole('textbox',{name:'助手问题',exact:true}).inputValue()!=='')throw new Error('Cross-platform draft leaked')
    await region.getByRole('button',{name:/选择群组 同名测试群/}).click()
    await region.getByRole('textbox',{name:'助手问题',exact:true}).fill('查找同名成员并提出禁言建议')
    await send().click()
    await region.getByText('请先核对成员与当前群组。',{exact:true}).waitFor()
    await page.waitForFunction(()=>!document.querySelector('.assistant-progress'))
    await region.getByRole('button',{name:'选择成员',exact:true}).nth(1).click()
    if(chosen!==1)throw new Error('Human candidate selection missing')
    await region.getByRole('button',{name:'选择消息',exact:true}).click()
    await page.waitForFunction(()=>document.querySelector('textarea[aria-label="助手问题"]')?.value.includes('消息 71'))
    if(chosen!==2 || !(await region.getByRole('textbox',{name:'助手问题',exact:true}).inputValue()).includes('消息 71'))
      throw new Error('Message choice missing or mislabeled as member')
    await region.getByRole('button',{name:'查看动作与影响',exact:true}).click()
    const dialog=page.getByRole('dialog',{name:'确认群管动作',exact:true})
    await dialog.getByText('member-string-b',{exact:false}).waitFor()
    await dialog.getByText('此平台的生产准入尚未完成，当前仅可查看建议',{exact:true}).waitFor()
    if(await dialog.getByRole('button',{name:'确认禁言',exact:true}).count())throw new Error('Unaccepted action exposed confirmation')
    await dialog.evaluate(async el => {
      await Promise.all(el.getAnimations({subtree:true})
        .filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity)
        .map(animation => animation.finished.catch(() => {})))
    })
    for(const width of [320,390,768,1440]){
      await page.setViewportSize({width,height:900})
      await page.waitForTimeout(80)
      const bounds=await dialog.boundingBox()
      if(!bounds||bounds.x<0||bounds.x+bounds.width>width+1)throw new Error('Action dialog overflow '+width)
      await page.screenshot({path:`output/playwright/assistant-action-${width}.png`,fullPage:true,animations:'disabled'})
    }
    await page.keyboard.press('Escape')
    await dialog.waitFor({state:'hidden'})
    for(const width of [320,390,768,1440]){
      await page.setViewportSize({width,height:900})
      if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw new Error('Platform assistant overflow '+width)
      const input=region.getByRole('textbox',{name:'助手问题',exact:true})
      await input.focus()
      if(width<768)await page.setViewportSize({width,height:600})
      await page.waitForTimeout(150)
      const bounds=await input.boundingBox()
      if(!bounds||bounds.y+bounds.height>(width<768?600:900))throw new Error('Composer hidden by short viewport')
      await page.screenshot({path:`output/playwright/assistant-chili-${width}.png`})
    }
    await region.getByRole('textbox',{name:'助手问题',exact:true}).fill('辣椒聊独立草稿')
    const completedWrites=writes
    platformExpected='wsl'
    await region.getByRole('radiogroup',{name:'助手平台',exact:true}).locator('label').filter({hasText:'旺商聊'}).click()
    await page.waitForFunction(()=>location.hash==='#assistant'&&sessionStorage.getItem(`wsl:workspace:b2000000-0000-4000-8000-000000000001`)==='wsl')
    region=page.getByRole('region',{name:'助手会话工作台'})
    await openGroups(region)
    await region.getByRole('textbox',{name:'搜索授权群组',exact:true}).fill('授权群 149')
    await region.getByRole('button',{name:/选择群组 授权群 149/}).click()
    await page.waitForFunction(()=>document.querySelector('textarea[aria-label="助手问题"]')?.value==='旺商聊草稿')
    for (const [index, selected] of ['lajiaoliao','wsl','lajiaoliao','wsl'].entries()) {
      await region.getByRole('textbox',{name:'助手问题',exact:true}).fill('快速切换草稿\n'.repeat(8))
      await page.setViewportSize({width:index%2 ? 768 : 390,height:600})
      platformExpected=selected
      await region.getByRole('radiogroup',{name:'助手平台',exact:true}).locator('label')
        .filter({hasText:selected==='wsl'?'旺商聊':'辣椒聊'}).click()
      await page.waitForFunction(next=>sessionStorage.getItem(`wsl:workspace:b2000000-0000-4000-8000-000000000001`)===next&&location.hash==='#assistant',selected)
      region=page.getByRole('region',{name:'助手会话工作台'})
      await region.getByRole('radiogroup',{name:'助手平台',exact:true}).waitFor()
    }
    await page.waitForTimeout(80)
    if(browserErrors.length)throw new Error('Platform switch raised a browser error: '+browserErrors.join('; '))
    if(writes!==completedWrites)throw new Error('Platform/group switching created assistant work')
    return {passed:true,synthetic_only:true,platforms:['wsl','lajiaoliao'],groups:300,paginated:true,
      context_reads:reads,turns_created:turnsCreated,human_choices:chosen,production_blocked:true,
      in_place_platform_switch:true,scoped_draft_restored:true,
      rapid_resize_switch:true,runtime_errors:0,
      native_writes:0,model_calls:0,viewports:[320,390,768,1440]}
  }finally{page.off('pageerror',onPageError);await page.unroute('**/api/v1/**',handler)}
}
