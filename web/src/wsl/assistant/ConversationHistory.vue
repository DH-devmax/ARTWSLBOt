<template>
  <section class="assistant-history" aria-label="历史对话列表">
    <div class="assistant-history-heading">
      <h3
        >最近对话 <span>{{ items.length }}</span></h3
      >
      <ElTooltip content="新对话">
        <ElButton
          :icon="Plus"
          text
          aria-label="在历史列表新建对话"
          :disabled="disabled || !canCreate"
          :loading="creating"
          @click="$emit('create')"
        />
      </ElTooltip>
    </div>
    <ElInput
      v-if="items.length > 5 || search"
      v-model="search"
      :prefix-icon="Search"
      size="small"
      clearable
      aria-label="搜索历史对话"
      placeholder="搜索对话"
    />
    <ul v-if="filtered.length" class="assistant-history-list">
      <li
        v-for="item in filtered"
        :key="item.id"
        class="assistant-history-item"
        :class="{ selected: item.id === active }"
        :data-session-id="item.id"
      >
        <button
          type="button"
          class="assistant-history-select"
          :aria-label="`打开对话 ${label(item)}`"
          :aria-current="item.id === active ? 'true' : undefined"
          :disabled="disabled"
          @click="$emit('select', item)"
        >
          <ElIcon><ChatLineRound /></ElIcon>
          <span :title="label(item)">{{ label(item) }}</span>
        </button>
        <ElTooltip content="删除对话" placement="right">
          <ElButton
            class="assistant-history-delete"
            :icon="Delete"
            text
            size="small"
            :aria-label="`删除对话 ${label(item)}`"
            :disabled="disabled"
            @click="$emit('delete', item)"
          />
        </ElTooltip>
      </li>
    </ul>
    <p v-else class="assistant-history-empty">{{ search ? '没有匹配的对话' : '暂无历史对话' }}</p>
  </section>
</template>
<script setup lang="ts">
  import { computed, ref } from 'vue'
  import { ChatLineRound, Delete, Plus, Search } from '@element-plus/icons-vue'
  import type { Row } from '../api'

  const props = defineProps<{
    items: Row[]
    active: string
    disabled: boolean
    canCreate: boolean
    creating: boolean
  }>()
  defineEmits<{ select: [item: Row]; delete: [item: Row]; create: [] }>()
  const search = ref('')
  const label = (item: Row) => (item.is_empty ? '新对话' : item.title || '历史对话（内容已失效）')
  const filtered = computed(() =>
    props.items.filter((item) =>
      label(item).toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())
    )
  )
</script>
<style scoped>
  .assistant-history {
    min-width: 0;
    color: var(--ui-text);
    letter-spacing: 0;
  }
  .assistant-history-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    min-height: 36px;
    margin-bottom: 8px;
  }
  h3 {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.5;
    color: var(--ui-ink);
  }
  h3 span {
    margin-left: 6px;
    color: var(--ui-muted);
    font-weight: 400;
    font-variant-numeric: tabular-nums;
  }
  .assistant-history-heading .el-button {
    width: 32px;
    height: 32px;
    padding: 0;
    border-radius: 6px;
  }
  .assistant-history :deep(.el-input__wrapper) {
    min-height: 34px;
    border-radius: 6px;
  }
  .assistant-history-list {
    list-style: none;
    padding: 0;
    margin: 8px 0 0;
    display: grid;
    gap: 4px;
  }
  .assistant-history-item {
    display: flex;
    align-items: center;
    min-width: 0;
    min-height: 44px;
    border: 1px solid transparent;
    border-radius: 6px;
    transition:
      border-color 120ms ease,
      background-color 120ms ease;
  }
  .assistant-history-item:hover,
  .assistant-history-item:focus-within {
    background: var(--ui-soft-deep);
  }
  .assistant-history-item.selected {
    border-color: var(--ui-line);
    background: var(--ui-paper);
    color: var(--ui-ink);
  }
  .assistant-history-item.selected .assistant-history-select {
    font-weight: 600;
  }
  .assistant-history-select {
    display: flex;
    flex: 1;
    gap: 9px;
    align-items: center;
    min-width: 0;
    min-height: 42px;
    padding: 10px 8px;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    font-size: 13px;
    line-height: 1.5;
    letter-spacing: 0;
    text-align: left;
    cursor: pointer;
    border-radius: 6px;
  }
  .assistant-history-select:focus-visible {
    outline: 2px solid var(--ui-ink);
    outline-offset: -2px;
    box-shadow: var(--ui-focus);
  }
  .assistant-history-select:disabled {
    color: var(--el-disabled-text-color);
    cursor: not-allowed;
  }
  .assistant-history-select span:last-child {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .assistant-history-select .el-icon {
    flex: 0 0 15px;
    font-size: 15px;
    color: var(--ui-muted);
  }
  .assistant-history-item.selected .assistant-history-select .el-icon {
    color: var(--ui-ink);
  }
  .assistant-history-delete {
    flex: 0 0 32px;
    width: 32px;
    height: 32px;
    padding: 0;
    margin-right: 4px;
    border-radius: 6px;
    color: var(--ui-muted);
  }
  .assistant-history-delete:hover:not(:disabled) {
    color: var(--el-color-danger);
    --el-button-hover-bg-color: var(--el-color-danger-light-9);
  }
  .assistant-history-empty {
    color: var(--ui-muted);
    font-size: 12px;
    line-height: 1.6;
    margin: 16px 8px;
  }
  @media (hover: hover) and (pointer: fine) {
    .assistant-history-delete {
      opacity: 0;
    }
    .assistant-history-item:hover .assistant-history-delete,
    .assistant-history-item:focus-within .assistant-history-delete,
    .assistant-history-item.selected .assistant-history-delete {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .assistant-history-item {
      transition: none;
    }
  }
</style>
