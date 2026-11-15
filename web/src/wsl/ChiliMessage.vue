<template>
  <div class="chili-message" :data-kind="view.kind">
    <div v-if="view.kind === 'friend_notice' && view.friend_notice" class="chili-notice">
      <strong>{{ friendNoticeLabel }}</strong>
      <span>发送方：{{ view.friend_notice.sender.name || view.friend_notice.sender.id }}</span>
      <span>接收方：{{ view.friend_notice.recipient.name || view.friend_notice.recipient.id }}</span>
      <span v-if="view.friend_notice.friend">
        好友：{{ view.friend_notice.friend.name || view.friend_notice.friend.id }}
      </span>
    </div>
    <div v-if="view.kind === 'system_notice' && view.system_notice" class="chili-notice">
      <strong>{{ systemNoticeLabel }}</strong>
    </div>
    <div v-if="view.kind === 'notice' && view.notice" class="chili-notice">
      <strong>{{ noticeLabel }}</strong>
      <span v-if="view.notice.actor">{{ view.notice.actor.name || view.notice.actor.id }}</span>
      <span v-for="person in view.notice.targets || []" :key="person.id">
        {{ person.name || person.id }}
      </span>
      <span v-if="view.notice.seconds">{{ view.notice.seconds }} 秒</span>
      <p v-if="view.text">{{ view.text }}</p>
    </div>
    <blockquote v-if="view.quote">
      <ChiliMessage :view="view.quote" :workspace="workspace" :scope="scope" />
    </blockquote>
    <p v-if="view.kind === 'text' || view.kind === 'mention' || view.kind === 'quote'">
      {{ view.text || '（空文本）' }}
    </p>
    <div v-if="view.mentions?.length" class="chili-mentions">
      <span v-for="(person, index) in view.mentions" :key="`${person.id}:${index}`">
        @{{ person.name || person.id }}
      </span>
    </div>
    <details v-if="view.kind === 'forward'">
      <summary>{{ view.title || '聊天记录' }} · {{ view.items?.length || 0 }} 条</summary>
      <ol>
        <li v-for="(item, index) in view.items || []" :key="index">
          <small v-if="item.title">{{ item.title }}</small>
          <ChiliMessage :view="item" :workspace="workspace" :scope="scope" />
        </li>
      </ol>
    </details>
    <MessageImage
      v-if="view.kind === 'image'"
      :url="view.media?.image"
      :workspace="workspace"
      :scope="scope"
    />
    <MessageVideo
      v-else-if="view.kind === 'video'"
      :video="{ duration: view.duration || 0, size: view.size || 0 }"
      :url="view.media?.video"
      :poster="view.media?.poster"
      :workspace="workspace"
      :scope="scope"
    />
    <div v-else-if="view.kind === 'contact' && view.contact" class="chili-contact">
      <MessageImage
        :url="view.media?.avatar"
        :workspace="workspace"
        :scope="scope"
        :label="view.contact.name || view.contact.id"
        avatar
      />
      <div><strong>{{ view.contact.name || '联系人' }}</strong><small>{{ view.contact.id }}</small></div>
    </div>
    <p v-else-if="mediaLabel" class="chili-resource">{{ mediaLabel }} · 资源尚不可预览</p>
    <p v-if="view.kind === 'unsupported'" class="chili-resource">
      未适配的辣椒聊原生类型 {{ view.content_type }}
    </p>
    <small v-if="view.truncated" class="chili-resource">内容已截断</small>
  </div>
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import MessageImage from './MessageImage.vue'
  import MessageVideo from './MessageVideo.vue'
  import type { ChiliMessageView } from './api'
  import type { Workspace } from './workspaces'

  const props = defineProps<{ view: ChiliMessageView; workspace: Workspace; scope: string }>()
  const mediaLabel = computed(
    () => ({ image: '图片', voice: '语音', video: '视频', file: '文件' })[props.view.kind]
  )
  const noticeLabel = computed(() => ({
    group_updated: '群资料已更新',
    member_left: '成员退出群组',
    member_kicked: '成员被移出群组',
    member_invited: '邀请成员入群',
    member_joined: '成员加入群组',
    member_muted: '成员被禁言',
    member_unmuted: '成员已解禁',
    group_muted: '已开启全群禁言',
    group_unmuted: '已解除全群禁言',
    member_updated: '群成员资料已更新',
    announcement_updated: '群公告已更新',
    name_updated: '群名称已更新',
    message_pinned: '群消息已置顶',
    message_unpinned: '群消息已取消置顶'
  })[props.view.notice?.event || ''] || '群通知')
  const friendNoticeLabel = computed(() => {
    const event = props.view.friend_notice?.event
    if (!event) return '好友通知'
    return (
      {
        friend_request: '好友申请',
        friend_approved: '好友申请已通过',
        friend_added: '已添加好友'
      }[event] || '好友通知'
    )
  })
  const systemNoticeLabel = computed(() =>
    props.view.system_notice?.event === 'read_receipt' ? '回执通知' : '系统通知'
  )
</script>

<style scoped>
  .chili-message {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  p {
    margin: 0;
    white-space: pre-wrap;
  }
  blockquote {
    margin: 0 0 8px;
    padding: 6px 10px;
    border-left: 3px solid var(--el-border-color);
  }
  .chili-mentions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 6px;
    color: var(--el-color-primary);
  }
  .chili-resource,
  small {
    color: var(--el-text-color-secondary);
  }
  .chili-notice {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 10px;
    color: var(--el-text-color-secondary);
  }
  .chili-notice p {
    flex-basis: 100%;
    color: var(--el-text-color-primary);
  }
  .chili-contact {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 0;
  }
  .chili-contact > div {
    min-width: 0;
    display: grid;
    gap: 4px;
  }
  details > summary {
    cursor: pointer;
    font-weight: 600;
  }
  ol {
    margin: 8px 0 0;
    padding-left: 20px;
  }
  li {
    padding: 8px 0;
    border-top: 1px solid var(--el-border-color-lighter);
  }
</style>
