<template>
  <span v-if="typeof value === 'boolean'">{{ value ? '开启' : '关闭' }}</span>
  <ul v-else-if="Array.isArray(value) && value.length" class="config-values">
    <li v-for="(entry, i) in value.slice(0, 200)" :key="i">
      <template v-if="object(entry) && typeof entry.keyword === 'string'">
        <strong>{{ entry.keyword }}</strong
        ><span class="reply-text">{{ entry.reply }}</span>
      </template>
      <span v-else>{{
        typeof entry === 'string' || typeof entry === 'number' ? entry : '不支持的字段'
      }}</span>
    </li>
  </ul>
  <span v-else-if="Array.isArray(value) || value == null" class="muted">未设置</span>
  <span v-else>{{
    typeof value === 'string' || typeof value === 'number' ? value : '不支持的字段'
  }}</span>
</template>
<script setup lang="ts">
  import { object } from './client'
  defineProps<{ value: unknown }>()
</script>
<style scoped>
  .config-values {
    margin: 0;
    padding-left: 18px;
    max-height: 260px;
    overflow: auto;
  }
  li {
    padding-block: 3px;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }
  .reply-text {
    display: block;
    color: var(--el-text-color-secondary);
    margin-block: 4px;
  }
  .muted {
    color: var(--el-text-color-secondary);
  }
</style>
