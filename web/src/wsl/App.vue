<template>
  <div v-if="!session.ready" class="boot-state"
    ><ElIcon class="is-loading"><Loading /></ElIcon
  ></div>
  <main v-else-if="!session.user" class="login-shell">
    <section class="login-form">
      <img class="brand-logo" src="/wsl-logo.png" alt="" />
      <h1>旺商聊机器人</h1>
      <p class="muted">管理平台登录</p>
      <ElForm label-position="top" @submit.prevent="login">
        <ElFormItem label="用户名"
          ><ElInput v-model="credentials.username" autocomplete="username"
        /></ElFormItem>
        <ElFormItem label="密码"
          ><ElInput
            v-model="credentials.password"
            type="password"
            show-password
            autocomplete="current-password"
        /></ElFormItem>
        <ElAlert v-if="loginError" :title="loginError" type="error" :closable="false" />
        <ElButton native-type="submit" type="primary" :loading="busy" class="login-submit"
          >登录</ElButton
        >
      </ElForm>
    </section>
    <footer>旺商聊 SaaS · Art Design Pro</footer>
  </main>
  <div v-else class="platform-shell">
    <aside class="sidebar" :class="{ open: sidebarOpen }">
      <div class="brand">
        <img class="brand-logo" src="/wsl-logo.png" alt="" />
        <span class="brand-name">{{ isMaiBot ? '热群机器人' : '旺商聊机器人' }}</span>
        <ElButton
          v-if="can('workspaces:maibot')"
          class="workspace-switch"
          text
          :icon="Switch"
          aria-label="切换工作台"
          :title="isMaiBot ? '切换到旺商聊机器人' : '切换到热群机器人'"
          @click="switchWorkbench(isMaiBot ? 'wsl' : 'maibot')"
        />
      </div>
      <div class="workspace-label">{{ isAdmin ? '平台管理' : '工作空间' }}</div>
      <nav aria-label="主导航">
        <button
          v-for="item in visibleNavigation"
          :key="item.key"
          :class="{ active: page === item.key }"
          @click="navigate(item.key)"
          ><ElIcon><component :is="item.icon" /></ElIcon><span>{{ item.label }}</span>
          <span v-if="item.key === 'claims' && overview.pending_claims" class="nav-count">{{
            overview.pending_claims
          }}</span>
        </button>
      </nav>
      <div class="sidebar-footer"
        ><span class="status-dot"></span>控制面已连接<span>v0.1</span></div
      >
    </aside>
    <div v-if="sidebarOpen" class="sidebar-scrim" @click="sidebarOpen = false"></div>
    <section class="main-area">
      <header class="topbar">
        <ElButton class="mobile-menu" text aria-label="展开导航" @click="sidebarOpen = !sidebarOpen"
          ><ElIcon><Menu /></ElIcon
        ></ElButton>
        <div class="breadcrumb"
          >{{ isMaiBot ? '聊天引擎' : '群组管理' }}<span>/</span
          ><strong>{{ pageTitle }}</strong></div
        >
        <div class="topbar-actions">
          <ElSelect
            v-if="accountScopedPages.includes(page)"
            :model-value="session.selectedAccount || '__all__'"
            placeholder="选择机器人面板"
            aria-label="机器人面板"
            class="account-select"
            @change="session.selectAccount($event === '__all__' ? '' : $event)"
          >
            <ElOption label="工作台总览" value="__all__" />
            <ElOption v-for="a in data.accounts" :key="a.id" :value="a.id" :label="a.name">
              <span>{{ a.name }}</span
              ><span class="account-option-status">{{ connectionLabel(a) }}</span>
            </ElOption>
          </ElSelect>
          <ElPopover v-if="isAdmin" placement="bottom-end" :width="360" trigger="click">
            <template #reference>
              <ElButton text aria-label="机器人连接状态" :icon="Connection" />
            </template>
            <div class="connection-popover">
              <div v-for="account in data.accounts" :key="account.id" class="connection-row">
                <button class="account-name-button" @click="session.selectAccount(account.id)">{{
                  account.name
                }}</button>
                <ElTag :type="account.runtime_state === 'connected' ? 'success' : 'info'">{{
                  connectionLabel(account)
                }}</ElTag>
                <ElButton link @click="openNativeLogin(account)">登录</ElButton>
                <ElButton
                  v-if="account.auth_state === 'authenticated'"
                  link
                  type="danger"
                  @click="forgetNative(account)"
                  >清除会话</ElButton
                >
                <ElTooltip content="删除机器人">
                  <ElButton
                    text
                    type="danger"
                    :icon="Delete"
                    :aria-label="`删除机器人 ${account.name}`"
                    @click="openAccountRemoval(account)"
                  />
                </ElTooltip>
              </div>
              <ElEmpty v-if="!data.accounts.length" description="暂无机器人" :image-size="48" />
              <NativeDeployment @updated="refresh" />
            </div>
          </ElPopover>
          <ElButton
            v-if="isAdmin"
            type="primary"
            :icon="Key"
            class="native-login-entry"
            @click="openNativeLogin(null)"
            >注册 / 登录</ElButton
          >
          <ElTooltip content="刷新"
            ><ElButton text :loading="refreshing" aria-label="刷新" @click="refresh"
              ><ElIcon><Refresh /></ElIcon></ElButton
          ></ElTooltip>
          <ElDropdown @command="logout">
            <button class="user-menu"
              ><span class="avatar">{{ session.user.username.slice(0, 1).toUpperCase() }}</span
              ><span>{{ session.user.username }}</span
              ><ElIcon><ArrowDown /></ElIcon
            ></button>
            <template #dropdown
              ><ElDropdownMenu
                ><ElDropdownItem command="logout">退出登录</ElDropdownItem></ElDropdownMenu
              ></template
            >
          </ElDropdown>
        </div>
      </header>
      <div class="content">
        <div class="page-heading"
          ><div
            ><h1>{{ pageTitle }}</h1
            ><p
              >{{ isAdmin ? '平台管理' : '租户空间' }}<span class="separator">/</span
              >{{ selectedAccountName }}</p
            ></div
          >
          <div class="readiness"
            ><span class="status-dot warning"></span>原生接入未验收<span class="divider"></span>AI
            隔离未验收</div
          >
        </div>
        <ElAlert
          v-if="loadError"
          :title="loadError"
          type="error"
          :closable="false"
          class="load-error"
        />
        <template v-if="page === 'overview'">
          <div class="metrics">
            <div v-for="metric in metrics" :key="metric.label" class="metric"
              ><span>{{ metric.label }}</span
              ><strong
                >{{ metric.value ?? '—'
                }}<small v-if="metric.limit">/ {{ metric.limit }}</small></strong
              ><div class="metric-rule" :style="{ '--metric-color': metric.color }"></div
            ></div>
          </div>
          <div class="overview-grid">
            <section class="data-section"
              ><div class="section-title"
                ><h2>{{ isMaiBot ? '聊天机器人状态' : '群管机器人状态' }}</h2
                ><ElTag type="info" effect="plain">独立连接</ElTag></div
              >
              <ElTable :data="data.accounts" empty-text="暂无机器人">
                <ElTableColumn label="机器人" min-width="100"
                  ><template #default="{ row }">
                    <button class="account-name-button" @click="session.selectAccount(row.id)">{{
                      row.name
                    }}</button>
                  </template></ElTableColumn
                ><ElTableColumn label="连接"
                  ><template #default="{ row }"
                    ><ElTag :type="row.runtime_state === 'connected' ? 'success' : 'info'">{{
                      connectionLabel(row)
                    }}</ElTag></template
                  ></ElTableColumn
                >
                <ElTableColumn label="登录" min-width="110"
                  ><template #default="{ row }">
                    <ElButton v-if="isAdmin" link type="primary" @click="openNativeLogin(row)">{{
                      row.auth_state === 'authenticated' ? '已登录 · 重新登录' : '注册 / 登录'
                    }}</ElButton>
                    <span v-else>{{
                      row.auth_state === 'authenticated' ? '已登录' : '未登录'
                    }}</span>
                  </template></ElTableColumn
                >
                <ElTableColumn v-if="isAdmin" label="操作" width="70">
                  <template #default="{ row }">
                    <ElTooltip content="删除机器人">
                      <ElButton
                        text
                        type="danger"
                        :icon="Delete"
                        :aria-label="`删除机器人 ${row.name}`"
                        @click="openAccountRemoval(row)"
                      />
                    </ElTooltip>
                  </template>
                </ElTableColumn>
              </ElTable>
            </section>
            <section class="data-section"
              ><div class="section-title"
                ><h2>{{ isMaiBot ? '引擎状态' : '最近操作' }}</h2
                ><ElButton
                  v-if="!isMaiBot && can('operations:read')"
                  link
                  type="primary"
                  @click="navigate('operations')"
                  >查看记录<ElIcon><ArrowRight /></ElIcon></ElButton
              ></div>
              <ElDescriptions v-if="isMaiBot" :column="1" border class="engine-status">
                <ElDescriptionsItem label="引擎连接"
                  ><ElTag type="warning">尚未接入</ElTag></ElDescriptionsItem
                >
                <ElDescriptionsItem label="执行权限">聊天发言</ElDescriptionsItem>
                <ElDescriptionsItem label="群管与财务"
                  ><ElTag type="info">不可访问</ElTag></ElDescriptionsItem
                >
                <ElDescriptionsItem label="已分配群">{{ overview.groups || 0 }}</ElDescriptionsItem>
                <ElDescriptionsItem label="待启用">{{
                  overview.ai_requests || 0
                }}</ElDescriptionsItem>
              </ElDescriptions>
              <ElTable v-else :data="scopedOperations.slice(0, 6)" empty-text="暂无操作记录">
                <ElTableColumn prop="kind" label="动作" /><ElTableColumn label="状态"
                  ><template #default="{ row }"
                    ><ElTag :type="statusType(row.status)">{{
                      statusLabel(row.status)
                    }}</ElTag></template
                  ></ElTableColumn
                >
                <ElTableColumn label="时间" min-width="145"
                  ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
                >
              </ElTable>
            </section>
          </div>
          <section class="data-section capacity-section"
            ><div class="section-title"
              ><h2>验收目标</h2><ElTag type="warning" effect="plain">待验证</ElTag></div
            >
            <div class="capacity-grid"
              ><div
                ><strong>100<span>条/秒</span></strong
                ><p>聚合消息持续接入</p></div
              ><div
                ><strong>25<span>ms</span></strong
                ><p>可靠落盘 P95</p></div
              ><div
                ><strong>1<span>个引擎</span></strong
                ><p>最多 100 个独立聊天群，容量待验收</p></div
              ><div
                ><strong>8<span>核</span> / 32<span>GB</span></strong
                ><p>生产资源预算</p></div
              ></div
            >
          </section>
        </template>
        <SystemMonitor v-else-if="page === 'system-monitor'" :key="session.workspace" />
        <MaiBotConsole
          v-else-if="isMaiBot && page.startsWith('ai-')"
          :account="session.selectedAccount"
          :section="page.slice(3)"
          :title="pageTitle"
          @updated="refresh"
        />
        <section v-else class="data-section">
          <div class="table-toolbar"
            ><div class="toolbar-left"
              ><ElInput
                v-model="search"
                placeholder="搜索"
                clearable
                :prefix-icon="Search"
                class="search-input"
              /><span class="row-count">{{ filteredRows.length }} 条</span></div
            >
            <div class="table-toolbar-actions">
              <ElButton v-if="createLabel" type="primary" :icon="Plus" @click="openCreate">{{
                createLabel
              }}</ElButton>
            </div>
          </div>
          <ElTable
            :data="filteredRows"
            v-loading="refreshing"
            empty-text="暂无记录"
            :row-key="(row: Row) => row.id || row.group_id || row.account_id"
          >
            <template v-if="page === 'groups' && isMaiBot">
              <ElTableColumn prop="name" label="聊天群组" min-width="180" />
              <ElTableColumn prop="account_name" label="聊天机器人" min-width="160" />
              <ElTableColumn label="状态" min-width="160"
                ><template #default="{ row }">{{ aiStateLabel(row) }}</template></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'groups'">
              <ElTableColumn prop="name" label="群组" min-width="170" /><ElTableColumn
                prop="account_name"
                label="管理账号"
                min-width="140"
              />
              <ElTableColumn label="管理" width="100"
                ><template #default="{ row }"
                  ><ElTag :type="row.management_enabled ? 'success' : 'info'">{{
                    row.management_enabled ? '已启用' : '已停用'
                  }}</ElTag></template
                ></ElTableColumn
              >
              <ElTableColumn label="AI" width="100"
                ><template #default="{ row }"
                  ><ElTag :type="row.ai_enabled ? 'success' : 'info'">{{
                    aiStateLabel(row)
                  }}</ElTag></template
                ></ElTableColumn
              >
              <ElTableColumn label="授权" width="100"
                ><template #default="{ row }"
                  ><ElTag :type="row.active ? 'success' : 'danger'">{{
                    row.active ? '有效' : '已撤销'
                  }}</ElTag></template
                ></ElTableColumn
              >
              <ElTableColumn prop="version" label="版本" width="90" /><ElTableColumn
                label="操作"
                min-width="280"
                ><template #default="{ row }"
                  ><ElButton
                    v-if="!isMaiBot && can('groups:configure')"
                    link
                    type="primary"
                    @click="editGroup(row)"
                    >群配置</ElButton
                  ><ElButton
                    v-if="!isMaiBot && can('groups:ai')"
                    link
                    type="primary"
                    @click="openGroupAI(row)"
                    >AI 入群 / 启停</ElButton
                  ><ElButton
                    v-if="!isMaiBot && row.active && can('groups:revoke')"
                    link
                    type="danger"
                    @click="revokeGroup(row)"
                    >撤销授权</ElButton
                  ></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'claims'">
              <ElTableColumn prop="group_name" label="群组" min-width="150" /><ElTableColumn
                prop="native_identity"
                label="申请人原生身份"
                min-width="160"
              />
              <ElTableColumn label="状态" width="100"
                ><template #default="{ row }"
                  ><ElTag :type="statusType(row.state)">{{
                    statusLabel(row.state)
                  }}</ElTag></template
                ></ElTableColumn
              >
              <ElTableColumn label="申请时间" min-width="160"
                ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
              >
              <ElTableColumn label="操作" width="100" v-if="isAdmin"
                ><template #default="{ row }"
                  ><ElButton
                    v-if="row.state === 'pending'"
                    link
                    type="primary"
                    @click="openReview(row)"
                    >审核</ElButton
                  ></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'users'">
              <ElTableColumn prop="username" label="用户名" min-width="130" /><ElTableColumn
                prop="tenant_name"
                label="租户"
                min-width="150"
              />
              <ElTableColumn label="角色" width="100"
                ><template #default="{ row }">{{
                  row.role === 'superadmin' ? '超管' : '用户'
                }}</template></ElTableColumn
              >
              <ElTableColumn label="套餐到期" min-width="155"
                ><template #default="{ row }">{{ date(row.expires_at) }}</template></ElTableColumn
              >
              <ElTableColumn label="状态" width="90"
                ><template #default="{ row }"
                  ><ElTag :type="row.disabled ? 'danger' : 'success'">{{
                    row.disabled ? '已禁用' : '正常'
                  }}</ElTag></template
                ></ElTableColumn
              >
              <ElTableColumn label="操作" min-width="160"
                ><template #default="{ row }"
                  ><template v-if="row.role !== 'superadmin'"
                    ><ElButton link type="primary" @click="openSubscription(row)"
                      >开通 / 续期</ElButton
                    ><ElButton link type="primary" @click="openUserAccess(row)">权限</ElButton
                    ><ElButton
                      link
                      :type="row.disabled ? 'success' : 'danger'"
                      @click="toggleUser(row)"
                      >{{ row.disabled ? '启用' : '禁用' }}</ElButton
                    ></template
                  ></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'plans'">
              <ElTableColumn prop="name" label="套餐" min-width="150" /><ElTableColumn
                prop="group_limit"
                label="群名额"
                width="100"
              /><ElTableColumn prop="ai_group_limit" label="AI 群名额" width="110" /><ElTableColumn
                prop="monthly_ai_tokens"
                label="月度 AI Token"
                min-width="140"
              />
              <ElTableColumn label="模块" min-width="230"
                ><template #default="{ row }"
                  ><div class="tag-list"
                    ><ElTag v-for="m in row.modules" :key="m" size="small" type="info">{{
                      moduleLabels[m] || m
                    }}</ElTag></div
                  ></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'personas'">
              <ElTableColumn prop="account_name" label="机器人账号" min-width="150" /><ElTableColumn
                prop="nickname"
                label="AI 名称"
                min-width="120"
              />
              <ElTableColumn
                prop="personality"
                label="人设"
                min-width="250"
                show-overflow-tooltip
              /><ElTableColumn prop="version" label="版本" width="80" />
              <ElTableColumn v-if="isAdmin" label="操作" width="80"
                ><template #default="{ row }"
                  ><ElButton link type="primary" @click="openPersona(row)">编辑</ElButton></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'operations'">
              <ElTableColumn prop="kind" label="动作" min-width="120" /><ElTableColumn
                prop="target_id"
                label="目标"
                min-width="150"
              /><ElTableColumn label="状态" width="110"
                ><template #default="{ row }"
                  ><ElTag :type="statusType(row.status)">{{
                    statusLabel(row.status)
                  }}</ElTag></template
                ></ElTableColumn
              >
              <ElTableColumn prop="result_code" label="结果" min-width="180" /><ElTableColumn
                label="时间"
                min-width="160"
                ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
              >
              <ElTableColumn label="操作" width="80"
                ><template #default="{ row }"
                  ><ElButton
                    v-if="row.status === 'queued' && can('operations:cancel')"
                    link
                    type="danger"
                    @click="cancelOperation(row)"
                    >取消</ElButton
                  ></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'tests'">
              <ElTableColumn
                prop="group_id"
                label="隔离测试群"
                min-width="180"
                show-overflow-tooltip
              /><ElTableColumn label="动作" min-width="160"
                ><template #default="{ row }">{{ row.actions.join('、') }}</template></ElTableColumn
              >
              <ElTableColumn label="预算" width="100"
                ><template #default="{ row }"
                  >{{ row.used }} / {{ row.budget }}</template
                ></ElTableColumn
              >
              <ElTableColumn label="到期时间" min-width="160"
                ><template #default="{ row }">{{ date(row.expires_at) }}</template></ElTableColumn
              >
              <ElTableColumn label="操作" width="90"
                ><template #default="{ row }"
                  ><ElButton v-if="!row.cancelled" link type="danger" @click="cancelTest(row)"
                    >取消</ElButton
                  ><span v-else>已取消</span></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'ledger'">
              <ElTableColumn prop="member_id" label="成员" min-width="150" /><ElTableColumn
                prop="kind"
                label="类型"
                width="120"
              /><ElTableColumn label="金额" width="130" align="right"
                ><template #default="{ row }">{{
                  money(row.amount_cents)
                }}</template></ElTableColumn
              >
              <ElTableColumn
                prop="external_key"
                label="业务流水号"
                min-width="230"
                show-overflow-tooltip
              /><ElTableColumn label="时间" min-width="160"
                ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'activities'">
              <ElTableColumn prop="name" label="活动" min-width="180" /><ElTableColumn
                prop="participant_count"
                label="报名人数"
                width="100"
              /><ElTableColumn prop="winners" label="中奖名额" width="100" />
              <ElTableColumn label="状态" width="100"
                ><template #default="{ row }">{{ statusLabel(row.state) }}</template></ElTableColumn
              >
              <ElTableColumn label="截止时间" min-width="160"
                ><template #default="{ row }">{{ date(row.closes_at) }}</template></ElTableColumn
              >
              <ElTableColumn label="操作" width="90"
                ><template #default="{ row }"
                  ><ElButton
                    v-if="row.state === 'open' && can('activities:draw')"
                    link
                    type="primary"
                    @click="draw(row)"
                    >开奖</ElButton
                  ></template
                ></ElTableColumn
              >
            </template>
            <template v-else>
              <ElTableColumn prop="action" label="事件" min-width="200" /><ElTableColumn
                prop="resource_id"
                label="资源"
                min-width="220"
                show-overflow-tooltip
              />
              <ElTableColumn label="时间" min-width="160"
                ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
              >
            </template>
          </ElTable>
        </section>
        <footer class="page-footer"
          ><span>旺商聊管理平台</span
          ><span>{{ lastRefreshed ? `更新于 ${date(lastRefreshed)}` : '尚未同步' }}</span></footer
        >
      </div>
    </section>
    <ElDialog
      v-model="dialog.open"
      class="platform-config-dialog"
      :title="dialog.title"
      width="560px"
      align-center
      append-to-body
      :close-on-click-modal="false"
      :close-on-press-escape="!saving"
      :show-close="!saving"
      @close="persistDraft"
    >
      <ElForm label-position="top" :disabled="saving" @submit.prevent>
        <template v-if="dialog.kind === 'account'"
          ><ElFormItem label="账号名称"><ElInput v-model="form.name" maxlength="100" /></ElFormItem
        ></template>
        <template v-else-if="dialog.kind === 'user'">
          <ElFormItem label="租户名称"><ElInput v-model="form.tenant_name" /></ElFormItem>
          <ElFormItem label="用户名"
            ><ElInput v-model="form.username" autocomplete="off"
          /></ElFormItem>
          <ElFormItem label="初始密码（至少 12 字节）"
            ><ElInput
              v-model="form.password"
              type="password"
              show-password
              autocomplete="new-password"
          /></ElFormItem>
        </template>
        <template v-else-if="dialog.kind === 'plan'">
          <ElFormItem label="套餐名称"><ElInput v-model="form.name" /></ElFormItem>
          <div class="form-row"
            ><ElFormItem label="群名额"
              ><ElInputNumber v-model="form.group_limit" :min="0" :max="100" /></ElFormItem
            ><ElFormItem label="AI 群名额"
              ><ElInputNumber v-model="form.ai_group_limit" :min="0" :max="100" /></ElFormItem
          ></div>
          <ElFormItem label="月度 AI Token"
            ><ElInputNumber
              v-model="form.monthly_ai_tokens"
              :min="0"
              :max="1000000000"
              :step="10000"
          /></ElFormItem>
          <ElFormItem label="功能模块"
            ><ElCheckboxGroup v-model="form.modules"
              ><ElCheckbox v-for="(label, value) in moduleLabels" :key="value" :value="value">{{
                label
              }}</ElCheckbox></ElCheckboxGroup
            ></ElFormItem
          >
        </template>
        <template v-else-if="dialog.kind === 'subscription'">
          <ElFormItem label="套餐"
            ><ElSelect v-model="form.plan_id"
              ><ElOption
                v-for="p in data.plans"
                :key="p.id"
                :label="p.name"
                :value="p.id" /></ElSelect
          ></ElFormItem>
          <ElFormItem label="到期时间"
            ><ElDatePicker
              v-model="form.expires_at"
              type="datetime"
              value-format="YYYY-MM-DDTHH:mm:ssZ" /></ElFormItem
          ><ElFormItem label="启用"><ElSwitch v-model="form.enabled" /></ElFormItem>
        </template>
        <template v-else-if="dialog.kind === 'claim'">
          <ElFormItem label="群内授权码"
            ><ElInput v-model="form.code" autocomplete="off" /></ElFormItem
          ><ElFormItem label="申请人旺商聊 UID"
            ><ElInput v-model="form.native_identity"
          /></ElFormItem>
        </template>
        <template v-else-if="dialog.kind === 'review'">
          <ElFormItem label="审核结果"
            ><ElRadioGroup v-model="form.approve"
              ><ElRadio :value="true">批准</ElRadio
              ><ElRadio :value="false">拒绝</ElRadio></ElRadioGroup
            ></ElFormItem
          >
          <ElFormItem
            ><ElCheckbox v-model="form.identity_verified"
              >已在原生平台核验申请人的身份及群管理资格</ElCheckbox
            ></ElFormItem
          >
          <ElFormItem label="审核依据"
            ><ElInput v-model="form.note" type="textarea" :rows="4" maxlength="2000"
          /></ElFormItem>
        </template>
        <template v-else-if="dialog.kind === 'persona'">
          <ElFormItem label="机器人账号"
            ><ElSelect
              v-model="form.account_id"
              :disabled="Boolean(dialog.id)"
              @change="selectPersonaAccount"
              ><ElOption
                v-for="a in data.accounts"
                :key="a.id"
                :value="a.id"
                :label="a.name" /></ElSelect
          ></ElFormItem>
          <ElFormItem label="AI 名称"
            ><ElInput v-model="form.nickname" maxlength="100"
          /></ElFormItem>
          <ElFormItem label="人设"
            ><ElInput v-model="form.personality" type="textarea" :rows="6" maxlength="12000"
          /></ElFormItem>
          <ElFormItem label="回复风格"
            ><ElInput v-model="form.reply_style" type="textarea" :rows="3" maxlength="4000"
          /></ElFormItem>
        </template>
        <template v-else-if="dialog.kind === 'group'">
          <div class="form-row"
            ><ElFormItem label="管理与自动化"
              ><ElSwitch v-model="form.management_enabled" /></ElFormItem
            ><ElFormItem label="群 AI"
              ><ElTag>{{ form.ai_enabled ? '已启用' : '未启用' }}</ElTag></ElFormItem
            ></div
          >
          <ElFormItem label="拦截词（一行一个）"
            ><ElInput
              v-model="form.blocked_words"
              type="textarea"
              :rows="5"
              :disabled="!can('groups:moderation')"
          /></ElFormItem>
          <ElFormItem label="黑名单 UID（一行一个）"
            ><ElInput
              v-model="form.blacklist"
              type="textarea"
              :rows="3"
              :disabled="!can('groups:moderation')"
          /></ElFormItem>
          <ElFormItem label="豁免 UID（一行一个）"
            ><ElInput
              v-model="form.exemptions"
              type="textarea"
              :rows="3"
              :disabled="!can('groups:moderation')"
          /></ElFormItem>
          <ElFormItem label="AI 冷却时间 / 秒"
            ><ElInputNumber
              v-model="form.ai_cooldown_seconds"
              :min="0"
              :max="86400"
              :disabled="!can('groups:ai')"
          /></ElFormItem>
          <ElTag type="info">配置版本 {{ form.version }}</ElTag>
        </template>
        <template v-else-if="dialog.kind === 'group-ai'">
          <ElFormItem label="申请启用群 AI"><ElSwitch v-model="form.enabled" /></ElFormItem>
          <ElFormItem label="申请聊天机器人入群"
            ><ElSwitch v-model="form.invite" :disabled="!form.enabled"
          /></ElFormItem>
          <ElAlert
            title="当前仅保存申请；原生邀请与 AI 隔离未验收，不会执行拉群或发送消息。"
            type="warning"
            :closable="false"
          />
        </template>
        <template v-else-if="dialog.kind === 'access'">
          <ElFormItem label="权限角色">
            <ElSelect v-model="form.role_code" @change="selectAccessRole">
              <ElOption
                v-for="role in accessCatalog.roles || []"
                :key="role.code"
                :label="role.label"
                :value="role.code"
              />
              <ElOption label="自定义" value="custom" />
            </ElSelect>
          </ElFormItem>
          <ElFormItem label="用户权限">
            <ElCheckboxGroup v-model="form.permissions" class="permission-grid">
              <ElCheckbox
                v-for="permission in accessCatalog.permissions || []"
                :key="permission.code"
                :value="permission.code"
                @change="form.role_code = 'custom'"
                >{{ permission.label }}</ElCheckbox
              >
            </ElCheckboxGroup>
          </ElFormItem>
        </template>
        <template
          v-else-if="
            dialog.kind === 'ledger' || dialog.kind === 'activity' || dialog.kind === 'test'
          "
        >
          <ElFormItem label="群组"
            ><ElSelect v-model="form.group_id"
              ><ElOption
                v-for="g in data.groups"
                :key="g.group_id"
                :value="g.group_id"
                :label="g.name" /></ElSelect
          ></ElFormItem>
          <template v-if="dialog.kind === 'ledger'">
            <ElFormItem label="成员 UID"><ElInput v-model="form.member_id" /></ElFormItem
            ><ElFormItem label="类型"
              ><ElSelect v-model="form.kind"
                ><ElOption label="奖励" value="reward" /><ElOption
                  label="调整"
                  value="adjustment" /><ElOption label="支付确认" value="paid" /></ElSelect
            ></ElFormItem>
            <ElFormItem label="金额 / 元"
              ><ElInput v-model="form.amount" inputmode="decimal" /></ElFormItem
            ><ElFormItem label="业务流水号"><ElInput v-model="form.external_key" /></ElFormItem
            ><ElFormItem label="依据"><ElInput v-model="form.note" type="textarea" /></ElFormItem>
          </template>
          <template v-else-if="dialog.kind === 'activity'">
            <ElFormItem label="活动名称"><ElInput v-model="form.name" /></ElFormItem
            ><ElFormItem label="截止时间"
              ><ElDatePicker
                v-model="form.closes_at"
                type="datetime"
                value-format="YYYY-MM-DDTHH:mm:ssZ" /></ElFormItem
            ><ElFormItem label="中奖名额"
              ><ElInputNumber v-model="form.winners" :min="1" :max="100"
            /></ElFormItem>
          </template>
          <template v-else>
            <ElFormItem label="测试账号"
              ><ElSelect v-model="form.account_ids" multiple
                ><ElOption
                  v-for="a in data.accounts"
                  :key="a.id"
                  :label="a.name"
                  :value="a.id" /></ElSelect
            ></ElFormItem>
            <ElFormItem label="允许动作"
              ><ElCheckboxGroup v-model="form.actions"
                ><ElCheckbox value="send_text">发文本</ElCheckbox
                ><ElCheckbox value="recall">撤回</ElCheckbox
                ><ElCheckbox value="mute">禁言</ElCheckbox
                ><ElCheckbox value="unmute">解除禁言</ElCheckbox></ElCheckboxGroup
              ></ElFormItem
            >
            <ElFormItem label="测试目标 UID（一行一个）"
              ><ElInput v-model="form.targets" type="textarea"
            /></ElFormItem>
            <div class="form-row"
              ><ElFormItem label="有效期 / 秒"
                ><ElInputNumber v-model="form.seconds" :min="1" :max="300" /></ElFormItem
              ><ElFormItem label="动作次数上限"
                ><ElInputNumber v-model="form.budget" :min="1" :max="10" /></ElFormItem
            ></div>
          </template>
        </template>
      </ElForm>
      <ElAlert
        v-if="saveError"
        :title="saveError"
        type="error"
        :closable="false"
        class="save-error"
      />
      <template #footer
        ><ElButton
          v-if="saveErrorCode === 'version_conflict'"
          :loading="saving"
          @click="reloadVersion"
          >核对最新版本</ElButton
        ><ElButton :disabled="saving" @click="dialog.open = false">取消</ElButton
        ><ElButton type="primary" :loading="saving" @click="save">保存</ElButton></template
      >
    </ElDialog>
    <NativeLogin
      v-if="isAdmin"
      ref="nativeLogin"
      v-model="nativeLoginOpen"
      :account="nativeTarget"
      :owner="session.user?.id || ''"
      :workspace="session.workspace"
      @updated="refresh"
      @created="accountCreated"
    />
    <AccountRemoval
      v-if="isAdmin"
      v-model="removalOpen"
      :account="removalTarget"
      :owner="session.user?.id || ''"
      :workspace="session.workspace"
      @removed="accountRemoved"
    />
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
  import { ElMessage, ElMessageBox } from 'element-plus'
  import {
    Connection,
    Loading,
    Menu,
    Refresh,
    Search,
    Plus,
    ArrowDown,
    ArrowRight,
    Monitor,
    User,
    Tickets,
    ChatDotRound,
    Setting,
    Document,
    Bell,
    Coin,
    Present,
    Checked,
    Key,
    Switch,
    Delete
  } from '@element-plus/icons-vue'
  import { api as request, ApiError, type Row } from './api'
  import { useSession } from './store'
  import NativeDeployment from './NativeDeployment.vue'
  import NativeLogin from './NativeLogin.vue'
  import MaiBotConsole from './MaiBotConsole.vue'
  import SystemMonitor from './SystemMonitor.vue'
  import AccountRemoval from './AccountRemoval.vue'

  const session = useSession()
  const api = <T,>(path: string, method = 'GET', body?: unknown, signal?: AbortSignal) =>
    request<T>(path, method, body, signal, session.workspace)
  const isMaiBot = computed(() => session.workspace === 'maibot')
  const can = (permission: string) => session.permissions.includes(permission)
  const accessCatalog = ref<Row>({})
  const nativeTarget = ref<Row | null>(null),
    nativeLoginOpen = ref(false)
  const nativeLogin = ref<InstanceType<typeof NativeLogin>>()
  const removalTarget = ref<Row | null>(null),
    removalOpen = ref(false)
  const credentials = reactive({ username: '', password: '' })
  const busy = ref(false),
    loginError = ref(''),
    refreshing = ref(false),
    loadError = ref(''),
    saveError = ref(''),
    saving = ref(false)
  const sidebarOpen = ref(false),
    page = ref('overview'),
    search = ref(''),
    lastRefreshed = ref('')
  const overview = ref<Row>({})
  const data = reactive<Record<string, Row[]>>({
    accounts: [],
    groups: [],
    claims: [],
    users: [],
    plans: [],
    personas: [],
    operations: [],
    tests: [],
    ledger: [],
    activities: [],
    audit: []
  })
  const dialog = reactive({ open: false, kind: '', title: '', id: '', scope: '', account: '' })
  const saveErrorCode = ref('')
  const form = ref<Row>({})
  const isAdmin = computed(() => session.user?.role === 'superadmin')
  const accountScopedPages = [
    'overview',
    'accounts',
    'groups',
    'claims',
    'personas',
    'operations',
    'tests',
    'ledger',
    'activities',
    'ai-chat',
    'ai-models',
    'ai-learning',
    'ai-memory',
    'ai-people',
    'ai-emoji',
    'ai-statistics',
    'ai-plugins',
    'ai-logs',
    'ai-backups',
    'ai-system'
  ]
  const navigationPermissions: Record<string, string> = {
    overview: 'overview:read',
    accounts: 'accounts:read',
    groups: 'groups:read',
    claims: 'claims:read',
    users: 'users:manage',
    plans: 'plans:manage',
    personas: 'personas:read',
    operations: 'operations:read',
    tests: 'tests:manage',
    ledger: 'ledger:read',
    activities: 'activities:read',
    audit: 'audit:read',
    'system-monitor': 'workspaces:maibot'
  }
  const navigation = [
    { key: 'overview', label: '工作台', icon: Monitor },
    { key: 'groups', label: '群组管理', icon: ChatDotRound },
    { key: 'claims', label: '群授权申请', icon: Checked },
    { key: 'users', label: '用户管理', icon: User, admin: true },
    { key: 'plans', label: 'SaaS 套餐', icon: Tickets, admin: true },
    { key: 'personas', label: 'AI 人设', icon: Setting },
    { key: 'operations', label: '操作记录', icon: Document },
    { key: 'tests', label: '受控测试', icon: Bell, admin: true },
    { key: 'ledger', label: '财务台账', icon: Coin },
    { key: 'activities', label: '活动抽奖', icon: Present },
    { key: 'audit', label: '审计日志', icon: Document },
    { key: 'system-monitor', label: '系统监控', icon: Monitor, admin: true },
    { key: 'ai-chat', label: '聊天配置', icon: ChatDotRound },
    { key: 'ai-models', label: '模型服务', icon: Setting },
    { key: 'ai-learning', label: '表达与黑话', icon: Document },
    { key: 'ai-memory', label: '长期记忆', icon: Tickets },
    { key: 'ai-people', label: '人物与印象', icon: User },
    { key: 'ai-emoji', label: '表情与媒体', icon: Present },
    { key: 'ai-statistics', label: '运行统计', icon: Monitor },
    { key: 'ai-plugins', label: '插件与工具', icon: Setting },
    { key: 'ai-logs', label: '引擎操作记录', icon: Document },
    { key: 'ai-backups', label: '备份与迁移', icon: Tickets },
    { key: 'ai-system', label: '引擎与升级', icon: Setting }
  ]
  const moduleLabels: Record<string, string> = {
    moderation: '群管规则',
    replies: '自动回复',
    schedules: '定时任务',
    rankings: '排行',
    cards: '名片',
    friends: '好友审批',
    activities: '活动',
    finance: '财务',
    ai: '群 AI'
  }
  const visibleNavigation = computed(() =>
    navigation
      .filter(
        (n) =>
          can(n.key.startsWith('ai-') ? 'workspaces:maibot' : navigationPermissions[n.key]) &&
          (isMaiBot.value
            ? ['overview', 'personas', 'groups', 'system-monitor'].includes(n.key) ||
              n.key.startsWith('ai-')
            : n.key !== 'personas' && !n.key.startsWith('ai-'))
      )
      .map((n) => (isMaiBot.value && n.key === 'groups' ? { ...n, label: '聊天群组' } : n))
  )
  const pageTitle = computed(() =>
    isMaiBot.value && page.value === 'groups'
      ? '聊天群组'
      : navigation.find((n) => n.key === page.value)?.label || '工作台'
  )
  const selectedAccountName = computed(() =>
    !accountScopedPages.includes(page.value)
      ? '平台设置'
      : data.accounts.find((a) => a.id === session.selectedAccount)?.name || '全部账号'
  )
  const scopedOperations = computed(() =>
    data.operations.filter(
      (o) => !session.selectedAccount || o.account_id === session.selectedAccount
    )
  )
  const scopedGroups = computed(() =>
    data.groups.filter(
      (group) =>
        !session.selectedAccount ||
        [group.primary_account_id, group.ai_account_id].includes(session.selectedAccount)
    )
  )
  const metrics = computed(() => [
    {
      label: isMaiBot.value ? '聊天机器人' : '群管机器人',
      value: session.selectedAccount ? 1 : overview.value.accounts,
      limit: overview.value.limits?.accounts ?? 10,
      color: '#4361ee'
    },
    {
      label: '已授权群组',
      value: session.selectedAccount
        ? scopedGroups.value.filter((group) => group.active !== false).length
        : overview.value.groups,
      limit: overview.value.limits?.groups ?? 100,
      color: '#13a584'
    },
    {
      label: 'AI 群组',
      value: session.selectedAccount
        ? scopedGroups.value.filter((group) => group.active !== false && group.ai_enabled).length
        : overview.value.ai_groups,
      limit: overview.value.limits?.ai_groups ?? 100,
      color: '#b477e2'
    },
    {
      label: isMaiBot.value ? '待启用 AI 群' : '待审核申请',
      value: session.selectedAccount
        ? isMaiBot.value
          ? scopedGroups.value.filter((group) => group.ai_requested && !group.ai_enabled).length
          : data.claims.filter(
              (claim) => claim.account_id === session.selectedAccount && claim.state === 'pending'
            ).length
        : isMaiBot.value
          ? overview.value.ai_requests
          : overview.value.pending_claims,
      limit: 0,
      color: '#e5a339'
    }
  ])
  const filteredRows = computed(() =>
    (data[page.value] || []).filter((r) => {
      const account = session.selectedAccount
      if (account && page.value === 'accounts' && r.id !== account) return false
      if (
        account &&
        ['groups', 'operations', 'claims', 'personas'].includes(page.value) &&
        ![r.account_id, r.primary_account_id, r.ai_account_id].includes(account)
      )
        return false
      if (
        account &&
        ['ledger', 'activities', 'tests'].includes(page.value) &&
        !data.groups.some(
          (g) =>
            g.group_id === r.group_id && [g.primary_account_id, g.ai_account_id].includes(account)
        )
      )
        return false
      return !search.value || JSON.stringify(r).toLowerCase().includes(search.value.toLowerCase())
    })
  )
  const createPermissions: Record<string, string> = {
    users: 'users:manage',
    plans: 'plans:manage',
    claims: 'claims:create',
    personas: 'personas:manage',
    tests: 'tests:manage',
    ledger: 'ledger:create',
    activities: 'activities:create'
  }
  const createLabel = computed(() =>
    session.permissions.includes(createPermissions[page.value])
      ? {
          users: '创建用户',
          plans: '新建套餐',
          claims: '添加群组',
          personas: '配置人设',
          tests: '创建测试窗口',
          ledger: '新增记录',
          activities: '创建活动'
        }[page.value] || ''
      : ''
  )
  let controller: AbortController | null = null
  let refreshGeneration = 0
  let mutationGeneration = 0
  let interval: ReturnType<typeof setInterval> | undefined
  const viewSearch = new Map<string, string>()
  const workspaceViews = new Map<
    string,
    { page: string; data: Record<string, Row[]>; overview: Row }
  >()
  watch(
    () => `${session.workspace}:${page.value}:${session.selectedAccount}`,
    (next, previous) => {
      if (previous) viewSearch.set(previous, search.value)
      search.value = viewSearch.get(next) || ''
    }
  )
  watch(
    () => session.workspace,
    (next, previous) => {
      workspaceViews.set(previous, {
        page: page.value,
        data: clone(data),
        overview: clone(overview.value)
      })
      controller?.abort()
      refreshGeneration++
      mutationGeneration++
      persistDraft()
      dialog.open = false
      nativeLoginOpen.value = false
      nativeTarget.value = null
      removalOpen.value = false
      removalTarget.value = null
      saving.value = false
      const stored = workspaceViews.get(next)
      for (const key of Object.keys(data)) data[key] = stored?.data[key] || []
      overview.value = stored?.overview || {}
      page.value = stored?.page || 'overview'
      if (!visibleNavigation.value.some((n) => n.key === page.value)) page.value = 'overview'
      location.hash = page.value
      void refresh()
    }
  )
  watch(
    () => session.selectedAccount,
    () => {
      mutationGeneration++
      if (dialog.open) {
        persistDraft()
        dialog.open = false
      }
      saving.value = false
    }
  )
  watch(
    () => session.user?.id,
    () => {
      controller?.abort()
      refreshGeneration++
      mutationGeneration++
      saving.value = false
      for (const key of Object.keys(data)) data[key] = []
      overview.value = {}
      dialog.open = false
      dialog.scope = ''
      dialog.account = ''
      form.value = {}
      page.value = 'overview'
      search.value = ''
      viewSearch.clear()
      workspaceViews.clear()
      removalOpen.value = false
      removalTarget.value = null
    }
  )

  function navigate(key: string) {
    if (!visibleNavigation.value.some((item) => item.key === key)) key = 'overview'
    page.value = key
    sidebarOpen.value = false
    location.hash = key
  }
  function switchWorkbench(value: 'wsl' | 'maibot') {
    session.selectWorkspace(value)
  }
  function openNativeLogin(row: Row | null) {
    nativeTarget.value = row
    nativeLoginOpen.value = true
  }
  function accountCreated(account: Row) {
    session.selectAccount(account.id)
    void refresh()
  }
  function openAccountRemoval(account: Row) {
    removalTarget.value = account
    removalOpen.value = true
  }
  function accountRemoved(result: Row, owner: string) {
    if (owner !== session.user?.id) return
    controller?.abort()
    refreshGeneration++
    mutationGeneration++
    session.removeAccountData(result.id, result.workspace, result.management_group_ids)
    nativeLogin.value?.forgetAccount(result.id)
    const removedScopes = new Set([result.id, ...(result.management_group_ids || [])])
    if (
      dialog.scope.split(':').some((part) => removedScopes.has(part)) ||
      dialog.account === result.id
    ) {
      dialog.open = false
      dialog.scope = ''
      dialog.account = ''
      form.value = {}
    }
    if (removalTarget.value?.id === result.id) {
      removalOpen.value = false
      removalTarget.value = null
    }
    if (nativeTarget.value?.id === result.id) {
      nativeLoginOpen.value = false
      nativeTarget.value = null
    }
    for (const key of viewSearch.keys())
      if (key.split(':').includes(result.id)) viewSearch.delete(key)
    workspaceViews.clear()
    const removedResources = new Set([
      result.id,
      ...data.claims.filter((row) => row.account_id === result.id).map((row) => row.id),
      ...data.operations.filter((row) => row.account_id === result.id).map((row) => row.id)
    ])
    data.accounts = data.accounts.filter((row) => row.id !== result.id)
    for (const key of ['personas', 'claims', 'operations']) {
      data[key] = data[key].filter((row) => row.account_id !== result.id)
    }
    data.groups = data.groups.map((row) =>
      removedScopes.has(row.group_id)
        ? {
            ...row,
            primary_account_id: null,
            ai_account_id: null,
            active: false,
            management_enabled: false,
            ai_enabled: false,
            ai_requested: false,
            config: {},
            account_name: '机器人已删除'
          }
        : row.ai_account_id === result.id
          ? { ...row, ai_account_id: null, ai_enabled: false, ai_requested: false }
          : row
    )
    data.tests = data.tests
      .filter((row) => !(row.account_ids?.length === 1 && row.account_ids[0] === result.id))
      .map((row) => {
        if (!row.account_ids?.includes(result.id)) return row
        const accountEpochs = { ...row.account_epochs }
        delete accountEpochs[result.id]
        return {
          ...row,
          cancelled: true,
          account_ids: row.account_ids.filter((id: string) => id !== result.id),
          account_epochs: accountEpochs
        }
      })
    data.audit = data.audit.filter(
      (row) =>
        !removedResources.has(row.resource_id) &&
        row.detail?.account_id !== result.id &&
        !row.detail?.account_ids?.includes(result.id) &&
        !row.detail?.AccountIDs?.includes(result.id)
    )
    ElMessage.success('机器人已删除，关联账号数据已清理')
    void refresh()
  }
  function connectionLabel(account: Row) {
    if (account.runtime_state === 'connected') return '在线'
    if (account.runtime_state === 'connecting') return '连接中'
    if (account.runtime_state === 'reconnecting') return '重连中'
    return account.auth_state === 'authenticated' ? '已登录 · 未连接' : '未登录'
  }
  function aiStateLabel(group: Row) {
    if (group.ai_enabled) return '已启用'
    if (group.ai_request_state === 'pending_invite') return '待验收 · 未拉群'
    if (group.ai_requested) return '待验收 · 未启用'
    return '已停用'
  }
  async function forgetNative(row: Row) {
    try {
      await ElMessageBox.confirm(
        `清除 ${row.name} 的加密原生会话？账号和历史记录保留。`,
        '清除原生会话',
        { type: 'warning' }
      )
      await api(`/accounts/${row.id}/forget-session`, 'POST', { version: row.version })
      await refresh()
    } catch (e) {
      if (e instanceof Error) ElMessage.error(e.message)
    }
  }
  function date(value: string) {
    return value
      ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
      : '—'
  }
  function money(value: string) {
    const n = BigInt(value)
    const abs = n < 0n ? -n : n
    return `${n < 0n ? '-' : ''}${abs / 100n}.${String(abs % 100n).padStart(2, '0')}`
  }
  function statusLabel(state: string) {
    return (
      (
        {
          pending: '待审核',
          approved: '已批准',
          rejected: '已拒绝',
          expired: '已过期',
          queued: '排队中',
          running: '执行中',
          succeeded: '成功',
          failed: '失败',
          cancelled: '已取消',
          unknown: '结果未知',
          open: '报名中',
          closed: '已截止',
          drawn: '已开奖'
        } as Record<string, string>
      )[state] || state
    )
  }
  function statusType(state: string): 'info' | 'success' | 'warning' | 'danger' {
    return ['succeeded', 'approved'].includes(state)
      ? 'success'
      : ['failed', 'rejected'].includes(state)
        ? 'danger'
        : ['pending', 'unknown', 'running'].includes(state)
          ? 'warning'
          : 'info'
  }
  function lines(value: string) {
    return (value || '')
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean)
  }
  async function login() {
    busy.value = true
    loginError.value = ''
    try {
      const result = await api<{ user: any }>('/auth/login', 'POST', credentials)
      session.setUser(result.user)
      credentials.password = ''
      await session.restore()
      await refresh()
    } catch (e) {
      loginError.value = (e as Error).message
    } finally {
      busy.value = false
    }
  }
  async function logout() {
    try {
      await session.logout()
      controller?.abort()
      refreshGeneration++
      for (const key of Object.keys(data)) data[key] = []
      overview.value = {}
      dialog.open = false
      form.value = {}
    } catch (e) {
      ElMessage.error((e as Error).message)
    }
  }
  async function refresh() {
    if (!session.user) return
    const owner = session.user.id
    const workspace = session.workspace
    const generation = ++refreshGeneration
    controller?.abort()
    controller = new AbortController()
    const signal = controller.signal
    refreshing.value = true
    loadError.value = ''
    try {
      const me = await request<{ user: any; permissions: string[] }>(
        '/me',
        'GET',
        undefined,
        signal
      )
      if (
        generation !== refreshGeneration ||
        owner !== session.user?.id ||
        workspace !== session.workspace
      )
        return
      session.permissions = me.permissions
      session.user = me.user
      if (workspace === 'maibot' && !can('workspaces:maibot')) {
        session.selectWorkspace('wsl')
        return
      }
      const candidates: Record<string, [string, string]> = isMaiBot.value
        ? {
            accounts: ['/accounts', 'accounts:read'],
            groups: ['/groups', 'groups:read'],
            personas: ['/personas', 'personas:read']
          }
        : {
            accounts: ['/accounts', 'accounts:read'],
            groups: ['/groups', 'groups:read'],
            claims: ['/claims', 'claims:read'],
            operations: ['/operations', 'operations:read'],
            ledger: ['/ledger', 'ledger:read'],
            activities: ['/activities', 'activities:read'],
            audit: ['/audit', 'audit:read'],
            users: ['/users', 'users:manage'],
            plans: ['/plans', 'plans:manage'],
            tests: ['/test-windows', 'tests:manage']
          }
      const endpoints = Object.fromEntries(
        Object.entries(candidates)
          .filter(([, [, permission]]) => can(permission))
          .map(([key, [path]]) => [key, path])
      )
      const results = await Promise.all(
        Object.entries(endpoints).map(
          async ([key, path]) => [key, await api<Row[]>(path, 'GET', undefined, signal)] as const
        )
      )
      const nextOverview = can('overview:read')
        ? await api<Row>('/overview', 'GET', undefined, signal)
        : {}
      if (
        generation !== refreshGeneration ||
        owner !== session.user?.id ||
        workspace !== session.workspace
      )
        return
      for (const key of Object.keys(data)) if (!(key in endpoints)) data[key] = []
      for (const [key, value] of results) data[key] = value
      overview.value = nextOverview
      if (session.selectedAccount && !data.accounts.some((a) => a.id === session.selectedAccount))
        session.selectAccount('')
      if (!visibleNavigation.value.some((n) => n.key === page.value))
        page.value = visibleNavigation.value[0]?.key || 'overview'
      lastRefreshed.value = new Date().toISOString()
    } catch (e) {
      if (signal.aborted) return
      if (e instanceof ApiError && e.status === 401) session.clear()
      else loadError.value = (e as Error).message
    } finally {
      if (generation === refreshGeneration) refreshing.value = false
    }
  }
  function clone<T>(value: T): T {
    return JSON.parse(JSON.stringify(value))
  }
  function openDialog(kind: string, title: string, value: Row, id = '', scope = '') {
    dialog.kind = kind
    dialog.title = title
    dialog.id = id
    dialog.scope = scope
    dialog.account = kind === 'persona' ? value.account_id : ''
    const draft = scope ? (session.drafts[scope] as Row | undefined) : undefined
    const hasDraft = draft && typeof draft.version === 'number'
    form.value = clone(hasDraft ? draft : value)
    saveErrorCode.value = hasDraft && draft.version !== value.version ? 'version_conflict' : ''
    saveError.value = saveErrorCode.value ? '保留了旧版本草稿；请核对最新版本后再保存' : ''
    dialog.open = true
  }
  function persistDraft() {
    if (dialog.scope) session.saveDraft(dialog.scope, form.value)
    if (dialog.kind === 'user') form.value.password = ''
  }
  function openCreate() {
    const kind = (
      {
        accounts: 'account',
        users: 'user',
        plans: 'plan',
        claims: 'claim',
        personas: 'persona',
        tests: 'test',
        ledger: 'ledger',
        activities: 'activity'
      } as Record<string, string>
    )[page.value]
    const initial: Record<string, Row> = {
      account: { name: '' },
      user: { tenant_name: '', username: '', password: '' },
      plan: { name: '', group_limit: 1, ai_group_limit: 0, monthly_ai_tokens: 0, modules: [] },
      claim: { code: '', native_identity: '' },
      persona: {
        account_id: session.selectedAccount || data.accounts[0]?.id || '',
        nickname: '',
        personality: '',
        reply_style: '',
        version: 0
      },
      test: {
        group_id: '',
        account_ids: [],
        targets: '',
        actions: ['send_text'],
        seconds: 60,
        budget: 2
      },
      ledger: {
        group_id: '',
        member_id: '',
        kind: 'reward',
        amount: '',
        external_key: crypto.randomUUID(),
        note: ''
      },
      activity: { group_id: '', name: '', closes_at: '', winners: 1 }
    }
    openDialog(kind, createLabel.value, initial[kind])
    if (kind === 'persona' && form.value.account_id) selectPersonaAccount(form.value.account_id)
  }
  function editGroup(row: Row) {
    openDialog(
      'group',
      row.name,
      {
        version: row.version,
        management_enabled: row.management_enabled,
        ai_enabled: row.ai_enabled,
        blocked_words: (row.config.blocked_words || []).join('\n'),
        blacklist: (row.config.blacklist || []).join('\n'),
        exemptions: (row.config.exemptions || []).join('\n'),
        keyword_replies: row.config.keyword_replies || [],
        ai_cooldown_seconds: row.config.ai_cooldown_seconds ?? 0
      },
      row.group_id,
      `${session.workspace}:${session.user!.tenant_id || 'platform'}:${row.primary_account_id}:${row.group_id}`
    )
  }
  function personaScope(account: string) {
    return `${session.workspace}:${session.user!.tenant_id || 'platform'}:${account}:persona`
  }
  function openGroupAI(row: Row) {
    openDialog(
      'group-ai',
      `${row.name} · 群 AI`,
      {
        version: row.version,
        enabled: row.ai_requested || row.ai_enabled,
        invite: row.ai_request_state === 'pending_invite'
      },
      row.group_id,
      `${session.workspace}:${session.user!.tenant_id || 'platform'}:${row.primary_account_id}:${row.group_id}:ai`
    )
  }
  async function openUserAccess(row: Row) {
    const owner = session.user?.id,
      workspace = session.workspace
    try {
      const [catalog, access] = await Promise.all([
        api<Row>('/access/catalog'),
        api<Row>(`/users/${row.id}/access`)
      ])
      if (owner !== session.user?.id || workspace !== session.workspace) return
      accessCatalog.value = catalog
      openDialog(
        'access',
        `${row.username} · 权限`,
        { role_code: access.role_code, permissions: access.permissions, version: access.version },
        row.id
      )
    } catch (e) {
      ElMessage.error((e as Error).message)
    }
  }
  function selectAccessRole(code: string) {
    const role = (accessCatalog.value.roles || []).find((entry: Row) => entry.code === code)
    if (role) form.value.permissions = [...role.permissions]
  }
  function openPersona(row: Row) {
    openDialog(
      'persona',
      `${row.account_name} · 人设`,
      {
        account_id: row.account_id,
        nickname: row.nickname,
        personality: row.personality,
        reply_style: row.reply_style,
        version: row.version
      },
      row.account_id,
      personaScope(row.account_id)
    )
  }
  function selectPersonaAccount(id: string) {
    if (dialog.scope && dialog.account !== id) {
      session.saveDraft(dialog.scope, { ...form.value, account_id: dialog.account })
    }
    const row = data.personas.find((p) => p.account_id === id)
    const initial = row
      ? {
          account_id: id,
          nickname: row.nickname,
          personality: row.personality,
          reply_style: row.reply_style,
          version: row.version
        }
      : { account_id: id, nickname: '', personality: '', reply_style: '', version: 0 }
    dialog.scope = personaScope(id)
    dialog.account = id
    const draft = session.drafts[dialog.scope] as Row | undefined
    const hasDraft = draft && typeof draft.version === 'number'
    form.value = clone(hasDraft ? draft : initial)
    saveErrorCode.value = hasDraft && draft.version !== initial.version ? 'version_conflict' : ''
    saveError.value = saveErrorCode.value ? '保留了旧版本草稿；请核对最新版本后再保存' : ''
  }
  function openReview(row: Row) {
    openDialog(
      'review',
      `${row.group_name} · 授权审核`,
      { approve: true, identity_verified: false, note: '' },
      row.id
    )
  }
  function openSubscription(row: Row) {
    openDialog(
      'subscription',
      `${row.tenant_name} · 套餐`,
      {
        plan_id: row.plan_id || '',
        expires_at: row.expires_at || '',
        enabled: row.subscription_enabled ?? true,
        version: row.subscription_version || 0
      },
      row.tenant_id
    )
  }
  async function save() {
    saving.value = true
    saveError.value = ''
    const owner = session.user?.id
    const workspace = session.workspace
    const generation = ++mutationGeneration
    const frozen = { ...dialog },
      input = clone(form.value)
    try {
      let path = '',
        method = 'POST',
        body: Row = input
      switch (frozen.kind) {
        case 'access':
          path = `/users/${frozen.id}/access`
          method = 'PUT'
          break
        case 'group-ai':
          path = `/groups/${frozen.id}/ai-service`
          method = 'PUT'
          break
        case 'user':
          path = '/users'
          break
        case 'plan':
          path = '/plans'
          break
        case 'claim':
          path = '/claims'
          break
        case 'review':
          path = `/claims/${frozen.id}/review`
          break
        case 'subscription':
          path = `/subscriptions/${frozen.id}`
          method = 'PUT'
          break
        case 'persona':
          path = `/personas/${input.account_id}`
          method = 'PUT'
          delete body.account_id
          break
        case 'group':
          path = `/groups/${frozen.id}`
          method = 'PATCH'
          body = {
            version: input.version,
            management_enabled: input.management_enabled,
            ai_enabled: input.ai_enabled,
            config: {
              blocked_words: lines(input.blocked_words),
              blacklist: lines(input.blacklist),
              exemptions: lines(input.exemptions),
              keyword_replies: input.keyword_replies,
              ai_cooldown_seconds: input.ai_cooldown_seconds
            }
          }
          break
        case 'test':
          path = '/test-windows'
          body = {
            group_id: input.group_id,
            account_ids: input.account_ids,
            target_ids: lines(input.targets),
            actions: input.actions,
            seconds: input.seconds,
            budget: input.budget
          }
          break
        case 'ledger':
          path = '/ledger'
          break
        case 'activity':
          path = '/activities'
          break
      }
      await api(path, method, body)
      if (
        owner !== session.user?.id ||
        generation !== mutationGeneration ||
        workspace !== session.workspace
      )
        return
      if (frozen.scope) session.saveDraft(frozen.scope, {})
      dialog.scope = ''
      dialog.open = false
      ElMessage.success(frozen.kind === 'group-ai' ? '申请已保存，尚未执行拉群或启用' : '已保存')
      await refresh()
    } catch (e) {
      if (
        owner !== session.user?.id ||
        generation !== mutationGeneration ||
        workspace !== session.workspace
      )
        return
      saveError.value = (e as Error).message
      saveErrorCode.value = e instanceof ApiError ? e.code : ''
      if (frozen.scope)
        session.saveDraft(frozen.scope, {
          ...input,
          ...(frozen.kind === 'persona' ? { account_id: frozen.account } : {})
        })
    } finally {
      if (generation === mutationGeneration) saving.value = false
    }
  }
  async function reloadVersion() {
    const frozen = { ...dialog },
      owner = session.user?.id
    const generation = ++mutationGeneration
    saving.value = true
    try {
      const groupKind = frozen.kind === 'group' || frozen.kind === 'group-ai'
      const path = groupKind ? '/groups' : frozen.kind === 'persona' ? '/personas' : '/users'
      const current =
        frozen.kind === 'access'
          ? await api<Row>(`/users/${frozen.id}/access`)
          : (await api<Row[]>(path)).find((row) =>
              groupKind
                ? row.group_id === frozen.id
                : frozen.kind === 'persona'
                  ? row.account_id === frozen.account
                  : row.tenant_id === frozen.id
            )
      if (!current) throw new Error('资源已失效或不再拥有访问权限')
      const version =
        frozen.kind === 'subscription' ? current.subscription_version : current.version
      await ElMessageBox.confirm(
        `服务器最新版本为 ${version}。继续会保留当前输入并采用该版本号；再次保存前请核对配置，避免覆盖他人的修改。`,
        '配置版本冲突'
      )
      if (
        generation !== mutationGeneration ||
        owner !== session.user?.id ||
        frozen.scope !== dialog.scope ||
        frozen.id !== dialog.id
      )
        return
      form.value.version = version
      saveError.value = ''
      saveErrorCode.value = ''
      if (frozen.scope) session.saveDraft(frozen.scope, form.value)
    } catch (e) {
      if (e instanceof Error && owner === session.user?.id) saveError.value = e.message
    } finally {
      if (generation === mutationGeneration) saving.value = false
    }
  }
  async function revokeGroup(row: Row) {
    try {
      await ElMessageBox.confirm(
        `撤销「${row.name}」的授权？自动化与 AI 将停用，群归属、历史台账和证据仍保留。`,
        '撤销群授权',
        { type: 'warning' }
      )
      await api(`/groups/${row.group_id}/revoke`, 'POST', { version: row.version })
      await refresh()
    } catch (e) {
      if (e instanceof Error) ElMessage.error(e.message)
    }
  }
  async function toggleUser(row: Row) {
    try {
      await ElMessageBox.confirm(
        `${row.disabled ? '启用' : '禁用'}用户 ${row.username}？`,
        '用户状态'
      )
      await api(`/users/${row.id}`, 'PATCH', { disabled: !row.disabled })
      await refresh()
    } catch (e) {
      if (e instanceof Error) ElMessage.error(e.message)
    }
  }
  async function cancelOperation(row: Row) {
    try {
      await api(`/operations/${row.id}/cancel`, 'POST')
      await refresh()
    } catch (e) {
      ElMessage.error((e as Error).message)
    }
  }
  async function cancelTest(row: Row) {
    try {
      await api(`/test-windows/${row.id}/cancel`, 'POST')
      await refresh()
    } catch (e) {
      ElMessage.error((e as Error).message)
    }
  }
  async function draw(row: Row) {
    try {
      await ElMessageBox.confirm(`确认对「${row.name}」开奖？`, '活动开奖')
      const result = await api<{ winners: string[] }>(`/activities/${row.id}/draw`, 'POST', {
        version: row.version
      })
      ElMessage.success(`中奖成员：${result.winners.join('、')}`)
      await refresh()
    } catch (e) {
      if (e instanceof Error) ElMessage.error(e.message)
    }
  }
  onMounted(async () => {
    try {
      await session.restore()
      const hash = location.hash.slice(1)
      if (visibleNavigation.value.some((n) => n.key === hash)) page.value = hash
      await refresh()
    } catch {
      /* An unauthenticated browser starts at login. */
    }
    interval = setInterval(() => {
      if (!dialog.open) void refresh()
    }, 15000)
  })
  onUnmounted(() => {
    controller?.abort()
    clearInterval(interval)
  })
</script>
