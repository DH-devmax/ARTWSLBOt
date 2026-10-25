// Read-only Docker runtime verification; never submits native/model requests.
// prettier-ignore
async (page) => {
  const base = 'http://127.0.0.1:5173'
  if (new URL(page.url()).origin !== base) throw new Error('Local console only')
  const request = async (path, workspace = 'wsl') => {
    const response = await page.request.get(base + '/api/v1' + path, { headers: { 'X-Workspace': workspace } })
    const result = await response.json()
    if (!response.ok() || result.code !== 0) throw new Error(`Read failed: ${path}`)
    return result.data
  }
  const catalog = await request('/workspaces')
  if (catalog.items.length !== 2 || catalog.items.some((item) => item.limits.accounts !== 10 || item.limits.groups !== 100)) throw new Error('Workspace limits are inconsistent')
  const wsl = await request('/accounts'), chat = await request('/accounts', 'maibot')
  for (const account of [...wsl, ...chat]) {
    const dashboard = await request(`/accounts/${account.id}/dashboard`, account.workspace)
    if (dashboard.account.id !== account.id || dashboard.account.workspace !== account.workspace ||
        dashboard.runtime_observed || dashboard.performance.series.length !== 15 ||
        (!dashboard.performance.latency_samples && dashboard.performance.p95_ms !== null)) {
      throw new Error('Scoped live dashboard claimed unavailable metrics or mixed accounts')
    }
  }
  const sample = await request('/system/monitor')
  if (!sample.available || sample.stale || !sample.sampled_at) throw new Error('Simon projection is not live')
  const serialized = JSON.stringify(sample)
  if (serialized.includes('processes') || serialized.includes('host_name') || serialized.includes('env')) throw new Error('Monitor exposed forbidden fields')
  const capabilities = await request('/maibot/capabilities', 'maibot')
  if (capabilities.engine_ready !== false || capabilities.items.length < 10) throw new Error('Capabilities imply parity or omit migration inventory')
  const logo = await page.request.get(base + '/wsl-logo.png')
  const logoBytes = await logo.body()
  if (!logo.ok() || ![137, 80, 78, 71].every((byte, index) => logoBytes[index] === byte)) throw new Error('Logo failed to load')
  const health = (await (await page.request.get(base + '/healthz')).json()).data
  if (health.native_ready || health.ai_ready) throw new Error('Acceptance gates unexpectedly enabled')
  return {
    passed: true, runtime: 'docker', accountPanels: { wsl: wsl.length, maibot: chat.length },
    authenticated: [...wsl, ...chat].filter((account) => account.auth_state === 'authenticated').length,
    connected: [...wsl, ...chat].filter((account) => account.runtime_state === 'connected').length,
    simonLive: true, monitorScope: sample.scope, capabilityCount: capabilities.items.length,
    dashboards: wsl.length + chat.length, writes: 0, native_ready: false, ai_ready: false
  }
}
