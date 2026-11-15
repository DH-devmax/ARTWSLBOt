import { api, type Row, type Workspace } from './api'

export async function chiliSecurityCaptcha(
  host: HTMLElement,
  account: string,
  kind: string,
  workspace: Workspace,
  signal: AbortSignal
): Promise<string> {
  const result = await api<Row>(
    `/accounts/${account}/native-security-challenge?kind=${encodeURIComponent(kind)}`,
    'GET',
    undefined,
    signal,
    workspace
  )
  if (signal.aborted) throw new Error('验证已取消')
  if (result.challenge?.required === false) return ''
  if (result.challenge?.required !== true) throw new Error('官方安全验证暂不可用')
  return new Promise((resolve, reject) => {
    const frame = document.createElement('iframe'),
      cancel = document.createElement('button'),
      id = crypto.randomUUID()
    frame.title = '辣椒聊安全验证'
    frame.setAttribute('sandbox', 'allow-scripts allow-forms')
    frame.style.cssText =
      'position:fixed;inset:0;width:100%;height:100%;border:0;background:var(--el-bg-color);pointer-events:auto'
    cancel.type = 'button'
    cancel.textContent = '取消验证'
    cancel.style.cssText =
      'position:fixed;right:20px;top:20px;z-index:1;padding:8px 16px;pointer-events:auto'
    const start = () =>
      frame.contentWindow?.postMessage(
        { source: 'artwsl-lajiao-login', id, challenge: result.challenge },
        '*'
      )
    function finish(error?: string, proof = '') {
      frame.contentWindow?.postMessage({ source: 'artwsl-lajiao-login', id, action: 'cancel' }, '*')
      clearTimeout(timer)
      window.removeEventListener('message', receive)
      signal.removeEventListener('abort', abort)
      frame.remove()
      cancel.remove()
      if (error) reject(new Error(error))
      else resolve(proof)
    }
    const abort = () => finish('验证已取消')
    function receive(event: MessageEvent) {
      const data = event.data
      if (
        event.source !== frame.contentWindow ||
        event.origin !== 'null' ||
        data?.source !== 'artwsl-lajiao-captcha'
      )
        return
      if (data.type === 'ready') {
        start()
        return
      }
      if (data.id !== id) return
      if (
        data.type === 'success' &&
        typeof data.value === 'string' &&
        data.value.length > 0 &&
        data.value.length <= 32768
      )
        finish(undefined, data.value)
      else if (data.type === 'failed' || data.type === 'cancelled') finish('官方安全验证未完成')
    }
    const timer = setTimeout(() => finish('官方安全验证已超时'), 120000)
    cancel.onclick = abort
    frame.onload = start
    window.addEventListener('message', receive)
    signal.addEventListener('abort', abort, { once: true })
    host.append(frame, cancel)
    frame.src = '/lajiaoliao-captcha.html'
    if (signal.aborted) abort()
  })
}
