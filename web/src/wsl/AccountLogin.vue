<template>
  <ElDialog
    :model-value="modelValue && step !== 'login'"
    title="添加机器人"
    width="344px"
    class="robot-add-picker"
    align-center
    append-to-body
    :close-on-click-modal="false"
    @update:model-value="pickerChanged"
  >
    <div v-if="step === 'purpose'" class="robot-add-options">
      <button v-if="managementPlatforms.length" type="button" @click="choosePurpose('management')">
        <img src="/robot.svg" alt="" />
        <span>群管机器人</span>
      </button>
      <button v-if="canChat" type="button" @click="choosePurpose('chat')">
        <img :src="workspaceLogos.maibot" alt="" />
        <span>热群机器人</span>
      </button>
    </div>
    <template v-else>
      <div class="robot-add-step">
        <ElButton text :icon="ArrowLeft" aria-label="返回选择用途" @click="step = 'purpose'" />
        <span>{{ purpose === 'chat' ? '热群机器人' : '群管机器人' }} · 选择平台</span>
      </div>
      <div class="robot-add-options">
        <button v-for="item in platforms" :key="item" type="button" @click="choosePlatform(item)">
          <img :src="workspaceLogos[item]" alt="" />
          <span>{{ item === 'wsl' ? '旺商聊' : '辣椒聊' }}</span>
        </button>
      </div>
    </template>
  </ElDialog>
  <DesktopLogin
    ref="desktop"
    :model-value="modelValue && step === 'login' && platform === 'wsl'"
    :account="account"
    :owner="owner"
    :workspace="loginWorkspace"
    @update:model-value="loginChanged($event, 'wsl')"
    @updated="emit('updated')"
    @created="created"
  />
  <LajiaoLogin
    :model-value="modelValue && step === 'login' && platform === 'lajiaoliao'"
    :account="account"
    :owner="owner"
    :workspace="loginWorkspace"
    @update:model-value="loginChanged($event, 'lajiaoliao')"
    @updated="emit('updated')"
    @created="created"
  />
</template>

<script setup lang="ts">
  import { computed, ref, watch } from 'vue'
  import { ElButton, ElDialog } from 'element-plus'
  import { ArrowLeft } from '@element-plus/icons-vue'
  import DesktopLogin from './NativeLogin.vue'
  import LajiaoLogin from './LajiaoLogin.vue'
  import type { Row } from './api'
  import { workspaceLogos, type Workspace, type WorkspaceEntry } from './workspaces'

  const props = defineProps<{
    modelValue: boolean
    account: Row | null
    owner: string
    workspace: Workspace
    panels: WorkspaceEntry[]
  }>()
  const emit = defineEmits<{ 'update:modelValue': [boolean]; updated: []; created: [Row] }>()
  type Platform = 'wsl' | 'lajiaoliao'
  const platform = ref<Platform>('wsl')
  const purpose = ref<'management' | 'chat'>('management')
  const step = ref<'purpose' | 'platform' | 'login'>('purpose')
  const desktop = ref<InstanceType<typeof DesktopLogin>>()
  const managementPlatforms = computed(() =>
    (['wsl', 'lajiaoliao'] as Platform[]).filter((id) =>
      props.panels.some((panel) => panel.id === id)
    )
  )
  const canChat = computed(() => props.panels.some((panel) => panel.id === 'maibot'))
  const platforms = computed<Platform[]>(() =>
    purpose.value === 'chat' && canChat.value ? ['wsl', 'lajiaoliao'] : managementPlatforms.value
  )
  const loginWorkspace = computed<Workspace>(() =>
    props.account ? props.workspace : purpose.value === 'chat' ? 'maibot' : platform.value
  )
  watch(
    () => [props.modelValue, props.account?.id, props.workspace, props.owner],
    () => {
      if (!props.modelValue) return
      platform.value =
        props.account?.platform === 'lajiaoliao' ||
        (!props.account && props.workspace === 'lajiaoliao')
          ? 'lajiaoliao'
          : 'wsl'
      purpose.value = props.workspace === 'maibot' ? 'chat' : 'management'
      step.value = props.account ? 'login' : 'purpose'
    },
    { immediate: true }
  )
  function choosePurpose(value: 'management' | 'chat') {
    if (value === 'chat' ? !canChat.value : !managementPlatforms.value.length) return
    purpose.value = value
    step.value = 'platform'
  }
  function choosePlatform(value: Platform) {
    if (!platforms.value.includes(value)) return
    platform.value = value
    step.value = 'login'
  }
  function pickerChanged(value: boolean) {
    // Element Plus also emits false after the chooser's leave animation.
    if (step.value !== 'login') emit('update:modelValue', value)
  }
  function loginChanged(value: boolean, source: Platform) {
    if (step.value === 'login' && platform.value === source) emit('update:modelValue', value)
  }
  function created(account: Row) {
    emit('created', {
      ...account,
      workspace: loginWorkspace.value,
      platform: platform.value,
      purpose: purpose.value
    })
  }
  defineExpose({ forgetAccount: (id: string) => desktop.value?.forgetAccount(id) })
</script>

<style>
  .el-dialog.robot-add-picker {
    width: min(344px, calc(100vw - 24px)) !important;
    max-height: calc(100dvh - 24px);
    padding: 16px;
  }
  .robot-add-picker > .el-dialog__header {
    padding: 0 28px 12px 0;
    margin: 0;
  }
  .robot-add-picker > .el-dialog__body {
    padding: 0 !important;
  }
  .robot-add-picker .el-dialog__title {
    font-size: 16px;
  }
  .robot-add-step {
    display: flex;
    gap: 4px;
    align-items: center;
    margin-bottom: 12px;
    font-size: 13px;
    color: var(--el-text-color-secondary);
  }
  .robot-add-options {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }
  .robot-add-options > button {
    min-width: 0;
    min-height: 88px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 12px 8px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 6px;
    background: var(--el-fill-color-light);
    color: var(--el-text-color-primary);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .robot-add-options > button:hover,
  .robot-add-options > button:focus-visible {
    border-color: var(--el-color-primary);
    outline: none;
  }
  .robot-add-options img {
    width: 32px;
    height: 32px;
    object-fit: contain;
  }
</style>
