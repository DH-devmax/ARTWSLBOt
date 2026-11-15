<template>
  <section class="media-composer" aria-label="媒体发送">
    <input ref="picker" class="file-picker" type="file" :accept="accept" @change="chooseFile" />
    <div class="media-tools">
      <ElTooltip v-for="item in availableKinds" :key="item.kind" :content="item.label">
        <ElButton
          text
          :icon="item.icon"
          :aria-label="`选择${item.label}`"
          :disabled="busy || blockedUpload || disabled"
          @click="pick(item.kind)"
        />
      </ElTooltip>
    </div>
    <div v-if="file || upload || operation" class="media-selection">
      <span class="media-name">{{ label }} · {{ file?.name || '已上传媒体' }}</span>
      <span v-if="file" class="media-size">{{ (file.size / 1048576).toFixed(2) }} MiB</span>
      <ElButton
        v-if="!busy && !blockedUpload"
        text
        :icon="Close"
        aria-label="移除附件"
        @click="clearSelection"
      />
      <ElTooltip :content="sendReason">
        <span>
          <ElButton
            v-if="!operation"
            size="small"
            type="primary"
            :icon="Promotion"
            :loading="busy"
            :disabled="!canSend"
            aria-label="发送附件"
            @click="send"
            >发送{{ label }}</ElButton
          >
        </span>
      </ElTooltip>
    </div>
    <div v-if="status" class="media-status" role="status">
      <span>{{ status }}</span>
      <ElButton v-if="canInspect" text size="small" :loading="busy" @click="inspect"
        >核对媒体状态</ElButton
      >
      <ElButton
        v-if="operation?.status === 'queued' && !busy"
        text
        size="small"
        :disabled="disabled || !context?.enabled || !context?.media_enabled"
        @click="resume"
      >
        继续提交媒体
      </ElButton>
      <ElButton
        v-if="operation?.status === 'unknown' && !busy"
        text
        size="small"
        @click="reconcile"
      >
        查询媒体回显
      </ElButton>
    </div>
    <p v-if="error" class="media-error" role="alert">{{ error }}</p>
  </section>
</template>

<script lang="ts">
  export interface UploadState {
    id: string
    kind: string
    status: string
    result_code: string
    test_window_id?: string
  }
</script>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import { ElMessageBox } from 'element-plus'
  import { Picture, Microphone, VideoCamera, Close, Promotion } from '@element-plus/icons-vue'
  import { api, ApiError, errorText } from './api'
  import type { Workspace } from './workspaces'
  import type { TextOperation } from './messageSending'
  import { sendingVersions } from './messageSending'
  import type { SendingScope, SendingWindow } from './messageSending'

  const props = defineProps<{
    account: string
    workspace: Workspace
    conversation: string
    title: string
    disabled: boolean
    context: {
      enabled: boolean
      reason: string
      media_enabled?: boolean
      media_kinds?: string[]
      media_limits?: Record<string, number>
      scope?: SendingScope
      test_windows?: SendingWindow[]
      recent_uploads?: UploadState[]
      recent?: TextOperation[]
    } | null
  }>()
  const emit = defineEmits<{ blocking: [boolean]; refresh: [] }>()
  const kinds = [
    { kind: 'image', label: '图片', icon: Picture },
    { kind: 'voice', label: '语音', icon: Microphone },
    { kind: 'video', label: '视频', icon: VideoCamera }
  ]
  const kind = ref('image'),
    file = ref<File | null>(null),
    upload = ref<UploadState | null>(null),
    operation = ref<TextOperation | null>(null),
    picker = ref<HTMLInputElement>(),
    busy = ref(false),
    confirming = ref(false),
    error = ref(''),
    requestUnknown = ref(false)
  let uploadKey = '',
    sendKey = '',
    windowID = '',
    active = true
  const controller = new AbortController()
  const availableKinds = computed(() =>
    kinds.filter(
      (item) =>
        (props.workspace !== 'lajiaoliao' || ['image', 'voice', 'video'].includes(item.kind)) &&
        (!props.context?.media_kinds || props.context.media_kinds.includes(item.kind))
    )
  )
  const label = computed(() => kinds.find((item) => item.kind === kind.value)?.label || '媒体')
  const accept = computed(
    () =>
      ({
        image: 'image/png,image/jpeg,image/gif,image/webp',
        voice: '.mp3,.wav,audio/mpeg,audio/wav',
        video: 'video/mp4,.mp4'
      })[kind.value]
  )
  const base = computed(() => `/accounts/${props.account}/conversations/${props.conversation}`)
  const testWindow = computed(() =>
    props.context?.test_windows?.find(
      (item) =>
        item.actions?.includes(
          `send_${props.context?.scope?.scene === 'private' ? 'private_' : ''}${kind.value}`
        ) &&
        (!windowID || item.id === windowID) &&
        item.remaining >= (upload.value?.status === 'succeeded' ? 1 : 2)
    )
  )
  const blockedUpload = computed(
    () =>
      requestUnknown.value ||
      (!!upload.value && ['preparing', 'submitting', 'unknown'].includes(upload.value.status)) ||
      (!!operation.value &&
        ['queued', 'running', 'unknown', 'request_unknown'].includes(operation.value.status))
  )
  const sendReason = computed(() =>
    !props.context?.media_enabled
      ? errorText('native_media_acceptance_pending')
      : !testWindow.value
        ? '需要授权目标的相应媒体测试权限；上传和发送各占一次动作'
        : errorText(props.context?.reason || '')
  )
  const canSend = computed(
    () =>
      !props.disabled &&
      !busy.value &&
      !confirming.value &&
      !blockedUpload.value &&
      !operation.value &&
      !!props.context?.enabled &&
      !!props.context?.media_enabled &&
      availableKinds.value.some((item) => item.kind === kind.value) &&
      !!testWindow.value &&
      (!!file.value || upload.value?.status === 'succeeded')
  )
  const canInspect = computed(
    () => !!operation.value?.id || !!upload.value?.id || !!uploadKey || !!sendKey
  )
  const status = computed(() => {
    if (requestUnknown.value) return '请求结果待核对，不会自动重传或重发'
    if (operation.value)
      return (
        {
          queued: '媒体意图已保存，尚未发送',
          running: '媒体正在提交，勿重复发送',
          unknown: '媒体发送结果未知，请查询回显，勿重发',
          succeeded: '原生服务端已接受媒体，不代表送达或已读',
          failed: '媒体发送失败',
          expired: '媒体发送意图已过期',
          cancelled: '媒体发送已取消'
        }[operation.value.status] || '媒体状态待核对'
      )
    return upload.value
      ? {
          preparing: '正在准备上传',
          submitting: '媒体正在上传',
          unknown: '上传结果未知，不会自动重传',
          succeeded: '媒体已上传，等待提交消息',
          failed: '媒体上传失败',
          expired: '上传凭证已过期'
        }[upload.value.status] || ''
      : ''
  })
  watch(
    () => props.context,
    (context) => {
      if (upload.value || file.value || operation.value || busy.value) return
      const pending = context?.recent?.find(
        (item) =>
          ['send_image', 'send_voice', 'send_video', 'send_file'].includes(item.kind || '') &&
          ['queued', 'running', 'unknown'].includes(item.status)
      )
      if (pending) {
        operation.value = pending
        kind.value = pending.kind!.slice(5)
        windowID = pending.test_window_id || ''
        return
      }
      const saved = context?.recent_uploads?.find((item) =>
        ['preparing', 'submitting', 'unknown', 'succeeded'].includes(item.status)
      )
      if (saved) {
        upload.value = saved
        kind.value = saved.kind
        windowID = saved.test_window_id || ''
      }
    },
    { immediate: true }
  )
  watch(
    () =>
      operation.value?.status !== 'succeeded' &&
      (!!file.value ||
        (!!upload.value && !['failed', 'expired'].includes(upload.value.status)) ||
        blockedUpload.value ||
        busy.value),
    (value) => emit('blocking', value),
    { immediate: true }
  )
  function pick(value: string) {
    kind.value = value
    if (picker.value) {
      picker.value.value = ''
      // Vue updates accept before the native file picker opens.
      picker.value.accept = {
        image: 'image/png,image/jpeg,image/gif,image/webp',
        voice: '.mp3,.wav,audio/mpeg,audio/wav',
        video: '.mp4,video/mp4'
      }[value]!
      picker.value.click()
    }
  }
  function chooseFile() {
    const selected = picker.value?.files?.[0]
    if (!selected) return
    clearSelection()
    const limit =
      props.context?.media_limits?.[kind.value] || (kind.value === 'image' ? 5 : 16) * 1048576
    if (selected.size === 0 || selected.size > limit) {
      error.value = `文件必须大于 0 且不超过 ${limit / 1048576} MiB`
      return
    }
    file.value = selected
  }
  function clearSelection() {
    file.value = null
    upload.value = null
    operation.value = null
    error.value = ''
    requestUnknown.value = false
    uploadKey = sendKey = windowID = ''
  }
  async function confirm() {
    if (confirming.value) return false
    confirming.value = true
    try {
      await ElMessageBox.confirm(
        `向“${props.title}”发送${label.value}？上传和消息提交各占一次测试动作。`,
        '确认发送媒体',
        { type: 'warning', confirmButtonText: '确认发送', cancelButtonText: '取消' }
      )
      return active
    } catch {
      return false
    } finally {
      confirming.value = false
    }
  }
  async function poster(selected: File): Promise<Blob> {
    const url = URL.createObjectURL(selected)
    const video = document.createElement('video')
    video.muted = true
    video.preload = 'auto'
    const abort = () => {
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
    try {
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => done(new Error('无法读取 MP4 封面')), 5000)
        const done = (error?: Error) => {
          clearTimeout(timer)
          video.onloadeddata = video.onerror = null
          controller.signal.removeEventListener('abort', cancelled)
          error ? reject(error) : resolve()
        }
        const cancelled = () => done(new Error('已取消'))
        controller.signal.addEventListener('abort', cancelled, { once: true })
        video.onloadeddata = () => done()
        video.onerror = () => done(new Error('无法读取 MP4 封面'))
        video.src = url
      })
      if (!active || video.videoWidth < 1 || video.videoHeight < 1) throw new Error('视频不可用')
      const canvas = document.createElement('canvas')
      canvas.width = 256
      canvas.height = Math.max(1, Math.round((256 * video.videoHeight) / video.videoWidth))
      if (canvas.height > 4096) throw new Error('视频尺寸不支持')
      canvas.getContext('2d')!.drawImage(video, 0, 0, canvas.width, canvas.height)
      return await new Promise((resolve, reject) =>
        canvas.toBlob(
          (blob) => (blob ? resolve(blob) : reject(new Error('无法生成封面'))),
          'image/jpeg',
          0.85
        )
      )
    } finally {
      abort()
      URL.revokeObjectURL(url)
    }
  }
  async function stageAndDispatch() {
    if (!active) return
    if (!operation.value) {
      if (upload.value?.status !== 'succeeded') return
      sendKey ||= crypto.randomUUID()
      operation.value = await api<TextOperation>(
        `${base.value}/media-operations`,
        'POST',
        {
          upload_id: upload.value.id,
          test_window_id: windowID,
          ...sendingVersions(
            props.context?.scope,
            props.context?.test_windows?.find((item) => item.id === windowID)
          )
        },
        controller.signal,
        props.workspace,
        sendKey
      )
    }
    if (!active || operation.value.status !== 'queued') return
    const id = operation.value.id
    operation.value = { ...operation.value, status: 'running' }
    operation.value = await api<TextOperation>(
      `${base.value}/media-operations/${id}/dispatch`,
      'POST',
      {},
      controller.signal,
      props.workspace
    )
    if (operation.value.status === 'succeeded') file.value = null
    emit('refresh')
  }
  function failed(errorValue: unknown) {
    if (!(errorValue instanceof ApiError) || errorValue.status >= 500 || errorValue.status === 408)
      requestUnknown.value = !!uploadKey || !!sendKey
    if (active) error.value = (errorValue as Error).message
  }
  async function send() {
    if (!canSend.value || !(await confirm()) || !canSend.value) return
    busy.value = true
    error.value = ''
    windowID ||= testWindow.value!.id
    try {
      if (upload.value?.status !== 'succeeded') {
        const data = new FormData()
        data.append(
          'request',
          JSON.stringify({
            kind: kind.value,
            test_window_id: windowID,
            ...sendingVersions(props.context?.scope, testWindow.value)
          })
        )
        data.append('file', file.value!)
        if (kind.value === 'video')
          data.append('thumbnail', await poster(file.value!), 'thumbnail.jpg')
        if (!active) return
        uploadKey = crypto.randomUUID()
        upload.value = await api<UploadState>(
          `${base.value}/media-uploads`,
          'POST',
          data,
          controller.signal,
          props.workspace,
          uploadKey
        )
      }
      await stageAndDispatch()
    } catch (e) {
      failed(e)
    } finally {
      busy.value = false
    }
  }
  async function inspect() {
    if (busy.value) return
    busy.value = true
    try {
      if (sendKey || operation.value?.id) {
        operation.value = await api<TextOperation>(
          `/accounts/${props.account}/native-text-operations${operation.value?.id ? `/${operation.value.id}` : `?key=${encodeURIComponent(sendKey)}`}`,
          'GET',
          undefined,
          controller.signal,
          props.workspace
        )
      } else {
        upload.value = await api<UploadState>(
          `/accounts/${props.account}/native-media-uploads${upload.value?.id ? `/${upload.value.id}` : `?key=${encodeURIComponent(uploadKey)}`}`,
          'GET',
          undefined,
          controller.signal,
          props.workspace
        )
        windowID = upload.value.test_window_id || windowID
      }
      requestUnknown.value = false
      error.value = ''
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        requestUnknown.value = false
        if (sendKey && !operation.value) sendKey = ''
        else if (uploadKey && !upload.value) uploadKey = ''
        error.value = '尚未找到已保存记录；不会自动上传或发送，请重新确认'
      } else failed(e)
    } finally {
      busy.value = false
    }
  }
  async function resume() {
    if (
      busy.value ||
      props.disabled ||
      !props.context?.enabled ||
      !props.context?.media_enabled ||
      !(await confirm())
    )
      return
    busy.value = true
    try {
      await stageAndDispatch()
    } catch (e) {
      failed(e)
    } finally {
      busy.value = false
    }
  }
  async function reconcile() {
    if (busy.value || !operation.value?.id) return
    busy.value = true
    try {
      operation.value = await api<TextOperation>(
        `/accounts/${props.account}/native-text-operations/${operation.value.id}/reconcile`,
        'POST',
        {},
        controller.signal,
        props.workspace
      )
      requestUnknown.value = false
      emit('refresh')
    } catch (e) {
      failed(e)
    } finally {
      busy.value = false
    }
  }
  onUnmounted(() => {
    active = false
    controller.abort()
    file.value = null
  })
</script>

<style scoped>
  .file-picker {
    display: none;
  }
  .media-tools,
  .media-selection,
  .media-status {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex-wrap: wrap;
  }
  .media-tools :deep(.el-button) {
    margin: 0;
    padding: 6px;
    width: 30px;
    height: 30px;
  }
  .media-selection,
  .media-status {
    font-size: 12px;
    line-height: 1.6;
    margin: 4px 0;
  }
  .media-name {
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .media-size,
  .media-status {
    color: var(--el-text-color-secondary);
  }
  .media-error {
    color: var(--el-color-danger);
    font-size: 12px;
    overflow-wrap: anywhere;
    margin: 4px 0;
  }
</style>
