<template>
  <section class="native-social" data-testid="native-social" v-loading="loading">
    <div class="social-toolbar">
      <ElSelect
        v-if="mode === 'groups'"
        v-model="group"
        aria-label="邀请或退群目标"
        placeholder="选择已授权群"
        :disabled="busy"
        filterable
      >
        <ElOption
          v-for="item in availableGroups"
          :key="item.native_id"
          :value="item.native_id"
          :label="item.name"
        />
      </ElSelect>
      <ElButton
        :icon="Refresh"
        :disabled="busy || loading || (mode === 'groups' && !group)"
        @click="load"
        >重新读取</ElButton
      >
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="operation">
      <ElAlert
        :title="outcome"
        :type="operation.status === 'succeeded' ? 'success' : 'warning'"
        :closable="false"
      />
      <div class="social-operation">
        <span>操作编号：{{ operation.id || operation.key }}</span>
        <ElButton v-if="unresolved" :disabled="busy" @click="inspect()">查询原操作</ElButton>
        <ElButton
          v-if="operation.status === 'unknown' && operation.id && socialOperation"
          :disabled="busy"
          @click="reconcile"
          >只读核对结果</ElButton
        >
      </div>
    </template>
    <template v-if="snapshot">
      <div class="social-meta">
        <span>采样 {{ new Date(snapshot.sampled_at).toLocaleString() }}</span>
        <ElTag v-if="!snapshot.native_actions_enabled" type="warning">原生动作已关闭</ElTag>
        <span v-if="mode === 'groups'">{{
          snapshot.in_group ? roleLabel(snapshot.self_role) : '当前账号不在群'
        }}</span>
      </div>
      <ElAlert
        v-if="mode === 'applications'"
        title="同意指定申请请使用“受控验证”的独立授权窗口。"
        type="info"
        :closable="false"
      />
      <ElTable
        v-if="mode === 'applications'"
        class="social-desktop-applications"
        :data="snapshot.applications"
        max-height="320"
        empty-text="当前返回窗口无好友申请"
      >
        <ElTableColumn label="申请人" min-width="140">
          <template #default="{ row }"
            >{{ row.user.name || row.user.id }}<small class="social-id">{{ row.user.id }}</small
            ><small class="social-id">申请编号：{{ row.id }}</small></template
          >
        </ElTableColumn>
        <ElTableColumn label="状态" min-width="110">
          <template #default="{ row }"
            >{{ row.direction === 'FROM_ME' ? '我发起 · ' : ''
            }}{{ stateLabel(row.state) }}</template
          >
        </ElTableColumn>
        <ElTableColumn label="操作" width="132">
          <template #default="{ row }">
            <ElButton
              link
              type="primary"
              disabled
              :aria-label="`同意${row.user.id}好友申请`"
              @click="
                submit(
                  { kind: 'friend_approve', target_id: row.user.id, text: row.id },
                  `同意 ${row.user.name || row.user.id} 的好友申请`
                )
              "
              >同意</ElButton
            >
            <ElButton
              link
              type="danger"
              :disabled="blocked || !pending(row)"
              :aria-label="`拒绝${row.user.id}好友申请`"
              @click="
                submit(
                  { kind: 'friend_reject', target_id: row.user.id, text: row.id },
                  `拒绝 ${row.user.name || row.user.id} 的好友申请`
                )
              "
              >拒绝</ElButton
            >
          </template>
        </ElTableColumn>
      </ElTable>
      <div v-if="mode === 'applications'" class="social-mobile-applications">
        <ElEmpty
          v-if="!snapshot.applications.length"
          description="当前返回窗口无好友申请"
          :image-size="42"
        />
        <div v-for="row in snapshot.applications" :key="row.id" class="social-application">
          <div>
            <strong>{{ row.user.name || row.user.id }}</strong>
            <small class="social-id"
              >{{ row.user.id }} · {{ row.direction === 'FROM_ME' ? '我发起 · ' : ''
              }}{{ stateLabel(row.state) }}</small
            >
            <small class="social-id">申请编号：{{ row.id }}</small>
          </div>
          <div class="social-application-actions">
            <ElButton
              link
              type="primary"
              disabled
              :aria-label="`同意${row.user.id}好友申请`"
              @click="
                submit(
                  { kind: 'friend_approve', target_id: row.user.id, text: row.id },
                  `同意 ${row.user.name || row.user.id} 的好友申请`
                )
              "
              >同意</ElButton
            >
            <ElButton
              link
              type="danger"
              :disabled="blocked || !pending(row)"
              :aria-label="`拒绝${row.user.id}好友申请`"
              @click="
                submit(
                  { kind: 'friend_reject', target_id: row.user.id, text: row.id },
                  `拒绝 ${row.user.name || row.user.id} 的好友申请`
                )
              "
              >拒绝</ElButton
            >
          </div>
        </div>
      </div>
      <template v-else>
        <div class="social-invite">
          <ElSelect
            v-model="friend"
            aria-label="邀请的好友"
            placeholder="选择未在群的好友"
            :disabled="blocked"
            filterable
          >
            <ElOption
              v-for="item in eligibleFriends"
              :key="item.id"
              :value="item.id"
              :label="`${item.name || item.id} · ${item.id}`"
            />
          </ElSelect>
          <ElButton
            type="primary"
            :disabled="blocked || !friend || !['owner', 'admin'].includes(snapshot.self_role)"
            @click="
              submit(
                { kind: 'group_invite', group_id: group, target_id: friend },
                '邀请所选好友进入当前群'
              )
            "
            >邀请入群</ElButton
          >
        </div>
        <div class="social-leave">
          <span>退群前须关闭该群的群管及热群服务；群主不可直接退群。</span>
          <ElButton
            type="danger"
            plain
            :disabled="
              blocked || !snapshot.in_group || !['member', 'admin'].includes(snapshot.self_role)
            "
            @click="
              submit(
                { kind: 'group_leave', group_id: group },
                '让当前机器人退出此群，保留授权和历史数据'
              )
            "
            >退出群聊</ElButton
          >
        </div>
      </template>
    </template>
    <ElEmpty v-else-if="!loading && !error" description="请选择已授权群后读取" :image-size="48" />
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { Refresh } from '@element-plus/icons-vue'
  import { ElMessageBox } from 'element-plus'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'

  const props = defineProps<{
    account: string
    groups: Row[]
    mode: 'applications' | 'groups'
    scope: string
  }>()
  const session = useSession()
  const snapshot = ref<Row | null>(null),
    operation = ref<Row | null>(null)
  const group = ref(''),
    friend = ref(''),
    error = ref(''),
    loading = ref(false),
    busy = ref(false)
  let generation = 0,
    controller: AbortController | undefined
  const workspace = computed(() => session.workspace)
  const root = computed(() => `/accounts/${props.account}`)
  const availableGroups = computed(() =>
    props.groups.filter(
      (item) =>
        item.platform !== 'lajiaoliao' &&
        item.active &&
        (item.primary_account_id === props.account || item.ai_account_id === props.account)
    )
  )
  const eligibleFriends = computed(() =>
    (snapshot.value?.friends || []).filter(
      (item: Row) =>
        item.id !== snapshot.value?.self_id && !snapshot.value?.member_ids.includes(item.id)
    )
  )
  const unresolved = computed(
    () => operation.value && ['preparing', 'submitting', 'unknown'].includes(operation.value.status)
  )
  const socialOperation = computed(() =>
    ['friend_approve', 'friend_reject', 'group_invite', 'group_leave'].includes(
      operation.value?.kind
    )
  )
  const blocked = computed(
    () => busy.value || loading.value || unresolved.value || !snapshot.value?.native_actions_enabled
  )
  const roleLabel = (role: string) =>
    ({ owner: '群主', admin: '管理员', member: '成员' })[role] || '角色未核验'
  const stateLabel = (state: string) =>
    ({
      FRIENDLOG_STATE_AGREE: '已同意',
      FRIENDLOG_STATE_REFUSE: '已拒绝',
      FRIENDLOG_STATE_PROCESSING: '待处理',
      FRIENDLOG_STATE_DELETE: '已删除',
      FRIENDLOG_STATE_EXPIRE: '已过期'
    })[state] || '未知'
  const pending = (row: Row) =>
    row.direction === 'FROM_HIM' && row.state === 'FRIENDLOG_STATE_PROCESSING'
  const outcome = computed(() => {
    if (operation.value?.status === 'succeeded')
      return operation.value.result_code === 'native_invitation_requested'
        ? '邀请请求已获上游确认，对方是否入群需重新读取核对'
        : operation.value.result_code === 'desired_state_observed'
          ? '已观察到目标状态，未重发原操作'
          : '上游已确认操作，真实能力验收仍待完成'
    if (operation.value?.status === 'failed') return '操作失败，未自动重试'
    return '操作处理中或结果未知，已阻止再次提交，请查询并核对'
  })
  async function load() {
    const version = ++generation
    controller?.abort()
    controller = new AbortController()
    snapshot.value = null
    error.value = ''
    loading.value = true
    try {
      const pending = await api<Row>(
        `${root.value}/native-setting-operations`,
        'GET',
        undefined,
        controller.signal,
        workspace.value
      )
      if (version !== generation) return
      if (pending.pending?.length) operation.value = pending.pending[0]
      if (props.mode === 'groups' && !group.value) return
      const result = await api<Row>(
        `${root.value}/native-social${group.value ? `?group_id=${encodeURIComponent(group.value)}` : ''}`,
        'GET',
        undefined,
        controller.signal,
        workspace.value
      )
      if (version !== generation) return
      const view = result.snapshot
      if (
        result.native_ready !== false ||
        !view ||
        view.group_id !== group.value ||
        !Array.isArray(view.applications) ||
        !Array.isArray(view.friends) ||
        !Array.isArray(view.member_ids) ||
        !/^[a-f0-9]{64}$/.test(view.digest) ||
        !Number.isFinite(Date.parse(view.sampled_at))
      )
        throw new Error('原生好友与群操作响应不兼容')
      snapshot.value = view
      if (!eligibleFriends.value.some((item: Row) => item.id === friend.value)) friend.value = ''
    } catch (cause) {
      if (version === generation && !controller.signal.aborted)
        error.value = (cause as Error).message
    } finally {
      if (version === generation) loading.value = false
    }
  }
  async function submit(change: Row, description: string) {
    if (blocked.value || !snapshot.value) return
    const version = generation
    try {
      await ElMessageBox.confirm(description, '确认原生操作', {
        confirmButtonText: '确认提交',
        cancelButtonText: '取消',
        type: 'warning'
      })
    } catch {
      return
    }
    if (version !== generation || blocked.value || !snapshot.value) return
    const key = crypto.randomUUID()
    const body = {
      change,
      digest: snapshot.value.digest,
      account_epoch: snapshot.value.account_epoch,
      account_version: snapshot.value.account_version
    }
    busy.value = true
    operation.value = { key, kind: change.kind, status: 'unknown' }
    error.value = ''
    try {
      const result = await api<Row>(
        `${root.value}/native-social-operations`,
        'POST',
        body,
        controller?.signal,
        workspace.value,
        key
      )
      if (version !== generation) return
      operation.value = { ...result.operation, key }
    } catch (cause) {
      if (version === generation) {
        error.value = (cause as Error).message
        if (
          cause instanceof ApiError &&
          cause.status >= 400 &&
          cause.status < 500 &&
          !['native_setting_pending', 'idempotency_conflict'].includes(cause.code)
        )
          operation.value = { key, kind: change.kind, status: 'failed' }
      }
    } finally {
      if (version === generation) busy.value = false
    }
  }
  async function inspect(recheck = false) {
    if (busy.value || !operation.value) return
    const version = generation,
      current = { ...operation.value }
    busy.value = true
    error.value = ''
    try {
      const path = recheck
        ? `${root.value}/native-social-operations/${current.id}/reconcile`
        : `${root.value}/native-setting-operations${current.id ? `/${current.id}` : `?key=${encodeURIComponent(current.key)}`}`
      const result = await api<Row>(
        path,
        recheck ? 'POST' : 'GET',
        recheck ? {} : undefined,
        controller?.signal,
        workspace.value
      )
      if (version === generation) operation.value = { ...result.operation, key: current.key }
    } catch (cause) {
      if (version === generation) error.value = (cause as Error).message
    } finally {
      if (version === generation) busy.value = false
    }
  }
  const reconcile = () => inspect(true)
  watch(
    () => `${props.scope}:${props.mode}:${props.account}:${workspace.value}:${session.user?.id}`,
    () => {
      generation++
      controller?.abort()
      group.value = ''
      friend.value = ''
      operation.value = null
      busy.value = false
      void load()
    },
    { immediate: true }
  )
  watch(group, () => {
    friend.value = ''
    void load()
  })
  onBeforeUnmount(() => {
    generation++
    controller?.abort()
  })
</script>

<style scoped>
  .native-social {
    min-width: 0;
    margin-top: 18px;
  }
  .social-toolbar,
  .social-meta,
  .social-invite,
  .social-leave,
  .social-operation {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin: 12px 0;
  }
  .social-toolbar .el-select,
  .social-invite .el-select {
    width: 260px;
    max-width: 100%;
  }
  .social-meta,
  .social-leave,
  .social-operation {
    font-size: 12px;
    color: #657080;
  }
  .social-operation span {
    overflow-wrap: anywhere;
  }
  .social-id {
    display: block;
    color: #657080;
  }
  .social-mobile-applications {
    display: none;
  }
  .social-application {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 14px 0;
    border-bottom: 1px solid #e9edf1;
  }
  .social-application > div:first-child {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .social-application strong {
    font-size: 13px;
  }
  .social-application-actions {
    flex-shrink: 0;
    display: flex;
    gap: 8px;
  }
  .social-application-actions .el-button + .el-button {
    margin-left: 0;
  }
  .social-leave {
    border-top: 1px solid #e9edf1;
    padding-top: 14px;
    justify-content: space-between;
  }
  .el-alert {
    margin: 10px 0;
  }
  @media (max-width: 600px) {
    .social-desktop-applications {
      display: none;
    }
    .social-mobile-applications {
      display: block;
    }
  }
  @media (max-width: 480px) {
    .social-toolbar .el-select,
    .social-invite .el-select {
      width: 100%;
    }
  }
</style>
