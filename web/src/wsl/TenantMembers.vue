<template>
  <section class="tenant-members commerce-panel">
    <header class="commerce-toolbar"
      ><div><h2>成员与群权限</h2><p>按群分配权限，动作由本人确认。</p></div
      ><ElButton v-if="canManage" :icon="Plus" type="primary" @click="edit()">添加成员</ElButton
      ><ElButton v-if="canManage" :icon="Setting" @click="editRole()">角色权限</ElButton
      ><ElButton :icon="Refresh" :loading="loading" aria-label="刷新成员" @click="load"
    /></header>
    <ElFormItem v-if="isAdmin" label="客户工作空间" class="member-tenant-picker"
      ><ElSelect
        v-model="selectedTenant"
        filterable
        clearable
        aria-label="选择成员管理客户"
        placeholder="先明确选择客户"
        ><ElOption
          v-for="tenant in tenantChoices"
          :key="tenant.id"
          :value="tenant.id"
          :label="tenant.name" /></ElSelect
    ></ElFormItem>
    <ElAlert
      v-if="!canManage"
      :title="isAdmin ? '请先明确选择需要管理的客户工作空间' : '成员管理仅向客户负责人开放'"
      type="info"
      :closable="false"
    />
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElTable :data="members" empty-text="暂无成员">
      <ElTableColumn prop="username" label="成员" min-width="120" />
      <ElTableColumn label="角色" min-width="110"
        ><template #default="{ row }">{{ roleName(row.tenant_role) }}</template></ElTableColumn
      >
      <ElTableColumn label="群范围" min-width="100"
        ><template #default="{ row }">{{
          row.tenant_role === 'owner' ? '本客户全部群' : `${row.groups?.length || 0} 个群`
        }}</template></ElTableColumn
      >
      <ElTableColumn label="状态" min-width="80"
        ><template #default="{ row }"
          ><ElTag :type="row.disabled ? 'info' : 'success'">{{
            row.disabled ? '已停用' : '有效'
          }}</ElTag></template
        ></ElTableColumn
      >
      <ElTableColumn v-if="canManage" label="操作" width="90"
        ><template #default="{ row }"
          ><ElButton
            link
            type="primary"
            :disabled="row.tenant_role === 'owner' || row.id === session.user?.id"
            @click="edit(row)"
            >编辑</ElButton
          ></template
        ></ElTableColumn
      >
    </ElTable>
    <ElDialog
      v-model="opened"
      :title="form.id ? '编辑成员' : '添加成员'"
      width="620px"
      class="customer-scope-dialog member-dialog"
      append-to-body
      align-center
      :close-on-click-modal="false"
    >
      <ElForm label-position="top" @submit.prevent="save">
        <div class="member-form-grid">
          <ElFormItem label="用户名"
            ><ElInput
              v-model="form.username"
              :disabled="!!form.id || saving"
              autocomplete="off"
              maxlength="80"
          /></ElFormItem>
          <ElFormItem :label="form.id ? '新密码（留空保留）' : '网站密码'"
            ><ElInput
              v-model="form.password"
              type="password"
              autocomplete="new-password"
              show-password
              :disabled="saving"
              maxlength="128"
          /></ElFormItem>
        </div>
        <ElFormItem label="角色"
          ><ElSelect v-model="form.role" :disabled="saving"
            ><ElOption
              v-for="role in roles.filter((r) => !['owner', 'legacy'].includes(r.code))"
              :key="role.code"
              :label="role.name"
              :value="role.code" /></ElSelect
        ></ElFormItem>
        <ElFormItem label="授权群"
          ><ElSelect
            v-model="form.groups"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            :max-collapse-tags="3"
            :disabled="saving"
            placeholder="明确选择成员可以访问的群"
            ><ElOption
              v-for="group in groups.filter((g) => g.active)"
              :key="group.group_id"
              :value="group.group_id"
              :label="`${platformLabel(group.platform)} · ${group.name} · ${robotLabel(group)}`" /></ElSelect
        ></ElFormItem>
        <div class="member-group-preview"
          ><span v-for="group in selectedGroups" :key="group.group_id"
            >{{ platformLabel(group.platform) }} · {{ group.name }}</span
          ><span v-if="!selectedGroups.length">尚未分配群组</span></div
        >
        <ElFormItem v-if="form.id" label="成员状态"
          ><ElSwitch v-model="form.disabled" :disabled="saving" active-text="停用此成员"
        /></ElFormItem>
        <section class="member-overrides"
          ><h3>个人权限覆盖</h3>
          <div v-for="(override, index) in form.overrides" :key="index" class="override-row">
            <ElSelect
              v-model="override.permission"
              filterable
              aria-label="覆盖权限"
              :disabled="saving"
              ><ElOption
                v-for="spec in permissions"
                :key="spec.code"
                :label="spec.label"
                :value="spec.code"
                :disabled="override.effect === 'allow' && !assignable(spec)"
            /></ElSelect>
            <ElSelect v-model="override.effect" aria-label="覆盖方式" :disabled="saving"
              ><ElOption label="允许" value="allow" /><ElOption label="拒绝" value="deny"
            /></ElSelect>
            <ElDatePicker
              v-model="override.expires_at"
              type="datetime"
              value-format="YYYY-MM-DDTHH:mm:ssZ"
              placeholder="无到期时间"
              :disabled="saving"
            />
            <ElButton
              :icon="Delete"
              text
              aria-label="移除权限覆盖"
              :disabled="saving"
              @click="form.overrides.splice(index, 1)"
            />
          </div>
          <ElButton
            :icon="Plus"
            :disabled="saving"
            @click="form.overrides.push({ permission: '', effect: 'deny', expires_at: null })"
            >添加覆盖</ElButton
          >
        </section>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
      </ElForm>
      <template #footer
        ><ElButton :disabled="saving" @click="opened = false">关闭</ElButton
        ><ElButton
          type="primary"
          :loading="saving"
          :disabled="!canManage || uncertain"
          @click="save"
          >保存成员</ElButton
        ></template
      >
    </ElDialog>
    <ElDialog
      v-model="roleOpened"
      title="角色权限"
      width="660px"
      class="customer-scope-dialog member-role-dialog"
      append-to-body
      align-center
      :close-on-click-modal="false"
    >
      <ElForm label-position="top" @submit.prevent="saveRole">
        <ElFormItem label="角色"
          ><ElSelect v-model="selectedRole" :disabled="saving" @change="selectRole"
            ><ElOption label="新建自定义角色" value="" /><ElOption
              v-for="role in roles.filter((r) => !['owner', 'legacy'].includes(r.code))"
              :key="role.code"
              :value="role.code"
              :label="role.name" /></ElSelect
        ></ElFormItem>
        <div class="member-form-grid"
          ><ElFormItem label="标识"
            ><ElInput
              v-model="roleForm.code"
              :disabled="!!selectedRole || saving"
              maxlength="40" /></ElFormItem
          ><ElFormItem label="名称"
            ><ElInput v-model="roleForm.name" :disabled="saving" maxlength="80" /></ElFormItem
        ></div>
        <ElFormItem v-if="!selectedRole" label="助手权限模板"
          ><ElSelect v-model="templateCode" clearable :disabled="saving" @change="applyTemplate"
            ><ElOption
              v-for="item in templates"
              :key="item.code"
              :value="item.code"
              :label="item.name" /></ElSelect
        ></ElFormItem>
        <ElCheckboxGroup
          v-model="roleForm.permissions"
          class="permission-list"
          :disabled="saving || !canManage"
        >
          <section
            v-for="section in permissionGroups"
            :key="section.name"
            class="permission-section"
            ><h3>{{ section.name }}</h3
            ><ElCheckbox
              v-for="spec in section.items"
              :key="spec.code"
              :value="spec.code"
              :disabled="!assignable(spec) && !roleForm.permissions.includes(spec.code)"
              >{{ spec.label
              }}<small v-if="!assignable(spec)">未在客户上限内开放</small></ElCheckbox
            ></section
          >
        </ElCheckboxGroup>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
      </ElForm>
      <template #footer
        ><ElButton :disabled="saving" @click="roleOpened = false">关闭</ElButton
        ><ElButton
          type="primary"
          :loading="saving"
          :disabled="!canManage || uncertain"
          @click="saveRole"
          >保存角色</ElButton
        ></template
      >
    </ElDialog>
  </section>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElCheckbox,
    ElCheckboxGroup,
    ElDatePicker,
    ElDialog,
    ElForm,
    ElFormItem,
    ElInput,
    ElOption,
    ElSelect,
    ElSwitch,
    ElTable,
    ElTableColumn,
    ElTag
  } from 'element-plus'
  import { Delete, Plus, Refresh, Setting } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'
  import { platformLabel, robotLabel } from './customer'
  const props = defineProps<{ users?: Row[]; initialTenant?: string }>()
  const session = useSession()
  const isAdmin = computed(() => session.user?.role === 'superadmin')
  const selectedTenant = ref(props.initialTenant || '')
  const tenantChoices = computed(() => [
    ...new Map(
      (props.users || [])
        .filter((user) => user.tenant_id && user.role !== 'superadmin' && user.owner_id === user.id)
        .map((user) => [
          user.tenant_id,
          { id: user.tenant_id, name: user.tenant_name || user.username }
        ])
    ).values()
  ])
  const activeTenant = computed(() =>
    isAdmin.value ? selectedTenant.value : session.user?.tenant_id || ''
  )
  const canManage = computed(
    () =>
      !!activeTenant.value &&
      (isAdmin.value || session.user?.tenant_role === 'owner') &&
      session.permissions.includes('members:manage')
  )
  const memberPath = (path: string) =>
    isAdmin.value ? `${path}?tenant_id=${encodeURIComponent(activeTenant.value)}` : path
  const members = ref<Row[]>([]),
    roles = ref<Row[]>([]),
    permissions = ref<Row[]>([]),
    templates = ref<Row[]>([]),
    groups = ref<Row[]>([])
  const capabilityCeiling = ref<string[]>([])
  const error = ref(''),
    saveError = ref(''),
    loading = ref(false),
    saving = ref(false),
    uncertain = ref(false)
  const opened = ref(false),
    roleOpened = ref(false),
    selectedRole = ref(''),
    templateCode = ref('')
  const form = reactive<Row>({ groups: [], overrides: [] }),
    roleForm = reactive<Row>({ code: '', name: '', permissions: [], version: 0 })
  const ownerScope = computed(
    () => `customer:${session.user?.id}:${activeTenant.value}:${session.workspace}:members`
  )
  const memberDraftKey = ref(''),
    roleDraftKey = ref('')
  const selectedGroups = computed(() =>
    groups.value.filter((group) => form.groups?.includes(group.group_id))
  )
  const assignable = (spec: Row) =>
    spec.assignable !== false &&
    capabilityCeiling.value.includes(spec.code) &&
    session.permissions.includes(spec.code)
  const permissionGroups = computed(() => {
    const sections = new Map<string, Row[]>()
    const labels: Record<string, string> = {
      moderation: '群规则与处置',
      replies: '自动回复',
      management: '群配置',
      management_assistant: '群管助手',
      ai: '热群机器人',
      finance: '财务',
      activities: '活动'
    }
    for (const spec of permissions.value) {
      if (['members:manage', 'plans:manage', 'users:manage'].includes(spec.code)) continue
      const name = spec.code.startsWith('group_action:')
        ? '具体群管动作（逐项选择）'
        : labels[spec.module] || '查询与记录'
      if (!sections.has(name)) sections.set(name, [])
      sections.get(name)!.push(spec)
    }
    return [...sections].map(([name, items]) => ({ name, items }))
  })
  let controller: AbortController | undefined,
    generation = 0,
    editor = 0,
    alive = true
  function actorKey() {
    return `${session.user?.id}:${activeTenant.value}`
  }
  async function load() {
    if (!canManage.value) return
    const request = ++generation,
      actor = actorKey()
    controller?.abort()
    const currentController = new AbortController()
    controller = currentController
    loading.value = true
    error.value = ''
    try {
      const [m, c] = await Promise.all([
        api<Row[]>(memberPath('/members'), 'GET', undefined, currentController.signal),
        api<Row>(memberPath('/member-roles'), 'GET', undefined, currentController.signal)
      ])
      if (!alive || request !== generation || actor !== actorKey()) return
      if (c.tenant_id !== activeTenant.value) throw new Error('客户范围已变化，请刷新后重新选择')
      members.value = m
      roles.value = c.roles || []
      permissions.value = c.permissions || []
      templates.value = c.assistant_templates || []
      capabilityCeiling.value = c.capability_ceiling || []
      groups.value = c.groups || []
    } catch (e) {
      if (
        alive &&
        request === generation &&
        actor === actorKey() &&
        !currentController.signal.aborted
      )
        error.value = (e as Error).message
    } finally {
      if (request === generation) loading.value = false
    }
  }
  function roleName(code: string) {
    return (
      { owner: '客户负责人', legacy: '旧权限（待确认）' }[code] ||
      roles.value.find((role) => role.code === code)?.name ||
      code
    )
  }
  function edit(row?: Row) {
    if (!canManage.value || row?.tenant_role === 'owner' || row?.id === session.user?.id) return
    editor++
    uncertain.value = false
    memberDraftKey.value = `${ownerScope.value}:${row?.id || 'new'}`
    const draft = session.drafts[memberDraftKey.value] as Row | undefined
    Object.assign(form, {
      id: row?.id || '',
      username: row?.username || '',
      role: row?.tenant_role || 'viewer',
      disabled: row?.disabled || false,
      version: row?.access_version || 0,
      groups: [...(row?.groups || [])],
      overrides: JSON.parse(JSON.stringify(row?.overrides || [])),
      ...(draft || {}),
      password: ''
    })
    saveError.value = ''
    opened.value = true
  }
  async function save() {
    if (!canManage.value || saving.value || uncertain.value || !opened.value) return
    const request = editor,
      actor = actorKey()
    const { id, ...body } = JSON.parse(JSON.stringify(form))
    saving.value = true
    saveError.value = ''
    try {
      await api(memberPath(id ? `/members/${id}` : '/members'), id ? 'PUT' : 'POST', body)
      if (!alive || request !== editor || actor !== actorKey()) return
      session.saveDraft(memberDraftKey.value, {})
      form.password = ''
      opened.value = false
      await load()
    } catch (e) {
      if (!alive || request !== editor || actor !== actorKey()) return
      saveError.value = (e as Error).message
      uncertain.value = !(e instanceof ApiError)
      if (uncertain.value) saveError.value = '保存结果尚未确认，请关闭后刷新成员列表核对'
    } finally {
      if (request === editor) saving.value = false
    }
  }
  function editRole() {
    if (!canManage.value) return
    editor++
    uncertain.value = false
    selectedRole.value = ''
    selectRole('')
    saveError.value = ''
    roleOpened.value = true
  }
  function selectRole(code: string) {
    templateCode.value = ''
    const role = roles.value.find((row) => row.code === code)
    roleDraftKey.value = `${ownerScope.value}:role:${code || 'new'}`
    const draft = session.drafts[roleDraftKey.value] as Row | undefined
    Object.assign(roleForm, {
      code: role?.code || '',
      name: role?.name || '',
      permissions: [...(role?.permissions || [])],
      version: role?.version || 0,
      ...(draft || {})
    })
  }
  function applyTemplate(code: string) {
    const template = templates.value.find((item) => item.code === code)
    if (!template || selectedRole.value) return
    Object.assign(roleForm, {
      code: template.code,
      name: template.name,
      permissions: template.permissions.filter(
        (permission: string) =>
          !permission.startsWith('group_action:') &&
          capabilityCeiling.value.includes(permission) &&
          session.permissions.includes(permission)
      ),
      version: 0
    })
  }
  async function saveRole() {
    if (!canManage.value || saving.value || uncertain.value || !roleOpened.value) return
    const request = editor,
      actor = actorKey(),
      body = JSON.parse(JSON.stringify(roleForm))
    saving.value = true
    saveError.value = ''
    try {
      await api(memberPath(`/member-roles/${encodeURIComponent(body.code)}`), 'PUT', {
        name: body.name,
        permissions: body.permissions,
        version: body.version
      })
      if (!alive || request !== editor || actor !== actorKey()) return
      session.saveDraft(roleDraftKey.value, {})
      roleOpened.value = false
      await load()
    } catch (e) {
      if (!alive || request !== editor || actor !== actorKey()) return
      saveError.value = (e as Error).message
      uncertain.value = !(e instanceof ApiError)
      if (uncertain.value) saveError.value = '保存结果尚未确认，请关闭后刷新角色列表核对'
    } finally {
      if (request === editor) saving.value = false
    }
  }
  watch(
    form,
    () => {
      if (!opened.value || !memberDraftKey.value) return
      const { password: _password, ...draft } = form
      session.saveDraft(memberDraftKey.value, draft)
    },
    { deep: true }
  )
  watch(
    roleForm,
    () => {
      if (roleOpened.value && roleDraftKey.value) session.saveDraft(roleDraftKey.value, roleForm)
    },
    { deep: true }
  )
  watch(opened, (value) => {
    if (!value) form.password = ''
  })
  watch(
    () => props.initialTenant,
    (value) => {
      if (value) selectedTenant.value = value
    }
  )
  watch(
    () => [session.user?.id, activeTenant.value, session.user?.tenant_role],
    () => {
      controller?.abort()
      generation++
      editor++
      opened.value = roleOpened.value = false
      form.password = ''
      members.value = roles.value = groups.value = []
      saving.value = false
      void load()
    },
    { immediate: true }
  )
  onBeforeUnmount(() => {
    alive = false
    generation++
    editor++
    controller?.abort()
    form.password = ''
  })
</script>
<style scoped>
  .commerce-toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 24px;
  }
  .commerce-toolbar > div {
    margin-right: auto;
  }
  .commerce-toolbar h2 {
    font-size: 18px;
    margin: 0;
  }
  .commerce-toolbar p {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    margin: 6px 0 0;
  }
  .member-form-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 16px;
  }
  .override-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 88px 36px;
    gap: 8px;
    margin-bottom: 16px;
  }
  .override-row :deep(.el-date-editor) {
    width: 100%;
    grid-column: 1 / 3;
    grid-row: 2;
  }
  .override-row > .el-button {
    margin: 0;
    grid-column: 3;
    grid-row: 1;
  }
  .member-overrides {
    border-top: 1px solid var(--el-border-color-light);
    padding-block: 12px 20px;
  }
  .member-overrides h3,
  .permission-section h3 {
    font-size: 14px;
    margin: 16px 0 12px;
  }
  .member-group-preview {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 16px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    padding-bottom: 20px;
  }
  .permission-list {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
  }
  .permission-list :deep(.el-checkbox) {
    height: auto;
    min-height: 36px;
    white-space: normal;
    margin-right: 16px;
  }
  .permission-list :deep(.el-checkbox__label) {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .permission-list small {
    display: block;
    color: var(--el-text-color-secondary);
    font-size: 11px;
  }
  .permission-section {
    display: flex;
    flex-direction: column;
    width: 100%;
    border-bottom: 1px solid var(--el-border-color-light);
    padding-bottom: 12px;
  }
  .commerce-panel {
    min-width: 0;
  }
  .el-select {
    width: 100%;
  }
  .member-tenant-picker {
    max-width: 460px;
    margin-block: 0 24px;
  }
  .el-alert {
    margin-top: 16px;
  }
  @media (max-width: 480px) {
    .member-form-grid {
      grid-template-columns: minmax(0, 1fr);
      gap: 0;
    }
    .commerce-toolbar > div {
      width: 100%;
    }
  }
</style>
