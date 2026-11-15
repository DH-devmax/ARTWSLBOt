<template>
  <section class="assistant-group-picker" aria-label="授权群组选择">
    <ElInput
      v-if="groups.length > 6 || hasMore || search.length"
      v-model="search"
      :prefix-icon="Search"
      clearable
      aria-label="搜索授权群组"
      placeholder="搜索群名"
      :disabled="disabled"
      @input="$emit('search', search)"
    />
    <div class="assistant-group-buttons">
      <ElButton
        v-for="item in visible"
        :key="item.group_id"
        :type="selected === item.group_id ? 'primary' : 'default'"
        :plain="selected !== item.group_id"
        :aria-pressed="selected === item.group_id"
        :aria-label="`选择群组 ${item.name} · ${account(item)} · ${String(item.group_id).slice(0, 8)}`"
        :disabled="disabled"
        @click="$emit('select', item.group_id)"
      >
        <img
          v-if="item.platform === 'wsl' || item.platform === 'lajiaoliao'"
          class="assistant-group-icon"
          :src="item.platform === 'lajiaoliao' ? '/lajiaoliao-logo.png' : '/wsl-logo.png'"
          :alt="item.platform === 'lajiaoliao' ? '辣椒聊' : '旺商聊'"
          width="20"
          height="20"
        />
        <ElIcon v-else class="assistant-group-icon"><ChatDotRound /></ElIcon>
        <span class="assistant-group-details">
          <span class="assistant-group-name">{{ item.name }}</span>
          <small
            >{{ item.platform === 'lajiaoliao' ? '辣椒聊' : '旺商聊' }} · {{ account(item) }} ·
            {{ String(item.native_id || item.group_id).slice(-8) }}</small
          >
        </span>
        <ElIcon v-if="selected === item.group_id" class="assistant-group-check"><Check /></ElIcon>
      </ElButton>
    </div>
    <ElButton
      v-if="filtered.length > 6"
      :icon="expanded ? ArrowUp : ArrowDown"
      text
      size="small"
      :disabled="disabled"
      @click="expanded = !expanded"
      >{{ expanded ? '收起群组' : `更多群组（${filtered.length - 6}）` }}</ElButton
    >
    <ElEmpty v-if="!filtered.length" description="暂无匹配的授权群" :image-size="32" />
    <ElButton v-if="hasMore" text :loading="loading" :disabled="disabled" @click="$emit('more')"
      >加载更多群组</ElButton
    >
  </section>
</template>
<script setup lang="ts">
  import { computed, ref } from 'vue'
  import { ArrowDown, ArrowUp, Check, ChatDotRound, Search } from '@element-plus/icons-vue'
  import type { Row } from '../api'
  import { accountOptionLabel } from '../accountIdentity'
  const props = defineProps<{
    groups: Row[]
    accounts: Row[]
    selected?: string
    request?: string
    disabled?: boolean
    hasMore?: boolean
    loading?: boolean
  }>()
  defineEmits<{ select: [id: string]; more: []; search: [value: string] }>()
  const search = ref(''),
    expanded = ref(false)
  const account = (item: Row) =>
    props.accounts.some((a) => a.id === item.primary_account_id)
      ? accountOptionLabel(props.accounts.find((a) => a.id === item.primary_account_id))
      : item.robot_nickname || '群机器人'
  const filtered = computed(() => {
    const query = search.value.trim().toLocaleLowerCase()
    const matches = props.groups.filter(
      (item) => !query || `${item.name} ${account(item)}`.toLocaleLowerCase().includes(query)
    )
    return [...matches].sort((a, b) => {
      const priority = (item: Row) =>
        item.group_id === props.selected
          ? 2
          : props.request && item.name && props.request.includes(item.name)
            ? 1
            : 0
      return priority(b) - priority(a) || String(a.name).localeCompare(String(b.name), 'zh-CN')
    })
  })
  const visible = computed(() =>
    expanded.value || search.value.trim() ? filtered.value : filtered.value.slice(0, 6)
  )
</script>
<style scoped>
  .assistant-group-picker {
    min-width: 0;
    color: var(--ui-text);
    letter-spacing: 0;
  }
  .assistant-group-picker :deep(.el-input__wrapper) {
    min-height: 36px;
    border-radius: 6px;
  }
  .assistant-group-buttons {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
    max-height: 224px;
    overflow-y: auto;
    overscroll-behavior: contain;
    margin-top: 8px;
    padding: 2px;
  }
  .assistant-group-buttons .el-button {
    --el-button-bg-color: transparent;
    --el-button-text-color: var(--ui-text);
    --el-button-border-color: transparent;
    --el-button-hover-bg-color: var(--ui-soft-deep);
    --el-button-hover-text-color: var(--ui-ink);
    --el-button-hover-border-color: var(--ui-line);
    --el-button-active-bg-color: var(--ui-soft-deep);
    --el-button-active-text-color: var(--ui-ink);
    --el-button-active-border-color: var(--ui-ink);
    --el-button-disabled-bg-color: transparent;
    --el-button-disabled-border-color: transparent;
    --el-button-disabled-text-color: var(--el-disabled-text-color);
    min-width: 0;
    height: auto;
    min-height: 64px;
    margin: 0;
    padding: 10px 8px;
    border-radius: 6px;
    text-align: left;
    justify-content: flex-start;
    letter-spacing: 0;
  }
  .assistant-group-buttons .el-button:focus-visible {
    outline: 2px solid var(--ui-ink);
    outline-offset: -2px;
    box-shadow: var(--ui-focus);
  }
  .assistant-group-buttons :deep(.el-button > span) {
    display: grid;
    grid-template-columns: 20px minmax(0, 1fr) 20px;
    align-items: center;
    min-width: 0;
    width: 100%;
    gap: 10px;
  }
  .assistant-group-details {
    display: grid;
    gap: 4px;
    min-width: 0;
  }
  .assistant-group-icon {
    display: block;
    width: 20px;
    height: 20px;
    object-fit: contain;
    border-radius: 4px;
    font-size: 18px;
    color: var(--ui-muted);
  }
  .assistant-group-check {
    width: 20px;
    height: 20px;
    padding: 3px;
    border-radius: 4px;
    background: var(--ui-lime);
    color: var(--ui-ink);
    font-size: 14px;
  }
  .assistant-group-buttons .el-button--primary {
    --el-button-bg-color: var(--ui-paper);
    --el-button-text-color: var(--ui-ink);
    --el-button-border-color: var(--ui-line);
    --el-button-hover-bg-color: var(--ui-paper);
    --el-button-hover-text-color: var(--ui-ink);
    --el-button-hover-border-color: var(--ui-ink);
    --el-button-disabled-bg-color: var(--ui-soft);
    --el-button-disabled-border-color: var(--ui-line);
    --el-button-disabled-text-color: var(--el-disabled-text-color);
  }
  .assistant-group-buttons .el-button--primary .assistant-group-name {
    font-weight: 600;
  }
  .assistant-group-buttons .is-disabled .assistant-group-icon,
  .assistant-group-buttons .is-disabled .assistant-group-check {
    opacity: 0.5;
  }
  .assistant-group-name {
    width: 100%;
    font-size: 13px;
    white-space: normal;
    overflow-wrap: anywhere;
    line-height: 1.5;
  }
  small {
    color: var(--ui-muted);
    font-size: 11px;
    line-height: 1.5;
    overflow-wrap: anywhere;
    white-space: normal;
  }
  .assistant-group-picker :deep(.el-empty) {
    padding: 20px 8px;
  }
  @media (max-width: 767px) {
    .assistant-group-buttons {
      max-height: 160px;
    }
  }
</style>
