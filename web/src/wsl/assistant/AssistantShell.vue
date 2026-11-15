<template>
  <section v-if="embedded" class="assistant-workspace" aria-label="助手会话工作台">
    <header class="assistant-workspace-header"><slot name="header" /></header>
    <aside class="assistant-workspace-sidebar"><slot name="sidebar" /></aside>
    <div class="assistant-workspace-body"><slot /></div>
    <footer class="assistant-workspace-footer"><slot name="footer" /></footer>
  </section>
  <ElDrawer
    v-else
    :model-value="open"
    title="群管助手"
    size="min(560px, 100vw)"
    append-to-body
    class="assistant-drawer"
    @update:model-value="$emit('update:open', $event)"
  >
    <template #header><slot name="header" /></template>
    <aside class="assistant-drawer-scope"><slot name="sidebar" /></aside>
    <slot />
    <template #footer><slot name="footer" /></template>
  </ElDrawer>
</template>
<script setup lang="ts">
  import { ElDrawer } from 'element-plus'
  defineProps<{ embedded?: boolean; open: boolean }>()
  defineEmits<{ 'update:open': [value: boolean] }>()
</script>
<style scoped>
  .assistant-workspace {
    display: grid;
    grid-template-columns: 248px minmax(0, 1fr);
    grid-template-rows: auto minmax(0, 1fr) auto;
    height: calc(100dvh - 155px);
    min-height: 480px;
    min-width: 0;
    background: var(--ui-paper);
    border: 1px solid var(--ui-line);
    border-radius: 8px;
    overflow: hidden;
    color: var(--ui-ink);
    letter-spacing: 0;
  }
  .assistant-workspace-header {
    grid-column: 2;
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: nowrap;
    gap: 12px;
    padding: 10px 24px;
    border-bottom: 1px solid var(--ui-line-soft);
    font-size: 13px;
  }
  .assistant-workspace-body {
    grid-column: 2;
    grid-row: 2;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow: hidden;
    padding: 14px 24px 0;
  }
  .assistant-workspace-body :deep(> .el-alert) {
    flex-shrink: 0;
  }
  .assistant-workspace-footer {
    grid-column: 2;
    position: relative;
    padding: 8px 24px 10px;
    border-top: 1px solid var(--ui-line-soft);
    background: var(--ui-paper);
  }
  .assistant-workspace-sidebar {
    grid-column: 1;
    grid-row: 1 / 4;
    padding: 16px 12px;
    background: var(--ui-soft);
    border-right: 1px solid var(--ui-line);
    overflow-y: auto;
    overscroll-behavior: contain;
    min-width: 0;
  }
  /* Element Plus teleports the drawer outside the component root. */
  :global(.assistant-drawer) {
    min-width: 0;
    max-width: 100vw;
    transition-property: transform;
    background: var(--ui-paper);
    letter-spacing: 0;
  }
  :global(.assistant-drawer .el-drawer__header) {
    gap: 12px;
    margin-bottom: 0;
    padding: 18px 20px;
    border-bottom: 1px solid var(--ui-line-soft);
  }
  :global(.assistant-drawer .el-drawer__body) {
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
    padding: 0 20px;
  }
  :global(.assistant-drawer .el-drawer__footer) {
    position: relative;
    padding: 14px 20px max(16px, env(safe-area-inset-bottom));
    border-top: 1px solid var(--ui-line-soft);
    background: var(--ui-paper);
  }
  :global(.assistant-drawer .el-alert) {
    flex-shrink: 0;
  }
  .assistant-drawer-scope {
    flex-shrink: 0;
    min-width: 0;
    padding: 16px 0;
    border-bottom: 1px solid var(--ui-line-soft);
  }
  @media (max-width: 900px) {
    .assistant-workspace {
      grid-template-columns: 210px minmax(0, 1fr);
    }
    .assistant-workspace-body {
      padding-inline: 16px;
    }
    .assistant-workspace-footer {
      padding-inline: 16px;
    }
  }
  @media (max-width: 767px) {
    .assistant-workspace {
      display: flex;
      flex-direction: column;
      height: calc(100dvh - 56px);
      min-height: 0;
    }
    .assistant-workspace-header {
      padding: 6px 14px;
      flex-wrap: nowrap;
    }
    .assistant-workspace-sidebar {
      flex: 0 0 auto;
      max-height: 34dvh;
      padding: 0 14px 4px;
      border-right: 0;
      border-bottom: 1px solid var(--ui-line-soft);
      background: var(--ui-paper);
      overflow-y: auto;
    }
    .assistant-workspace-body {
      flex: 1;
      padding: 8px 14px 0;
    }
    .assistant-workspace-footer {
      flex: 0 0 auto;
      padding: 8px 14px max(6px, env(safe-area-inset-bottom));
    }
  }
  @media (max-width: 390px) {
    :global(.assistant-drawer .el-drawer__header),
    :global(.assistant-drawer .el-drawer__body),
    :global(.assistant-drawer .el-drawer__footer) {
      padding-inline: 12px;
    }
  }
</style>
