<template>
  <span ref="root" class="message-image" :class="{ avatar, sticker }">
    <template v-if="avatar">
      <img v-if="source" :src="source" :alt="label + '的头像'" @error="broken" />
      <span v-else class="avatar-placeholder" :title="error">{{ label.slice(0, 1) || '·' }}</span>
    </template>
    <button
      v-else-if="!source"
      type="button"
      :class="{ 'image-load': !avatar }"
      :disabled="loading || !url"
      :aria-label="
        avatar ? '加载头像' : error ? '重新加载图片' : sticker ? '加载表情包' : '加载图片'
      "
      :title="error || (avatar ? '头像' : '')"
      @click.stop="load"
    >
      <ElIcon><Loading v-if="loading" class="is-loading" /><Picture v-else /></ElIcon>
      <span>{{
        !url ? '图片预览不可用' : loading ? '加载中' : error ? '加载失败，点击重试' : sticker ? '表情包' : '图片'
      }}</span>
      <small v-if="error">{{ error }}</small>
    </button>
    <button
      v-else
      type="button"
      :aria-label="avatar ? '查看头像' : '查看原图'"
      @click.stop="preview = true"
    >
      <img
        :src="source"
        :alt="avatar ? label + '的头像' : sticker ? '表情包' : '聊天图片'"
        @error="broken"
      />
    </button>
    <ElDialog
      v-if="preview"
      v-model="preview"
      title="图片"
      width="min(92vw, 800px)"
      append-to-body
      align-center
      destroy-on-close
    >
      <img class="message-image-preview" :src="source" alt="原图" />
    </ElDialog>
  </span>
</template>

<script setup lang="ts">
  import { onMounted, onUnmounted, ref, watch } from 'vue'
  import { Loading, Picture } from '@element-plus/icons-vue'
  import { nativeImage } from './messageMedia'
  import type { Workspace } from './workspaces'

  const props = withDefaults(
    defineProps<{
      url?: string
      workspace: Workspace
      scope: string
      label?: string
      avatar?: boolean
      sticker?: boolean
    }>(),
    { url: '', label: '', avatar: false, sticker: false }
  )
  const root = ref<HTMLElement | null>(null)
  const source = ref(''),
    error = ref(''),
    loading = ref(false),
    preview = ref(false)
  let controller = new AbortController()
  let observer: IntersectionObserver | undefined
  let generation = 0
  const clear = () => {
    generation++
    controller.abort()
    controller = new AbortController()
    if (source.value) URL.revokeObjectURL(source.value)
    source.value = ''
    error.value = ''
    loading.value = preview.value = false
  }
  async function load() {
    if (!props.url || loading.value) return
    const current = generation
    loading.value = true
    error.value = ''
    try {
      const blob = await nativeImage(props.url, props.workspace, controller.signal)
      if (current !== generation) return
      source.value = URL.createObjectURL(blob)
    } catch (e) {
      if (current === generation && !controller.signal.aborted) {
        error.value = e instanceof Error ? e.message : '图片暂不可用'
      }
    } finally {
      if (current === generation) loading.value = false
    }
  }
  const broken = () => {
    clear()
    error.value = '图片无法显示'
  }
  const observe = () => {
    observer?.disconnect()
    if (!props.avatar || !props.url || !root.value) return
    observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        observer?.disconnect()
        void load()
      }
    })
    observer.observe(root.value)
  }
  watch(
    () => [props.url, props.workspace, props.scope],
    () => {
      clear()
      observe()
    }
  )
  onMounted(observe)
  onUnmounted(() => {
    observer?.disconnect()
    clear()
  })
</script>

<style scoped>
  .message-image {
    display: inline-block;
    max-width: 100%;
    vertical-align: middle;
  }
  .message-image button {
    display: block;
    max-width: 100%;
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .message-image img {
    display: block;
    max-width: min(260px, 100%);
    max-height: 300px;
    object-fit: contain;
    border-radius: 4px;
  }
  .message-image .image-load {
    display: flex;
    min-height: 90px;
    width: 180px;
    max-width: 100%;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 12px;
    background: #f0f2f4;
    border-radius: 4px;
    font-size: 13px;
  }
  .image-load .el-icon {
    font-size: 26px;
  }
  .image-load small {
    max-width: 100%;
    font-size: 11px;
    overflow-wrap: anywhere;
    color: #777;
  }
  .avatar {
    width: 36px;
    height: 36px;
    flex-shrink: 0;
  }
  .avatar .avatar-placeholder,
  .avatar img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: 4px;
  }
  .avatar .avatar-placeholder {
    display: grid;
    place-items: center;
    background: #e3e7eb;
    font-size: 14px;
  }
  .sticker img {
    max-width: min(160px, 100%);
    max-height: 160px;
  }
  .message-image-preview {
    display: block;
    margin: auto;
    max-width: 100%;
    max-height: 70dvh;
    object-fit: contain;
  }
</style>
