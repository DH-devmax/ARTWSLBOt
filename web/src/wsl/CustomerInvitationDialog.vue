<template>
  <ElDialog
    :model-value="true"
    title="邀请客户负责人"
    width="500px"
    class="customer-scope-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
    @close="$emit('close')"
  >
    <ElForm v-if="!created" label-position="top" @submit.prevent="create">
      <ElFormItem label="邀请对象"
        ><ElRadioGroup v-model="form.mode"
          ><ElRadioButton value="new">新客户</ElRadioButton
          ><ElRadioButton value="existing">已有负责人账号</ElRadioButton></ElRadioGroup
        ></ElFormItem
      >
      <ElFormItem v-if="form.mode === 'new'" label="客户名称"
        ><ElInput v-model="form.tenant_name" maxlength="100" autocomplete="off"
      /></ElFormItem>
      <ElFormItem v-else label="已有负责人"
        ><ElSelect v-model="form.existing_user_id" filterable placeholder="选择已有负责人"
          ><ElOption
            v-for="user in eligibleUsers"
            :key="user.id"
            :value="user.id"
            :label="`${user.username} · ${user.tenant_name}`" /></ElSelect
      ></ElFormItem>
      <p class="invitation-meta"
        >邀请24小时有效。客户通过链接设置自己的密码；已有网站账号需登录后接受。</p
      >
    </ElForm>
    <template v-else>
      <ElAlert title="邀请已创建" type="success" :closable="false" />
      <dl class="invitation-details"
        ><dt>客户</dt
        ><dd>{{
          form.mode === 'new'
            ? form.tenant_name
            : eligibleUsers.find((user) => user.id === form.existing_user_id)?.tenant_name
        }}</dd
        ><dt>邀请对象</dt
        ><dd>{{
          form.mode === 'new'
            ? '由新负责人设置网站账号'
            : eligibleUsers.find((user) => user.id === form.existing_user_id)?.username
        }}</dd
        ><dt>有效期至</dt><dd>{{ customerDate(created.expires_at) }}</dd></dl
      >
      <ElInput
        v-if="invitationURL"
        :model-value="invitationURL"
        readonly
        aria-label="一次性邀请链接"
        autocomplete="off"
      >
        <template #append
          ><ElButton :icon="CopyDocument" aria-label="复制邀请链接" @click="copy"
        /></template>
      </ElInput>
      <ElAlert
        v-else
        title="此请求已创建邀请；一次性链接仅在首次创建时显示。"
        type="info"
        :closable="false"
      />
      <p class="invitation-meta">关闭后不再展示此链接。套餐仍需单独开通。</p>
    </template>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <template #footer
      ><ElButton :disabled="saving" @click="$emit('close')">关闭</ElButton
      ><ElButton
        v-if="!created"
        type="primary"
        :icon="Link"
        :loading="saving"
        :disabled="
          (form.mode === 'new' ? !form.tenant_name.trim() : !form.existing_user_id) || uncertain
        "
        @click="create"
        >创建邀请</ElButton
      ></template
    >
  </ElDialog>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, reactive, ref } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDialog,
    ElForm,
    ElFormItem,
    ElInput,
    ElMessage,
    ElOption,
    ElRadioButton,
    ElRadioGroup,
    ElSelect
  } from 'element-plus'
  import { CopyDocument, Link } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'
  import { customerDate } from './customer'
  const props = defineProps<{ users?: Row[] }>()
  const emit = defineEmits<{ close: []; created: [] }>()
  const session = useSession(),
    actor = session.user?.id
  const form = reactive({ mode: 'new', tenant_name: '', existing_user_id: '' })
  const eligibleUsers = computed(() =>
    (props.users || []).filter(
      (user) => user.role !== 'superadmin' && !user.disabled && user.owner_id === user.id
    )
  )
  const created = ref<Row>(),
    error = ref(''),
    saving = ref(false),
    uncertain = ref(false)
  let alive = true
  const requestKey = crypto.randomUUID()
  const invitationURL = computed(() => {
    if (!created.value?.token) return ''
    return `${location.origin}${location.pathname}#customer-invitation?token=${encodeURIComponent(created.value.token)}`
  })
  async function create() {
    if (
      saving.value ||
      uncertain.value ||
      session.user?.id !== actor ||
      session.user?.role !== 'superadmin'
    )
      return
    saving.value = true
    error.value = ''
    try {
      const result = await api<Row>(
        '/customer-invitations',
        'POST',
        {
          mode: form.mode,
          tenant_name: form.mode === 'new' ? form.tenant_name.trim() : '',
          ...(form.mode === 'existing' ? { existing_user_id: form.existing_user_id } : {})
        },
        undefined,
        'wsl',
        requestKey
      )
      if (!alive || actor !== session.user?.id) return
      created.value = result
      emit('created')
    } catch (e) {
      if (!alive || actor !== session.user?.id) return
      error.value = (e as Error).message
      uncertain.value = !(e instanceof ApiError)
      if (uncertain.value) error.value = '创建结果尚未确认，请先检查邀请记录，勿重复创建'
    } finally {
      if (alive) saving.value = false
    }
  }
  async function copy() {
    if (!invitationURL.value) return
    try {
      await navigator.clipboard.writeText(invitationURL.value)
      ElMessage.success('邀请链接已复制')
    } catch {
      error.value = '复制失败，请选中链接后复制'
    }
  }
  onBeforeUnmount(() => {
    alive = false
    created.value = undefined
  })
</script>
<style scoped>
  .invitation-meta {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    line-height: 1.7;
  }
  .invitation-details {
    display: grid;
    grid-template-columns: 76px minmax(0, 1fr);
    gap: 12px;
    padding: 12px 0;
  }
  .invitation-details dt {
    color: var(--el-text-color-secondary);
  }
  .invitation-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .el-alert {
    margin-top: 16px;
  }
</style>
