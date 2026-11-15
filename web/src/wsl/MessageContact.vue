<template>
  <section class="message-contact" aria-label="联系人名片" data-testid="message-contact">
    <div class="contact-person">
      <MessageImage
        :url="avatar"
        :workspace="workspace"
        :scope="scope"
        :label="contact.nickname || '用户'"
        avatar
      />
      <div class="contact-details">
        <strong>{{ contact.nickname || `用户 ${contact.user_id}` }}</strong>
        <span>账号 {{ contact.account_id || contact.user_id }}</span>
      </div>
    </div>
    <footer>个人名片</footer>
  </section>
</template>

<script setup lang="ts">
  import MessageImage from './MessageImage.vue'
  import type { Workspace } from './workspaces'

  defineProps<{
    contact: { user_id: number; account_id?: number; nickname?: string }
    avatar?: string
    workspace: Workspace
    scope: string
  }>()
</script>

<style scoped>
  .message-contact {
    width: 240px;
    max-width: 100%;
  }
  .contact-person {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 4px 0 12px;
  }
  .contact-details {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 5px;
    overflow-wrap: anywhere;
  }
  .contact-details strong {
    font-size: 14px;
    line-height: 1.5;
    font-weight: 500;
  }
  .contact-details span {
    font-size: 12px;
    color: #68736e;
  }
  .message-contact footer {
    padding-top: 7px;
    border-top: 1px solid #e1e5e2;
    color: #68736e;
    font-size: 11px;
  }
</style>
