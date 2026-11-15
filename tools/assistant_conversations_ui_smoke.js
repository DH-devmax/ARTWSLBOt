// Synthetic conversations only. The harness blocks every external origin.
// prettier-ignore
async (page) => {
  const origin = new URL(page.url()).origin
  const id = n => `c3000000-0000-4000-8000-${String(n).padStart(12,'0')}`
  const actor=id(1), account=id(2), group=id(3), otherGroup=id(4)
  const permissions=['overview:read','accounts:read','groups:read','assistant:use']
  let platform='wsl', role='superadmin', deletes=0, sessionPosts=0, blocked=false, delayed=false, releaseDelete
  const sessions=[]
  const now=new Date().toISOString()
  const groups=()=>[
    {group_id:group,name:'测试群',primary_account_id:account,robot_nickname:'原生机器人',platform,active:true,native_id:'test-one'},
    {group_id:otherGroup,name:'另一测试群',primary_account_id:account,robot_nickname:'原生机器人',platform,active:true,native_id:'test-two'}
  ]
  const seed=()=>{
    sessions.length=0
    for(let n=0;n<7;n++)sessions.push({id:id(100+n),platform,group_id:group,
      title:n===0?'检查今天的群公告':`第一条问题 ${n}`,is_empty:false,created_at:now})
  }
  const handler=async route=>{
    const req=route.request(),url=new URL(req.url()),path=url.pathname.replace('/api/v1','')
    let data=[],status=200
    if(req.method()==='DELETE'){
      if(!/^\/assistant\/sessions\/[^/]+$/.test(path))throw Error('Unexpected deletion')
      deletes++
      if(blocked){await route.fulfill({status:409,json:{code:1,msg:'assistant_session_busy'}});return}
      if(delayed){delayed=false;await new Promise(resolve=>{releaseDelete=resolve})}
      const index=sessions.findIndex(s=>s.id===path.split('/')[3])
      if(index>=0)sessions.splice(index,1)
      data={id:path.split('/')[3],deleted:true}
    }else if(req.method()!=='GET'){
      if(path!=='/assistant/sessions')throw Error('UI generated model or native work')
      sessionPosts++
      const body=req.postDataJSON()
      data=sessions.find(s=>s.is_empty&&s.platform===platform&&s.group_id===body.group_id)
      if(!data){data={id:id(500+sessionPosts),platform,group_id:body.group_id,is_empty:true,title:'',created_at:now};sessions.unshift(data)}
      status=201
    }else if(path==='/me')data={user:{id:actor,username:'synthetic',role},permissions}
    else if(path==='/workspaces')data={items:[{id:'user',name:'用户中心'},{id:'wsl',name:'旺商聊机器人'},{id:'lajiaoliao',name:'辣椒聊机器人'}]}
    else if(path==='/accounts')data=[{id:account,name:'账号备注',native_nickname:'原生机器人',workspace:platform,platform,purpose:'management'}]
    else if(path==='/groups')data=groups()
    else if(path==='/overview')data={accounts:1,groups:2}
    else if(path==='/assistant/context')data={platform,groups:groups(),permissions,total:2,next_offset:-1}
    else if(path==='/assistant/usage')data={unit:'micro_credit',used_or_reserved:0,timezone:'Asia/Shanghai'}
    else if(path==='/assistant/sessions')data=sessions.filter(s=>s.platform===platform&&s.group_id===url.searchParams.get('group_id'))
    else if(/\/sessions\/[^/]+\/transcript$/.test(path)){
      const s=sessions.find(s=>s.id===path.split('/')[3])
      data=!s||s.is_empty?[]:[{id:id(800+Number(s.id.slice(-3))),session_id:s.id,status:'succeeded',
        input:s.title,answer:'这是已保存的合成答复。',cards:[],cursor:2,created_at:now}]
    }
    await route.fulfill({status,json:{code:0,msg:'ok',data}})
  }
  const region=()=>page.getByRole('region',{name:'助手会话工作台'})
  const openHistory=async()=>{await region().getByRole('button',{name:'历史对话',exact:true}).click();return page.locator('.assistant-history-drawer')}
  const deleteDialog=()=>page.getByRole('dialog',{name:'删除对话',exact:true})
  const expandGroups=async()=>{
    const button=region().locator('.assistant-mobile-groups')
    if(await button.isVisible()&&await button.getAttribute('aria-expanded')!=='true')await button.click()
  }
  await page.route('**/api/v1/**',handler)
  const screenshots=[]
  try{
    await page.evaluate(actor=>{
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`,'user')
    },actor)
    await page.setViewportSize({width:1440,height:960})
    await page.goto(origin+'/#assistant');await page.reload()
    const sidebar=page.locator('#primary-sidebar')
    const entry=sidebar.locator(':scope > .sidebar-assistant')
    await entry.waitFor()
    if(await entry.locator('.admin-mark polygon').count()!==3)throw Error('Assistant sidebar does not share the brand SVG')
    if(await sidebar.locator('nav .sidebar-assistant,.workspace-label,.sidebar-footer').count())throw Error('Sidebar cleanup failed')
    if(!await entry.evaluate(el=>!!(el.compareDocumentPosition(el.parentElement.querySelector('nav'))&Node.DOCUMENT_POSITION_FOLLOWING)))throw Error('Assistant entry is not above navigation')
    for(const selectedPlatform of ['wsl','lajiaoliao']){
      platform=selectedPlatform
      // Use the real workspace switcher, preserving its permission checks.
      await page.getByRole('button',{name:'切换工作台',exact:true}).click()
      await page.getByRole('dialog',{name:'选择平台',exact:true}).getByRole('button',{name:platform==='wsl'?'旺商聊机器人':'辣椒聊机器人',exact:true}).click()
      seed()
      await page.goto(origin+'/#assistant');await page.reload()
      await region().getByRole('button',{name:/选择群组 测试群/}).click()
      await region().getByText('这是已保存的合成答复。',{exact:true}).waitFor()
      const platformLogo=platform==='wsl'?'/wsl-logo.png':'/lajiaoliao-logo.png'
      const platformImages=region().locator('.assistant-group-buttons img.assistant-group-icon')
      if(await platformImages.count()!==2)throw Error('Group candidates are missing their platform logos')
      for(const image of await platformImages.all()){
        if(await image.getAttribute('src')!==platformLogo||!await image.evaluate(el=>el.complete&&el.naturalWidth>0))
          throw Error('Group candidate logo is incorrect or failed to render')
      }
      if(await region().locator('.assistant-mobile-groups img').getAttribute('src')!==platformLogo)
        throw Error('Selected group uses another platform logo')
      if(await region().locator('.assistant-answer-avatar .admin-mark polygon').count()!==3)throw Error('Assistant answer avatar uses a different mark')
      let history=await openHistory()
      await history.getByRole('textbox',{name:'搜索历史对话',exact:true}).fill('今天')
      if(await history.locator('.assistant-history-item').count()!==1)throw Error('History search failed')
      await history.getByRole('button',{name:'删除对话 检查今天的群公告',exact:true}).click()
      await deleteDialog().getByRole('button',{name:'取消',exact:true}).click()
      if(deletes!== (platform==='wsl'?0:9))throw Error('Viewing/cancelling deletion submitted work')
      await history.getByRole('button',{name:'删除对话 检查今天的群公告',exact:true}).click()
      blocked=true
      await deleteDialog().getByRole('button',{name:'删除对话',exact:true}).click()
      await deleteDialog().getByText('此对话仍在生成或提交操作，请先停止生成或等待操作完成再删除',{exact:true}).waitFor()
      if(sessions.length!==7)throw Error('Failed deletion erased history')
      blocked=false
      await deleteDialog().getByRole('button',{name:'删除对话',exact:true}).click()
      await deleteDialog().waitFor({state:'hidden'})
      await region().locator('.assistant-welcome').waitFor()
      if(await region().locator('.assistant-welcome .admin-mark polygon').count()!==3)throw Error('Assistant welcome uses a different mark')
      if(await region().getByText('这是已保存的合成答复。',{exact:true}).count())throw Error('Deleted current body remains')
      history=await openHistory()
      await history.getByRole('textbox',{name:'搜索历史对话',exact:true}).fill('')
      if(await history.locator('.assistant-history-item').count()!==6)throw Error('History count not updated')
      await history.locator('.assistant-history-select').first().click()
      await region().getByText('这是已保存的合成答复。',{exact:true}).waitFor()
      for(const width of [320,390,509,635,768,1440]){
        await page.setViewportSize({width,height:769})
        await page.waitForTimeout(100)
        const send=await region().getByRole('button',{name:'发送',exact:true}).boundingBox()
        if(!send||send.x<0||send.x+send.width>width+1||send.y+send.height>769)throw Error('Composer overflow '+width)
        const filled=await region().evaluate(el=>el.getBoundingClientRect().width>=el.parentElement.getBoundingClientRect().width-40)
        if(!filled)throw Error('Assistant does not fill its work area '+width)
        if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Page overflow '+width)
        if(width<768&&!await region().locator('.assistant-mobile-groups img').evaluate(el=>el.complete&&el.naturalWidth>0))
          throw Error('Selected group platform logo did not render at '+width)
        const image=`output/playwright/assistant-conversations-${platform}-${width}.png`
        await page.screenshot({path:image,animations:'disabled'});screenshots.push(image)
      }
      await page.setViewportSize({width:390,height:500})
      await region().getByRole('textbox',{name:'助手问题',exact:true}).focus()
      const send=await region().getByRole('button',{name:'发送',exact:true}).boundingBox()
      if(!send||send.y+send.height>500)throw Error('Short viewport hides composer')
      history=await openHistory()
      await history.locator('.assistant-history-delete').first().click()
      await deleteDialog().waitFor()
      for(const width of [320,390,768,1440]){
        await page.setViewportSize({width,height:650})
        const box=await deleteDialog().getByRole('button',{name:'删除对话',exact:true}).boundingBox()
        if(!box||box.x<0||box.x+box.width>width+1||box.y+box.height>650)throw Error('Delete confirmation overflow '+width)
      }
      await deleteDialog().getByRole('button',{name:'取消',exact:true}).click()
      for(let n=0;n<6;n++){
        await history.locator('.assistant-history-delete').first().click()
        await deleteDialog().getByRole('button',{name:'删除对话',exact:true}).click()
        await deleteDialog().waitFor({state:'hidden'})
        if(n<5)history=await openHistory()
      }
      await region().locator('.assistant-welcome').waitFor()
      if(sessions.length!==0)throw Error('Last conversation not removed')
      const before=sessionPosts
      await region().getByRole('button',{name:'新对话',exact:true}).click()
      await region().locator('.assistant-conversation-title').getByText('新对话',{exact:true}).waitFor()
      await region().getByRole('textbox',{name:'助手问题',exact:true}).fill('空白对话草稿')
      await region().getByRole('button',{name:'新对话',exact:true}).click()
      await page.waitForTimeout(100)
      if(sessions.length!==1||sessionPosts!==before+2||await region().getByRole('textbox',{name:'助手问题',exact:true}).inputValue()!=='空白对话草稿')throw Error('Unique blank conversation regressed')
      history=await openHistory()
      await history.locator('.assistant-history-delete').first().click()
      delayed=true
      await deleteDialog().getByRole('button',{name:'删除对话',exact:true}).click()
      await page.waitForFunction(()=>!!document.querySelector('.el-dialog .is-loading'))
      // External route/scope changes are possible during an outstanding response.
      await page.goto(origin+'/#overview')
      releaseDelete()
      await page.waitForTimeout(80)
      await page.goto(origin+'/#assistant')
      await expandGroups()
      await region().getByRole('button',{name:/选择群组 另一测试群/}).click()
      if(await region().locator('.assistant-turn').count())throw Error('Late deletion polluted another group')
      await page.setViewportSize({width:1440,height:960})
    }
    await page.getByRole('button',{name:'切换到管理后台',exact:true}).click()
    await page.goto(origin+'/#overview')
    await sidebar.locator('.brand').getByText('管理后台',{exact:true}).waitFor()
    if(await sidebar.locator('.workspace-label,.sidebar-footer').count())throw Error('User center clutter remains')
    if(await sidebar.locator('.brand .admin-mark polygon').count()!==3)throw Error('Animated brand missing')
    await page.emulateMedia({reducedMotion:'reduce'})
    if(await sidebar.locator('.brand .admin-mark .cls-1').evaluate(el=>getComputedStyle(el).animationName)!=='none')throw Error('Reduced motion ignored')
    await page.screenshot({path:'output/playwright/admin-sidebar-1440.png',animations:'disabled'})
    await page.setViewportSize({width:635,height:769})
    await page.getByRole('button',{name:'展开导航',exact:true}).click()
    await page.screenshot({path:'output/playwright/admin-sidebar-635.png',animations:'disabled'})
    role='viewer';await page.reload()
    await page.getByRole('button',{name:'展开导航',exact:true}).click()
    await sidebar.locator('.brand').getByText('用户中心',{exact:true}).waitFor()
    if(await sidebar.locator('.brand .admin-mark polygon').count()!==3)throw Error('User center did not retain the same SVG brand')
    if(await sidebar.getByRole('button',{name:'切换到管理后台',exact:true}).count())throw Error('Ordinary user received administrator navigation')
    return {passed:true,synthetic_only:true,platforms:['wsl','lajiaoliao'],deletes,session_posts:sessionPosts,
      model_calls:0,native_writes:0,delete_cancel:true,delete_busy:true,last_session_deleted:true,
      late_response_isolated:true,unique_empty_session:true,sidebar_cleaned:true,platform_group_logos:true,
      viewports:[320,390,509,635,768,1440],screenshots}
  }finally{if(releaseDelete)releaseDelete();await page.unroute('**/api/v1/**',handler)}
}
