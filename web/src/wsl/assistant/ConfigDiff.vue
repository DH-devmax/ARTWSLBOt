<template>
  <div class="config-diff">
    <section v-for="item in rows" :key="item.id" class="diff-field">
      <h4>{{ item.label }}</h4>
      <div class="diff-columns">
        <div><small>变更前</small><ConfigValue :value="item.before" /></div>
        <div><small>保存后</small><ConfigValue :value="item.after" /></div>
      </div>
    </section>
  </div>
</template>
<script setup lang="ts">
  import { computed } from 'vue'
  import ConfigValue from './ConfigValue.vue'
  const props = defineProps<{
    before: Record<string, Record<string, unknown>>
    changes: Record<string, Record<string, unknown>>
  }>()
  const features: Record<string, string> = {
    blocked_words: '拦截词',
    blacklist: '黑名单',
    exemptions: '豁免名单',
    keyword_replies: '关键词回复'
  }
  const fields: Record<string, string> = {
    words: '词语',
    members: '成员',
    rules: '回复规则',
    enabled: '功能开关',
    admin_only: '仅管理员触发'
  }
  const rows = computed(() =>
    Object.entries(props.changes || {}).flatMap(([feature, values]) =>
      Object.entries(values).map(([field, after]) => ({
        id: `${feature}.${field}`,
        label: `${features[feature] || feature} · ${fields[field] || field}`,
        before: props.before?.[feature]?.[field],
        after
      }))
    )
  )
</script>
<style scoped>
  .config-diff {
    margin-block: 20px;
  }
  .diff-field {
    border-bottom: 1px solid var(--el-border-color-lighter);
    padding-block: 14px;
  }
  h4 {
    font-size: 14px;
    margin: 0 0 12px;
  }
  .diff-columns {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 20px;
  }
  .diff-columns > div {
    min-width: 0;
  }
  small {
    color: var(--el-text-color-secondary);
    display: block;
    margin-bottom: 8px;
  }
  @media (max-width: 480px) {
    .diff-columns {
      grid-template-columns: 1fr;
      gap: 12px;
    }
  }
</style>
