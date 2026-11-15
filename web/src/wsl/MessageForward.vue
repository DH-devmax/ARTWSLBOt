<template>
  <div class="message-forward">
    <button class="forward-open" type="button" @click="open = true">
      <strong>{{ forward.title || '聊天记录' }}</strong>
      <span v-for="(entry, index) in forward.entries.slice(0, 3)" :key="index">
        {{ entry.sender.name || entry.sender.id || '未知发言者' }}：{{ summary(entry) }}
      </span>
      <small
        >聊天记录 · {{ forward.total }} 条<span v-if="forward.truncated">
          · 部分内容截断</span
        ></small
      >
    </button>
    <ElDialog
      v-model="open"
      title="聊天记录"
      width="min(94vw, 640px)"
      append-to-body
      align-center
      destroy-on-close
      class="message-forward-dialog"
    >
      <div class="forward-entries">
        <article v-for="(entry, index) in forward.entries" :key="index" class="forward-entry">
          <MessageImage
            :url="links[index]?.avatar"
            :workspace="workspace"
            :scope="scope"
            :label="entry.sender.name"
            avatar
          />
          <div>
            <header
              ><strong>{{ entry.sender.name || entry.sender.id || '未知发言者' }}</strong
              ><time>{{ time(entry.created_at) }}</time></header
            >
            <p v-if="entry.reason">{{ reason(entry.reason) }}</p>
            <p v-else-if="entry.image?.preview_limited">图片 · 超出预览限制，消息已保留</p>
            <MessageImage
              v-else-if="entry.image"
              :url="links[index]?.image"
              :workspace="workspace"
              :scope="scope"
              :sticker="entry.image.sticker"
            />
            <MessageVideo
              v-else-if="entry.video"
              :video="entry.video"
              :url="links[index]?.video"
              :poster="links[index]?.poster"
              :workspace="workspace"
              :scope="scope"
            />
            <p v-else>{{ entry.text || '（空文本）' }}</p>
          </div>
        </article>
        <p v-if="!forward.entries.length">没有随消息附带的记录</p>
        <p v-if="forward.truncated" class="forward-note"
          >仅展示安全预览范围内的内容，未自动补拉其他聊天记录。</p
        >
      </div>
    </ElDialog>
  </div>
</template>

<script setup lang="ts">
  import { ref, watch } from 'vue'
  import type { Row } from './api'
  import type { Workspace } from './workspaces'
  import MessageImage from './MessageImage.vue'
  import MessageVideo from './MessageVideo.vue'
  const props = defineProps<{ forward: Row; links: Row[]; workspace: Workspace; scope: string }>()
  const open = ref(false)
  watch(
    () => props.scope,
    () => {
      open.value = false
    }
  )
  const reason = (value: string) =>
    value === 'nested_forward_unresolved'
      ? '嵌套聊天记录未随消息附带，未自动读取其他会话'
      : value === 'forward_content_missing'
        ? '原消息缺少正文'
        : '此记录类型尚未支持'
  const summary = (entry: Row) =>
    entry.reason
      ? reason(entry.reason)
      : entry.image
        ? entry.image.sticker
          ? '[表情包]'
          : '[图片]'
        : entry.video
          ? '[视频]'
          : entry.text
  const time = (value: number) => (value ? new Date(value).toLocaleString('zh-CN') : '')
</script>

<style scoped>
  .forward-open {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 240px;
    max-width: 100%;
    padding: 0;
    border: 0;
    text-align: left;
    color: inherit;
    background: transparent;
    cursor: pointer;
  }
  .forward-open > span {
    display: block;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: #777;
    font-size: 12px;
  }
  .forward-open strong {
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .forward-open small {
    padding-top: 8px;
    border-top: 1px solid #eee;
    color: #777;
    font-size: 11px;
  }
  .forward-entries {
    max-height: 65dvh;
    overflow: auto;
  }
  .forward-entry {
    display: flex;
    gap: 10px;
    padding: 12px 0;
    border-bottom: 1px solid #eee;
  }
  .forward-entry > div {
    flex: 1;
    min-width: 0;
  }
  .forward-entry header {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 6px;
    overflow-wrap: anywhere;
  }
  .forward-entry header strong {
    max-width: 100%;
  }
  .forward-entry time,
  .forward-note {
    color: #777;
    font-size: 12px;
  }
  .forward-entry p {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 0;
  }
</style>
