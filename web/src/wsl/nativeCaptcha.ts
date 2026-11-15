let loader: Promise<void> | undefined

function abortable<T>(task: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise((resolve, reject) => {
    const abort = () => reject(new Error('验证已取消'))
    if (signal.aborted) return abort()
    signal.addEventListener('abort', abort, { once: true })
    task.then(resolve, reject).finally(() => signal.removeEventListener('abort', abort))
  })
}

export async function nativeCaptcha(
  host: HTMLElement,
  id: string,
  signal: AbortSignal,
  onFailure?: () => void
): Promise<string> {
  const browser = window as any
  if (!browser.initNECaptcha) {
    loader ??= new Promise<void>((resolve, reject) => {
      const script = document.createElement('script')
      script.src = 'https://cstaticdun.126.net/load.min.js'
      script.async = true
      const timer = setTimeout(() => {
        script.remove()
        reject(new Error('人机验证加载超时'))
      }, 15000)
      script.onload = () => {
        clearTimeout(timer)
        if (typeof browser.initNECaptcha === 'function') resolve()
        else {
          script.remove()
          reject(new Error('人机验证加载失败'))
        }
      }
      script.onerror = () => {
        clearTimeout(timer)
        script.remove()
        reject(new Error('人机验证加载失败'))
      }
      document.head.append(script)
    }).catch((error) => {
      loader = undefined
      throw error
    })
    await abortable(loader, signal)
  }
  if (signal.aborted) throw new Error('验证已取消')
  return new Promise((resolve, reject) => {
    let instance: any,
      finished = false
    const timer = setTimeout(
      () => finish(undefined, new Error('人机验证已超时，请重新发起')),
      120000
    )
    const finish = (value?: string, error?: Error) => {
      if (finished) return
      finished = true
      clearTimeout(timer)
      signal.removeEventListener('abort', abort)
      instance?.destroy?.()
      error ? reject(error) : resolve(value!)
    }
    const abort = () => finish(undefined, new Error('验证已取消'))
    signal.addEventListener('abort', abort, { once: true })
    try {
      browser.initNECaptcha(
        {
          captchaId: id,
          element: host,
          mode: 'popup',
          apiVersion: 2,
          width: `${Math.max(1, Math.min(320, (window.visualViewport?.width || innerWidth) - 64))}px`,
          onClose: () => finish(undefined, new Error('验证已取消')),
          onVerify: (error: unknown, data: { validate?: string }) => {
            if (
              !error &&
              typeof data?.validate === 'string' &&
              data.validate.length <= 32768 &&
              data.validate.length > 0
            )
              finish(data.validate)
            else if (!finished) onFailure?.()
          }
        },
        (value: any) => {
          instance = value
          if (finished || signal.aborted) instance.destroy?.()
          else instance.verify()
        },
        () => finish(undefined, new Error('人机验证初始化失败'))
      )
    } catch {
      finish(undefined, new Error('人机验证初始化失败'))
    }
  })
}
