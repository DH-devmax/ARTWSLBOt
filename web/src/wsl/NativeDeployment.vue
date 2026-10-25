<template>
  <ElButton :icon="Setting" @click="open">协议配置</ElButton>
  <ElDialog
    v-model="visible"
    title="原生协议配置"
    class="platform-config-dialog"
    align-center
    width="560px"
    :close-on-click-modal="false"
    @close="reset"
  >
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElDescriptions :column="1" border class="native-info">
      <ElDescriptionsItem label="配置">{{
        status.configured ? '已配置' : '尚未导入'
      }}</ElDescriptionsItem>
      <ElDescriptionsItem label="协议版本">{{ status.protocol_version || '—' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="服务地址">{{ status.origin || '—' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="加密运行库">{{
        status.crypto_ready ? '可用' : '未启用 libsodium 构建'
      }}</ElDescriptionsItem>
    </ElDescriptions>
    <ElForm label-position="top" @submit.prevent="save">
      <ElFormItem label="协议配置 JSON">
        <input
          ref="fileInput"
          type="file"
          accept=".json,application/json"
          aria-label="协议配置 JSON"
          :disabled="saving"
          @change="selectFile"
        />
      </ElFormItem>
      <ElAlert
        title="配置仅加密保存到服务端，不返回签名密钥。替换配置会使已有原生会话需要重新登录。"
        type="warning"
        :closable="false"
      />
    </ElForm>
    <ElDivider />
    <ElForm label-position="top" @submit.prevent="saveIM">
      <ElFormItem
        :label="imStatus.configured ? '原生 IM 配置 · 已配置' : '原生 IM 配置 · 尚未导入'"
      >
        <ElInput
          v-model="imKey"
          type="password"
          autocomplete="off"
          aria-label="原生 IM AppKey"
          :disabled="savingIM"
        />
      </ElFormItem>
      <ElButton :loading="savingIM" :disabled="!imKey" :icon="Upload" @click="saveIM"
        >保存 IM 配置</ElButton
      >
    </ElForm>
    <template #footer>
      <ElButton :disabled="saving" @click="visible = false">关闭</ElButton>
      <ElButton
        type="primary"
        :icon="Upload"
        :disabled="!configuration"
        :loading="saving"
        @click="save"
        >导入并保存</ElButton
      >
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { ref } from 'vue'
  import { Setting, Upload } from '@element-plus/icons-vue'
  import { api, type Row } from './api'

  const emit = defineEmits<{ updated: [] }>()
  const visible = ref(false),
    saving = ref(false),
    error = ref(''),
    configuration = ref(''),
    imKey = ref(''),
    savingIM = ref(false)
  const imStatus = ref<Row>({})
  const status = ref<Row>({}),
    fileInput = ref<HTMLInputElement>()
  let generation = 0
  let selection = 0
  function reset() {
    generation++
    selection++
    configuration.value = ''
    imKey.value = ''
    error.value = ''
    if (fileInput.value) fileInput.value.value = ''
  }
  async function open() {
    visible.value = true
    const current = ++generation
    try {
      const [value, im] = await Promise.all([
        api<Row>('/native-deployment'),
        api<Row>('/native-im-profile')
      ])
      if (current === generation) {
        status.value = value
        imStatus.value = im
      }
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    }
  }
  async function selectFile(event: Event) {
    const selected = ++selection
    configuration.value = ''
    const file = (event.target as HTMLInputElement).files?.[0]
    if (!file) return
    if (file.size > 60000) {
      error.value = '配置文件不能超过 60KB'
      return
    }
    const current = generation
    const text = await file.text()
    if (current === generation && selected === selection) configuration.value = text
  }
  async function save() {
    if (saving.value || !configuration.value) return
    saving.value = true
    error.value = ''
    const current = generation
    try {
      await api('/native-deployment', 'PUT', {
        version: status.value.version || 0,
        configuration: configuration.value
      })
      if (current === generation) {
        configuration.value = ''
        visible.value = false
      }
      emit('updated')
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  async function saveIM() {
    if (savingIM.value || !imKey.value) return
    const current = generation
    savingIM.value = true
    error.value = ''
    try {
      const result = await api<Row>('/native-im-profile', 'PUT', {
        version: imStatus.value.version || 0,
        app_key: imKey.value
      })
      if (current === generation) {
        imStatus.value = result
        imKey.value = ''
      }
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      savingIM.value = false
    }
  }
</script>

<style scoped>
  .native-info {
    margin: 18px 0;
  }
  input[type='file'] {
    max-width: 100%;
  }
</style>
