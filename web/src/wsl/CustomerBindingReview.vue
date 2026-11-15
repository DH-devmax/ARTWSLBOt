<template>
  <ElDialog
    :model-value="true"
    :title="challenge?.transfer_required ? '跨租户转移审核' : '群归属审核'"
    width="620px"
    class="customer-scope-dialog customer-binding-review"
    append-to-body
    align-center
    :close-on-click-modal="false"
    @close="$emit('close')"
  >
    <div class="binding-review-heading">
      <span>认证消息证明</span>
      <ElButton
        text
        :icon="Refresh"
        :loading="loading"
        :disabled="saving"
        aria-label="刷新审核记录"
        @click="load"
      />
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElAlert
      v-if="conflict"
      title="审核记录已变化，请刷新后核对；转移确认需要重新勾选。"
      type="warning"
      :closable="false"
    />
    <ElAlert
      v-if="uncertain"
      title="审核结果尚未确认，请刷新原记录核对，不要重复提交。"
      type="warning"
      :closable="false"
    />
    <ElSkeleton v-if="loading && !challenge" :rows="6" animated />
    <template v-if="challenge">
      <dl class="binding-review-details">
        <dt>群组</dt><dd>{{ challenge.group?.name }}</dd> <dt>群标识</dt
        ><dd>{{ challenge.group?.native_id }}</dd> <dt>平台</dt
        ><dd>{{ platformLabel(platform) }}</dd> <dt>机器人</dt><dd>{{ robotLabel(challenge) }}</dd>
        <dt>原生账号</dt><dd>{{ challenge.robot.native_id }}</dd>
        <dt>{{ challenge.transfer_required ? '转入租户' : '申请租户' }}</dt>
        <dd
          >{{ targetName }}<small>{{ targetID }}</small></dd
        >
        <template v-if="challenge.transfer_required">
          <dt>转出租户</dt>
          <dd
            >{{ challenge.transfer.source_tenant_name
            }}<small>{{ challenge.transfer.source_tenant_id }}</small></dd
          >
        </template>
        <dt>原生角色</dt
        ><dd>{{
          challenge.proof_role === 'owner'
            ? '群主'
            : challenge.proof_role === 'admin'
              ? '管理员'
              : '未核验'
        }}</dd>
        <dt>审核状态</dt><dd>{{ stateLabel }}</dd>
      </dl>
      <ElAlert
        v-if="finished"
        :title="challenge.state === 'bound' ? '审核已通过，群业务运行保持关闭' : '申请已拒绝'"
        :type="challenge.state === 'bound' ? 'success' : 'info'"
        :closable="false"
      />
      <template v-else-if="challenge.state === 'review'">
        <ElForm label-position="top" @submit.prevent="submit">
          <ElFormItem label="审核决定">
            <ElRadioGroup v-model="decision" :disabled="saving || loading || conflict || uncertain">
              <ElRadio value="approve">批准</ElRadio>
              <ElRadio value="reject">拒绝</ElRadio>
            </ElRadioGroup>
          </ElFormItem>
          <ElCheckbox
            v-if="challenge.transfer_required && decision === 'approve'"
            v-model="transferConfirmed"
            class="binding-transfer-consent"
            :disabled="saving || loading || conflict || uncertain"
            >我确认将此群从「{{ challenge.transfer.source_tenant_name }}」转移至「{{
              targetName
            }}」</ElCheckbox
          >
          <p v-if="challenge.transfer_required" class="binding-review-note"
            >转移会暂停旧群业务；转入后需由新负责人重新确认启用。</p
          >
        </ElForm>
      </template>
      <ElAlert v-else title="此记录当前不处于待审核状态" type="info" :closable="false" />
    </template>
    <template #footer>
      <ElButton :disabled="saving" @click="$emit('close')">关闭</ElButton>
      <ElButton
        v-if="challenge?.state === 'bound'"
        type="primary"
        :icon="Setting"
        :disabled="loading || saving"
        @click="openRuntime"
        >运行与权限</ElButton
      >
      <ElButton
        v-if="challenge?.state === 'review'"
        :type="decision === 'approve' ? 'primary' : 'danger'"
        :icon="Check"
        :loading="saving"
        :disabled="!canSubmit"
        @click="submit"
        >{{ submitLabel }}</ElButton
      >
    </template>
  </ElDialog>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElCheckbox,
    ElDialog,
    ElForm,
    ElFormItem,
    ElRadio,
    ElRadioGroup,
    ElSkeleton
  } from 'element-plus'
  import { Check, Refresh, Setting } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { platformLabel, robotLabel, type CustomerPlatform } from './customer'
  import { useSession } from './store'
  const props = defineProps<{ claim: Row; tenants: Row[] }>()
  const emit = defineEmits<{ close: []; reviewed: []; manage: [group: Row] }>()
  const session = useSession()
  const actor = session.user?.id,
    workspace = session.workspace
  const claim = { ...props.claim },
    id = String(claim.onboarding_challenge_id || '')
  const platform = claim.platform as CustomerPlatform,
    targetID = String(claim.tenant_id || '')
  const tenantName = props.tenants.find((row) => row.tenant_id === targetID)?.tenant_name
  const targetName = computed(
    () => challenge.value?.target_tenant_name || claim.tenant_name || tenantName || targetID
  )
  const challenge = ref<Row>(),
    loading = ref(false),
    saving = ref(false),
    error = ref('')
  const conflict = ref(false),
    uncertain = ref(false),
    transferConfirmed = ref(false)
  const decision = ref('approve')
  const finished = computed(() => ['bound', 'rejected'].includes(challenge.value?.state))
  const stateLabel = computed(
    () =>
      (
        ({
          review: '待平台审核',
          bound: '已批准',
          rejected: '已拒绝',
          expired: '已过期',
          cancelled: '已取消'
        }) as Record<string, string>
      )[challenge.value?.state] || '等待核验'
  )
  const submitLabel = computed(() =>
    decision.value === 'reject'
      ? '拒绝申请'
      : challenge.value?.transfer_required
        ? '确认转移绑定'
        : '批准绑定'
  )
  const canSubmit = computed(
    () =>
      current() &&
      challenge.value?.state === 'review' &&
      !loading.value &&
      !saving.value &&
      !conflict.value &&
      !uncertain.value &&
      (!challenge.value.transfer_required || decision.value === 'reject' || transferConfirmed.value)
  )
  let alive = true,
    sequence = 0,
    key = crypto.randomUUID()
  const current = () =>
    alive &&
    session.user?.id === actor &&
    session.user?.role === 'superadmin' &&
    session.workspace === workspace
  function validate(value: Row) {
    if (
      value.id !== id ||
      value.platform !== platform ||
      value.group?.id !== claim.group_id ||
      value.robot?.id !== claim.account_id ||
      !Number.isInteger(value.version) ||
      value.version < 1 ||
      (value.target_tenant_id && value.target_tenant_id !== targetID)
    )
      throw new Error('审核范围已变化，请关闭后重新核对申请')
    if (
      value.transfer_required &&
      (!value.transfer?.source_tenant_id ||
        !value.transfer?.source_tenant_name ||
        !Number.isInteger(value.transfer.binding_version) ||
        value.transfer.binding_version < 1 ||
        value.transfer.source_tenant_id === targetID)
    )
      throw new Error('转出租户或绑定版本尚未核实，不能提交审核')
  }
  async function load() {
    if (!current() || saving.value) return
    const request = ++sequence
    loading.value = true
    error.value = ''
    transferConfirmed.value = false
    try {
      if (!id || !targetID || !['wsl', 'lajiaoliao'].includes(platform))
        throw new Error('此申请缺少完整接入证明，请刷新申请列表')
      const value = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}`,
        'GET',
        undefined,
        undefined,
        platform
      )
      if (!current() || request !== sequence) return
      validate(value)
      challenge.value = value
      conflict.value = false
      key = crypto.randomUUID()
      if (finished.value) uncertain.value = false
    } catch (e) {
      if (current() && request === sequence) {
        error.value = (e as Error).message
        challenge.value = undefined
      }
    } finally {
      if (current() && request === sequence) loading.value = false
    }
  }
  async function submit() {
    if (!canSubmit.value || !challenge.value) return
    const value = challenge.value,
      request = ++sequence
    const transfer = value.transfer_required === true
    const body = {
      approve: decision.value === 'approve',
      version: value.version,
      ...(transfer
        ? {
            source_tenant_id: value.transfer.source_tenant_id,
            binding_version: value.transfer.binding_version
          }
        : {})
    }
    saving.value = true
    error.value = ''
    try {
      const result = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/${transfer ? 'transfer-review' : 'review'}`,
        'POST',
        body,
        undefined,
        platform,
        transfer ? key : undefined
      )
      if (!current() || request !== sequence) return
      validate(result)
      challenge.value = result
      transferConfirmed.value = false
      emit('reviewed')
    } catch (e) {
      if (!current() || request !== sequence) return
      error.value = (e as Error).message
      conflict.value = e instanceof ApiError && e.status === 409
      uncertain.value = !(e instanceof ApiError)
      transferConfirmed.value = false
    } finally {
      if (current() && request === sequence) saving.value = false
    }
  }
  function openRuntime() {
    if (!current() || challenge.value?.state !== 'bound' || loading.value || saving.value) return
    const value = challenge.value
    emit('manage', {
      group_id: value.group.id,
      name: value.group.name,
      native_id: value.group.native_id,
      platform,
      tenant_id: targetID,
      primary_account_id: value.robot.id,
      robot_nickname: robotLabel(value),
      active: true
    })
  }
  watch(decision, () => {
    transferConfirmed.value = false
  })
  void load()
  onBeforeUnmount(() => {
    alive = false
    sequence++
  })
</script>
<style scoped>
  .binding-review-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    color: var(--el-text-color-secondary);
    margin-bottom: 12px;
  }
  .binding-review-details {
    display: grid;
    grid-template-columns: 80px minmax(0, 1fr);
    gap: 14px;
    margin: 20px 0 24px;
  }
  .binding-review-details dt {
    color: var(--el-text-color-secondary);
  }
  .binding-review-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .binding-review-details small {
    display: block;
    margin-top: 4px;
    color: var(--el-text-color-secondary);
  }
  .binding-transfer-consent {
    height: auto;
    align-items: flex-start;
    white-space: normal;
    margin-bottom: 12px;
  }
  .binding-transfer-consent :deep(.el-checkbox__input) {
    margin-top: 3px;
  }
  .binding-transfer-consent :deep(.el-checkbox__label) {
    white-space: normal;
    line-height: 22px;
    overflow-wrap: anywhere;
  }
  .binding-review-note {
    color: var(--el-text-color-secondary);
    line-height: 1.6;
  }
  .el-alert {
    margin-bottom: 12px;
  }
</style>
