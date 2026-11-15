<template>
  <section ref="root" class="message-video" data-testid="message-video" aria-label="视频消息">
    <video
      v-if="source"
      ref="player"
      :src="source"
      :poster="cover"
      controls
      playsinline
      preload="none"
      @error="broken"
    />
    <button
      v-else
      class="video-load"
      type="button"
      :aria-label="error ? '重新加载视频' : '播放视频'"
      :disabled="loading || !!unavailable"
      @click="load"
    >
      <img v-if="cover" :src="cover" alt="视频封面" @error="clearCover" />
      <ElIcon><Loading v-if="loading" class="is-loading" /><VideoPlay v-else /></ElIcon>
      <span v-if="loading">加载中</span>
    </button>
    <div class="video-meta">
      <span>{{ duration }}</span>
      <span v-if="video.size">{{ size }}</span>
    </div>
    <p v-if="error || unavailable" role="status">{{ error || unavailable }}</p>
  </section>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
  import { Loading, VideoPlay } from '@element-plus/icons-vue'
  import { maxVideoBytes, nativeImage, nativeVideo, ownVideo } from './messageMedia'
  import type { Workspace } from './workspaces'

  const props = defineProps<{
    video: { asset?: string; external?: boolean; duration: number; size: number }
    url?: string
    poster?: string
    workspace: Workspace
    scope: string
  }>()
  const root = ref<HTMLElement | null>(null)
  const player = ref<HTMLVideoElement | null>(null)
  const source = ref(''),
    cover = ref(''),
    error = ref(''),
    loading = ref(false)
  let controller = new AbortController()
  let posterController = new AbortController()
  let observer: IntersectionObserver | undefined
  let generation = 0
  let releaseVideo = () => {}
  const unavailable = computed(() =>
    props.video.size > maxVideoBytes
      ? '视频超过 16 MiB，仅显示信息与封面'
      : !props.url
        ? props.video.external
          ? '外链视频暂不读取'
          : '视频资源暂不可用'
        : ''
  )
  const duration = computed(() => {
    const seconds = props.video.duration
    return `${Math.floor(seconds / 60)
      .toString()
      .padStart(2, '0')}:${(seconds % 60).toString().padStart(2, '0')}`
  })
  const size = computed(() =>
    props.video.size < 1024 * 1024
      ? `${(props.video.size / 1024).toFixed(1)} KiB`
      : `${(props.video.size / (1024 * 1024)).toFixed(1)} MiB`
  )
  function clearVideo() {
    generation++
    controller.abort()
    controller = new AbortController()
    releaseVideo()
    releaseVideo = () => {}
    if (player.value) {
      player.value.pause()
      player.value.removeAttribute('src')
      player.value.load()
    }
    if (source.value) URL.revokeObjectURL(source.value)
    source.value = ''
    loading.value = false
    error.value = ''
  }
  function clearCover() {
    if (cover.value) URL.revokeObjectURL(cover.value)
    cover.value = ''
  }
  async function load() {
    if (loading.value || unavailable.value || !props.url) return
    releaseVideo = ownVideo(clearVideo)
    const current = generation
    loading.value = true
    error.value = ''
    try {
      const file = await nativeVideo(props.url, props.workspace, controller.signal)
      if (current !== generation) return
      source.value = URL.createObjectURL(file)
      await nextTick()
      if (current === generation) await player.value?.play().catch(() => {})
    } catch (e) {
      if (current === generation && !controller.signal.aborted) {
        error.value = e instanceof Error ? e.message : '视频暂不可用'
      }
    } finally {
      if (current === generation) loading.value = false
    }
  }
  function broken() {
    clearVideo()
    error.value = '浏览器无法播放此视频，请检查视频是否损坏或编码不受支持'
  }
  function observeCover() {
    observer?.disconnect()
    posterController.abort()
    posterController = new AbortController()
    clearCover()
    if (!props.poster || !root.value) return
    const signal = posterController.signal
    const path = props.poster
    const workspace = props.workspace
    observer = new IntersectionObserver((entries) => {
      if (!entries.some((entry) => entry.isIntersecting)) return
      observer?.disconnect()
      void nativeImage(path, workspace, signal).then(
        (file) => {
          if (!signal.aborted) cover.value = URL.createObjectURL(file)
        },
        () => {}
      )
    })
    observer.observe(root.value)
  }
  watch(
    () => [props.url, props.poster, props.scope, props.workspace],
    () => {
      clearVideo()
      observeCover()
    }
  )
  onMounted(observeCover)
  onUnmounted(() => {
    observer?.disconnect()
    posterController.abort()
    clearVideo()
    clearCover()
  })
</script>

<style scoped>
  .message-video {
    width: 260px;
    max-width: 100%;
  }
  .video-load,
  .message-video video {
    display: block;
    position: relative;
    width: 100%;
    aspect-ratio: 4 / 3;
    max-height: 320px;
    padding: 0;
    border: 0;
    border-radius: 4px;
    background: #252a27;
    object-fit: contain;
  }
  .video-load {
    color: white;
    cursor: pointer;
  }
  .video-load:disabled {
    cursor: default;
  }
  .video-load img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: 4px;
  }
  .video-load .el-icon {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    font-size: 36px;
    filter: drop-shadow(0 1px 3px #333);
  }
  .video-load > span {
    position: absolute;
    bottom: 10px;
    left: 0;
    width: 100%;
  }
  .video-meta {
    display: flex;
    justify-content: space-between;
    gap: 10px;
    margin-top: 6px;
    color: #68736e;
    font-size: 11px;
  }
  .message-video p {
    margin: 6px 0 0;
    color: #68736e;
    font-size: 12px;
    overflow-wrap: anywhere;
  }
</style>
