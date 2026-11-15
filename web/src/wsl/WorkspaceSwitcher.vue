<template>
  <ElTooltip content="切换平台">
    <ElButton text :icon="Switch" aria-label="切换工作台" @click="open = true" />
  </ElTooltip>
  <ElDialog
    v-model="open"
    title="选择平台"
    width="344px"
    class="workspace-picker"
    align-center
    append-to-body
  >
    <div class="workspace-grid">
      <button
        v-for="item in platformItems"
        :key="item.id"
        type="button"
        :class="['workspace-item', { selected: item.id === selected }]"
        :aria-pressed="item.id === selected"
        @click="choose(item.id)"
      >
        <img :src="item.id === 'user' ? '' : workspaceLogos[item.id]" alt="" />
        <span>{{ item.name }}</span>
        <ElIcon v-if="item.id === selected" class="workspace-check"><Check /></ElIcon>
      </button>
    </div>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, ref } from 'vue'
  import { ElButton, ElDialog, ElIcon, ElTooltip } from 'element-plus'
  import { Switch, Check } from '@element-plus/icons-vue'
  import { workspaceLogos, type Console, type Workspace, type WorkspaceEntry } from './workspaces'
  const props = defineProps<{ items: WorkspaceEntry[]; selected: Workspace; console?: Console }>()
  const platformItems = computed(() =>
    props.items.filter(
      (item) => item.id !== 'user' && (props.console !== 'user' || item.id !== 'maibot')
    )
  )
  const emit = defineEmits<{ select: [Workspace] }>()
  const open = ref(false)
  function choose(id: Workspace) {
    emit('select', id)
    open.value = false
  }
</script>

<style>
  .el-dialog.workspace-picker {
    /* Keep this small chooser independent of the framework's mobile form dialog. */
    width: min(344px, calc(100vw - 24px)) !important;
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 24px);
    padding: 16px;
  }
  .workspace-picker > .el-dialog__header {
    padding: 0 28px 12px 0;
    margin: 0;
  }
  .workspace-picker > .el-dialog__header .el-dialog__title {
    font-size: 16px;
  }
  .workspace-picker > .el-dialog__body {
    padding: 0 !important;
    overflow-y: auto;
  }
  .workspace-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }
  .workspace-item {
    min-width: 0;
    position: relative;
    min-height: 82px;
    border: 1px solid transparent;
    border-radius: 6px;
    background: var(--el-fill-color-light);
    color: var(--el-text-color-primary);
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 12px 8px;
    cursor: pointer;
    font: inherit;
    font-size: 13px;
    line-height: 20px;
  }
  .workspace-item.selected,
  .workspace-item:hover {
    border-color: var(--el-color-primary);
  }
  .workspace-item.selected {
    background: var(--el-color-primary-light-9);
  }
  .workspace-item:focus-visible {
    outline: 2px solid var(--el-color-primary);
    outline-offset: 2px;
  }
  .workspace-item > img {
    width: 28px;
    height: 28px;
    object-fit: contain;
  }
  .workspace-item > .el-icon {
    font-size: 28px;
  }
  .workspace-item > .workspace-check {
    position: absolute;
    right: 7px;
    top: 7px;
    font-size: 13px;
    color: var(--el-color-primary);
  }
  .workspace-icon-user {
    color: #087f8c;
  }
  .workspace-item > span {
    max-width: 100%;
    overflow-wrap: anywhere;
  }
</style>
