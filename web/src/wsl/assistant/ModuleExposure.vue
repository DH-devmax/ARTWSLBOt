<template>
  <section class="module-exposure" aria-label="助手模块开放">
    <header
      ><h2>助手模块开放</h2
      ><ElRadioGroup v-model="platform" @change="load"
        ><ElRadioButton value="wsl">旺商聊</ElRadioButton
        ><ElRadioButton value="lajiaoliao">辣椒聊</ElRadioButton></ElRadioGroup
      ><ElButton :icon="Edit" :disabled="!base || loading" @click="edit"
        >编辑开放范围</ElButton
      ></header
    >
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElTable :data="rows" max-height="360" empty-text="尚未读取模块目录"
      ><ElTableColumn prop="name" label="业务能力" min-width="130" /><ElTableColumn
        label="类别"
        min-width="110"
        ><template #default="{ row }">{{ labels[row.category] }}</template></ElTableColumn
      ><ElTableColumn label="开放状态" min-width="100"
        ><template #default="{ row }"
          ><ElTag :type="base?.capabilities.includes(row.id) ? 'success' : 'info'">{{
            base?.capabilities.includes(row.id) ? '已开放' : '关闭'
          }}</ElTag></template
        ></ElTableColumn
      ></ElTable
    >
    <ElDialog
      v-model="visible"
      :title="`${platform === 'wsl' ? '旺商聊' : '辣椒聊'} · 助手模块开放`"
      width="min(640px, calc(100vw - 24px))"
      :close-on-click-modal="false"
    >
      <ElAlert
        title="模块开放后仍检查套餐、租户上限、成员及字段权限；动作需本人确认和正式准入。"
        type="info"
        :closable="false"
      />
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <ElCheckboxGroup v-model="draft" class="module-options" :disabled="saving"
        ><ElCheckbox v-for="row in rows" :key="row.id" :value="row.id" :disabled="!row.implemented"
          >{{ row.name }} · {{ labels[row.category] }}</ElCheckbox
        ></ElCheckboxGroup
      >
      <template #footer
        ><ElButton :disabled="saving" @click="visible = false">取消</ElButton
        ><ElButton type="primary" :loading="saving" @click="save">保存开放范围</ElButton></template
      >
    </ElDialog>
  </section>
</template>
<script setup lang="ts">
  import { ref, computed, onMounted, onUnmounted } from 'vue'
  import { Edit } from '@element-plus/icons-vue'
  import { api, errorText, type Row } from '../api'
  import { useSession } from '../store'
  const session = useSession(),
    owner = session.user?.id
  const platform = ref<'wsl' | 'lajiaoliao'>('wsl'),
    base = ref<Row>(),
    draft = ref<string[]>([]),
    visible = ref(false),
    loading = ref(false),
    saving = ref(false),
    error = ref('')
  const rows = computed<Row[]>(() => base.value?.catalog || [])
  const labels: Record<string, string> = {
    query: '查询',
    config_propose: '配置建议',
    action_propose: '动作建议'
  }
  let generation = 0,
    controller = new AbortController(),
    editVersion = 0
  async function load() {
    const current = ++generation
    controller.abort()
    controller = new AbortController()
    visible.value = false
    base.value = undefined
    loading.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        '/assistant/module-exposure',
        'GET',
        undefined,
        controller.signal,
        platform.value
      )
      if (current === generation && owner === session.user?.id) base.value = data
    } catch (e) {
      if (current === generation && !controller.signal.aborted)
        error.value = errorText((e as Error).message)
    } finally {
      if (current === generation) loading.value = false
    }
  }
  function edit() {
    if (!base.value) return
    draft.value = [...base.value.capabilities]
    editVersion = base.value.version
    visible.value = true
  }
  async function save() {
    if (!base.value || saving.value) return
    const current = generation
    saving.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        '/assistant/module-exposure',
        'PUT',
        { version: editVersion, capabilities: draft.value },
        controller.signal,
        platform.value
      )
      if (current !== generation || owner !== session.user?.id) return
      base.value = data
      visible.value = false
    } catch (e) {
      if (current === generation && !controller.signal.aborted)
        error.value = errorText((e as Error).message)
    } finally {
      saving.value = false
    }
  }
  onMounted(load)
  onUnmounted(() => {
    generation++
    controller.abort()
  })
</script>
<style scoped>
  .module-exposure {
    margin: 24px 0;
    min-width: 0;
  }
  .module-exposure header {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .module-exposure h2 {
    font-size: 18px;
    margin: 0;
  }
  .module-options {
    display: flex;
    flex-direction: column;
    max-height: 55dvh;
    overflow: auto;
    padding: 14px 0;
  }
  .module-options :deep(.el-checkbox) {
    margin: 0;
    white-space: normal;
    min-height: 36px;
    height: auto;
  }
  .module-options :deep(.el-checkbox__label) {
    white-space: normal;
  }
</style>
