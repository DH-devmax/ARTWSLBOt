<template>
  <div class="native-security-panel">
    <ElAlert v-if="!supported" title="当前后端未开放此接口" type="info" :closable="false" />
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <div class="settings-avatar-preview">
      <MessageImage
        class="settings-current-avatar"
        :url="avatarUrl"
        :workspace="workspace"
        :scope="scope"
        :label="account.name"
        avatar
      />
      <div><strong>当前头像</strong><small>JPG / PNG，最大 1 MB</small></div>
    </div>
    <div v-if="preview" class="settings-avatar-preview settings-avatar-candidate">
      <img :src="preview" alt="待上传头像" />
      <div><strong>待上传的新头像</strong><small>尚未保存</small></div>
    </div>
    <input
      aria-label="选择头像"
      type="file"
      accept="image/jpeg,image/png"
      :disabled="disabled"
      @change="chooseImage"
    />
    <ElButton type="primary" :disabled="disabled || !image" :loading="busy" @click="submit">
      上传并保存头像
    </ElButton>
  </div>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import { ElMessageBox } from 'element-plus'
  import { api, type Row, type Workspace } from './api'
  import MessageImage from './MessageImage.vue'

  const props = defineProps<{
    account: Row
    scope: string
    workspace: Workspace
    snapshot: Row | null
    avatarUrl: string
    blocked: boolean
    supported: boolean
    capabilities?: Row
    operation: Row | null
  }>()
  const emit = defineEmits<{
    busy: [value: boolean]
    pending: [key: string]
    operation: [value: Row]
    saved: []
  }>()
  const busy = ref(false),
    error = ref(''),
    image = ref(''),
    preview = ref('')
  let generation = 0,
    controller = new AbortController()
  const disabled = computed(
    () =>
      props.blocked ||
      busy.value ||
      !props.supported ||
      !props.snapshot ||
      (props.capabilities && props.capabilities.avatar !== true)
  )
  const dirty = computed(() => !!image.value)
  function clearImage() {
    image.value = ''
    if (preview.value) URL.revokeObjectURL(preview.value)
    preview.value = ''
  }
  async function chooseImage(event: Event) {
    const input = event.target as HTMLInputElement,
      file = input.files?.[0]
    if (!file || disabled.value) return
    const version = generation
    error.value = ''
    if (
      !['image/jpeg', 'image/png'].includes(file.type) ||
      file.size > 1024 * 1024 ||
      file.size < 1
    ) {
      input.value = ''
      error.value = '请选择不超过 1 MB 的 JPG / PNG 图片'
      return
    }
    const bytes = new Uint8Array(await file.arrayBuffer())
    if (version !== generation) return
    let binary = ''
    for (const byte of bytes) binary += String.fromCharCode(byte)
    clearImage()
    image.value = btoa(binary)
    preview.value = URL.createObjectURL(file)
    input.value = ''
  }
  async function submit() {
    if (disabled.value || !props.snapshot || !image.value) return
    const version = generation,
      snapshot = props.snapshot
    busy.value = true
    emit('busy', true)
    error.value = ''
    let posted = false
    try {
      await ElMessageBox.confirm('确认将所选图片设置为此机器人的原生头像？', '确认账号操作', {
        confirmButtonText: '确认提交',
        cancelButtonText: '取消',
        closeOnClickModal: false,
        customClass: 'native-settings-confirm',
        type: 'warning'
      })
      if (version !== generation) return
      const key = crypto.randomUUID()
      emit('pending', key)
      posted = true
      const result = await api<Row>(
        `/accounts/${props.account.id}/native-security-operations`,
        'POST',
        {
          kind: 'avatar',
          input: { image: image.value },
          digest: snapshot.digest,
          account_epoch: snapshot.account_epoch,
          account_version: snapshot.account_version
        },
        controller.signal,
        props.workspace,
        key
      )
      if (version !== generation) return
      emit('operation', result.operation)
      if (result.operation.status === 'succeeded') {
        clearImage()
        emit('saved')
      }
    } catch (cause) {
      if (version !== generation) return
      if (cause !== 'cancel' && cause !== 'close')
        error.value = (cause as Error).message || '操作未完成'
      if (posted) error.value += '；请按原请求编号查询结果，不要重复提交。'
    } finally {
      if (version === generation) {
        busy.value = false
        emit('busy', false)
      }
    }
  }
  watch(
    () => props.scope,
    () => {
      generation++
      controller.abort()
      controller = new AbortController()
      clearImage()
      error.value = ''
      busy.value = false
      emit('busy', false)
    }
  )
  onUnmounted(() => {
    generation++
    controller.abort()
    if (busy.value) ElMessageBox.close()
    clearImage()
  })
  defineExpose({ dirty })
</script>

<style scoped>
  .native-security-panel {
    display: grid;
    gap: 16px;
  }
  .settings-avatar-preview {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .settings-avatar-preview img,
  .settings-current-avatar {
    width: 64px;
    height: 64px;
    flex-shrink: 0;
    object-fit: cover;
    border-radius: 8px;
  }
  .settings-avatar-preview div {
    display: grid;
    gap: 6px;
    min-width: 0;
  }
  .settings-avatar-preview small {
    color: #6b7280;
    font-size: 12px;
  }
  input[type='file'] {
    width: 100%;
    min-width: 0;
    font-size: 12px;
  }
</style>
