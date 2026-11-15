<template>
  <ElDialog
    :model-value="!!group"
    :title="`${group?.name || '原生群组'} · 成员`"
    class="platform-config-dialog native-members-dialog"
    width="900px"
    align-center
    @close="emit('close')"
  >
    <div v-loading="loading" class="native-members-body">
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <ElAlert v-if="roleResult" :title="roleResult" type="info" :closable="false" />
      <template v-if="snapshot">
        <div class="native-members-status">
          <ElTag type="info">只读核对</ElTag>
          <span>读取时间 {{ new Date(snapshot.sampled_at).toLocaleString() }}</span>
          <span>账号角色：{{ roleLabel(snapshot.self_role) }}</span>
          <span>{{ snapshot.members.length }} 人 · {{ snapshot.pages }} 页</span>
        </div>
        <ElAlert
          v-if="!snapshot.mapping_complete"
          title="部分成员缺少传输身份，不能用于发送或群管操作"
          type="warning"
          :closable="false"
        />
        <ElInput
          v-model="search"
          :prefix-icon="Search"
          placeholder="搜索昵称、名片或成员 ID"
          aria-label="搜索原生成员"
          clearable
        />
        <ElTable :data="pageRows" empty-text="无匹配成员" max-height="380">
          <ElTableColumn prop="name" label="昵称" min-width="130" show-overflow-tooltip />
          <ElTableColumn prop="card" label="群名片" min-width="130" show-overflow-tooltip />
          <ElTableColumn prop="id" label="成员 ID" min-width="150" />
          <ElTableColumn prop="transport" label="传输身份" min-width="150" show-overflow-tooltip />
          <ElTableColumn label="角色" width="95">
            <template #default="{ row }">{{ roleLabel(row.role) }}</template>
          </ElTableColumn>
          <ElTableColumn label="操作" width="130" fixed="right">
            <template #default="{ row }">
              <ElButton
                link
                type="primary"
                :icon="Edit"
                :aria-label="`修改${row.id === snapshot.self_id ? '机器人本人' : row.name || row.id}群昵称`"
                @click="editing = row"
              >
                {{ row.id === snapshot.self_id ? '修改我的群昵称' : '修改群昵称' }}
              </ElButton>
            </template>
          </ElTableColumn>
        </ElTable>
        <ElPagination
          v-if="filtered.length > 50"
          v-model:current-page="page"
          :page-size="50"
          :total="filtered.length"
          layout="prev, pager, next"
          small
        />
      </template>
      <ElEmpty v-else-if="!loading && !error" description="暂无成员快照" :image-size="50" />
    </div>
    <template #footer>
      <ElButton
        v-if="['wsl', 'lajiaoliao'].includes(workspace)"
        :icon="CircleCheck"
        :loading="verifying"
        :disabled="loading || !snapshot"
        @click="verifyRole"
        >核验当前身份</ElButton
      >
      <ElButton :icon="Refresh" :disabled="loading" @click="load">重新读取</ElButton>
      <ElButton @click="emit('close')">关闭</ElButton>
    </template>
  </ElDialog>
  <NativeSettings
    v-if="editing && group"
    :open="true"
    :account="{ id: account, name: '当前机器人' }"
    :workspace="workspace"
    :scope="`${scope}:${group.id}:${editing.id}`"
    :group="group"
    :target="editing.id"
    @close="editing = null"
    @saved="memberSaved"
  />
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import { CircleCheck, Edit, Refresh, Search } from '@element-plus/icons-vue'
  import { ElMessageBox } from 'element-plus'
  import { api, type Row } from './api'
  import NativeSettings from './NativeSettings.vue'

  const props = defineProps<{
    account: string
    workspace: import('./workspaces').Workspace
    group: Row | null
    scope: string
  }>()
  const emit = defineEmits<{ close: [] }>()
  const snapshot = ref<Row | null>(null)
  const editing = ref<Row | null>(null)
  function memberSaved() {
    editing.value = null
    void load()
  }
  const verifying = ref(false),
    roleResult = ref('')
  const loading = ref(false),
    error = ref(''),
    search = ref(''),
    page = ref(1)
  let generation = 0
  let controller: AbortController | undefined
  let roleController: AbortController | undefined
  const roleLabel = (role: string) =>
    ({ owner: '群主', admin: '管理员', member: '成员', unknown: '未核验' })[role] || '未核验'
  const filtered = computed(() =>
    (snapshot.value?.members || []).filter((item: Row) =>
      `${item.name} ${item.card} ${item.id}`.toLowerCase().includes(search.value.toLowerCase())
    )
  )
  const pageRows = computed(() => filtered.value.slice((page.value - 1) * 50, page.value * 50))
  watch(search, () => {
    page.value = 1
  })
  async function verifyRole() {
    if (!props.group || verifying.value || loading.value || !snapshot.value) return
    const version = generation
    const group = props.group.id
    try {
      await ElMessageBox.confirm(
        '将读取当前连接和完整原生成员信息。角色变化会使旧排队操作失效；不会认领群组、启用功能或发送消息。',
        '核验群身份',
        { confirmButtonText: '核验', cancelButtonText: '取消', type: 'warning' }
      )
    } catch {
      return
    }
    if (version !== generation) return
    verifying.value = true
    roleResult.value = ''
    error.value = ''
    roleController = new AbortController()
    try {
      const result = await api<Row>(
        `/accounts/${props.account}/native-role`,
        'POST',
        { group_id: group },
        roleController.signal,
        props.workspace
      )
      if (version !== generation) return
      const observation = result.observation
      if (
        result.grants_ownership !== false ||
        result.native_ready !== false ||
        !observation ||
        observation.account_id !== props.account ||
        observation.native_group_id !== group ||
        !['member', 'admin', 'owner'].includes(observation.role) ||
        !Number.isFinite(Date.parse(observation.observed_at)) ||
        typeof observation.individual_muted !== 'boolean'
      ) {
        throw new Error('群身份核验结果格式不兼容')
      }
      roleResult.value = `已核验：${roleLabel(observation.role)} · ${
        observation.individual_muted ? '账号处于单独禁言状态' : '未发现单独禁言'
      }。不代表全部发言条件或原生功能已验收。`
    } catch (cause) {
      if (version === generation && !roleController?.signal.aborted)
        error.value = (cause as Error).message
    } finally {
      if (version === generation) verifying.value = false
    }
  }
  async function load() {
    const version = ++generation
    controller?.abort()
    roleController?.abort()
    verifying.value = false
    roleResult.value = ''
    snapshot.value = null
    error.value = ''
    if (!props.group || !props.account) {
      loading.value = false
      return
    }
    controller = new AbortController()
    loading.value = true
    const group = props.group.id
    try {
      const result = await api<Row>(
        `/accounts/${props.account}/native-members`,
        'POST',
        { group_id: group },
        controller.signal,
        props.workspace
      )
      if (version !== generation) return
      const value = result.snapshot
      if (
        !result.read_only ||
        result.role_authorizes_actions !== false ||
        value?.group_id !== group ||
        !value.complete ||
        !Array.isArray(value.members) ||
        value.members.length > 10000 ||
        !Number.isInteger(value.pages) ||
        value.pages < 1 ||
        value.pages > 100 ||
        typeof value.mapping_complete !== 'boolean' ||
        !['owner', 'admin', 'member', 'unknown'].includes(value.self_role) ||
        value.members.some((item: unknown) => {
          const member = item as Row | null
          return (
            !member ||
            typeof member.id !== 'string' ||
            !(props.workspace === 'lajiaoliao' || props.group?.platform === 'lajiaoliao'
              ? /^[^\s\0]{1,128}$/.test(member.id)
              : /^[1-9][0-9]{0,19}$/.test(member.id)) ||
            typeof member.name !== 'string' ||
            typeof member.card !== 'string' ||
            typeof member.transport !== 'string' ||
            member.transport.length > 1024 ||
            !['owner', 'admin', 'member', 'unknown'].includes(member.role)
          )
        }) ||
        !Number.isFinite(Date.parse(value.sampled_at))
      )
        throw new Error('成员快照格式不兼容')
      snapshot.value = value
    } catch (cause) {
      if (version === generation && !controller?.signal.aborted)
        error.value = (cause as Error).message
    } finally {
      if (version === generation) loading.value = false
    }
  }
  watch(
    () => `${props.scope}:${props.group?.id || ''}`,
    () => {
      search.value = ''
      editing.value = null
      page.value = 1
      void load()
    },
    { immediate: true }
  )
  onUnmounted(() => {
    generation++
    controller?.abort()
    roleController?.abort()
    snapshot.value = null
  })
</script>

<style scoped>
  .native-members-body {
    min-height: 130px;
    min-width: 0;
  }
  .native-members-status {
    display: flex;
    flex-wrap: wrap;
    gap: 10px 16px;
    align-items: center;
    margin-bottom: 16px;
    color: #687386;
    font-size: 13px;
  }
  .native-members-body > .el-input,
  .native-members-body > .el-alert {
    margin-bottom: 16px;
  }
  .native-members-body > .el-pagination {
    margin-top: 16px;
  }
</style>
