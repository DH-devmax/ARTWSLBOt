import { ApiError } from './api'
import type { Workspace } from './workspaces'

type Job = { run: () => void; cancel: () => void }
let active = 0
const queue: Job[] = []
const mediaPath =
  /^\/api\/v1\/accounts\/[a-f0-9-]{36}\/(?:conversations\/[a-f0-9]{64}\/messages\/[1-9][0-9]*\/media\/(?:sender|target|image|contact-avatar|video-poster|entry-[0-9]+-(?:avatar|image|poster)|node-(?:[0-9]|[12][0-9]|3[01])-(?:avatar|image|poster))|native-groups\/[1-9][0-9]*\/avatar\/[a-f0-9]{64}|native-avatar\/[A-Za-z0-9_-]{64,4096})$/
const videoPath =
  /^\/api\/v1\/accounts\/[a-f0-9-]{36}\/conversations\/[a-f0-9]{64}\/messages\/[1-9][0-9]*\/media\/(?:video|entry-[0-9]+-video|node-(?:[0-9]|[12][0-9]|3[01])-video)$/
export const maxVideoBytes = 16 * 1024 * 1024
let stopVideo: (() => void) | undefined

// A tab retains at most one loaded/loading video, including forwarded records.
export function ownVideo(stop: () => void): () => void {
  stopVideo?.()
  stopVideo = stop
  return () => {
    if (stopVideo === stop) stopVideo = undefined
  }
}

function slot<T>(signal: AbortSignal, work: () => Promise<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    let started = false
    const abort = () => {
      if (started) return
      const index = queue.indexOf(job)
      if (index >= 0) queue.splice(index, 1)
      signal.removeEventListener('abort', abort)
      reject(new DOMException('Cancelled', 'AbortError'))
    }
    const job: Job = {
      cancel: abort,
      run: () => {
        if (signal.aborted) return abort()
        started = true
        signal.removeEventListener('abort', abort)
        active++
        work()
          .then(resolve, reject)
          .finally(() => {
            active--
            queue.shift()?.run()
          })
      }
    }
    if (signal.aborted) return abort()
    if (active >= 2 && queue.length >= 32) return reject(new ApiError(429, 'native_media_busy'))
    signal.addEventListener('abort', abort, { once: true })
    if (active < 2) job.run()
    else queue.push(job)
  })
}

export function nativeImage(
  path: string,
  workspace: Workspace,
  signal: AbortSignal
): Promise<Blob> {
  if (!mediaPath.test(path)) return Promise.reject(new ApiError(400, 'invalid_media_reference'))
  return nativeFile(path, workspace, signal, false)
}

export function nativeVideo(
  path: string,
  workspace: Workspace,
  signal: AbortSignal
): Promise<Blob> {
  if (!videoPath.test(path)) return Promise.reject(new ApiError(400, 'invalid_media_reference'))
  return nativeFile(path, workspace, signal, true)
}

function nativeFile(
  path: string,
  workspace: Workspace,
  signal: AbortSignal,
  video: boolean
): Promise<Blob> {
  return slot(signal, async () => {
    const response = await fetch(path, {
      credentials: 'same-origin',
      cache: 'no-store',
      signal,
      headers: { 'X-Workspace': workspace === 'user' ? 'wsl' : workspace }
    })
    if (!response.ok) {
      const result = await response.json().catch(() => ({}))
      throw new ApiError(response.status, result.msg || 'native_media_unavailable')
    }
    const mime = response.headers.get('Content-Type') || ''
    const limit = video ? maxVideoBytes : 5 * 1024 * 1024
    const types = video
      ? ['video/mp4', 'video/webm']
      : ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
    if (!types.includes(mime) || Number(response.headers.get('Content-Length')) > limit) {
      await response.body?.cancel()
      throw new ApiError(502, 'native_media_invalid')
    }
    const reader = response.body?.getReader()
    if (!reader) throw new ApiError(502, 'native_media_invalid')
    const chunks: Uint8Array[] = []
    let size = 0
    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        size += value.byteLength
        if (size > limit) {
          await reader.cancel()
          throw new ApiError(502, 'native_media_invalid')
        }
        chunks.push(new Uint8Array(value))
      }
    } finally {
      reader.releaseLock()
    }
    if (signal.aborted) throw new DOMException('Cancelled', 'AbortError')
    return new Blob(chunks, { type: mime })
  })
}
