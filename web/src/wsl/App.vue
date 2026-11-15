<template>
  <div v-if="!session.ready" class="boot-state"
    ><ElIcon class="is-loading"><Loading /></ElIcon
  ></div>
  <CustomerInvitation
    v-else-if="invitationVisible"
    :token="invitationToken"
    :user="session.user"
    @close="closeCustomerInvitation"
    @login="invitationVisible = false"
    @logout="logout"
    @complete="finishCustomerInvitation"
  />
  <main v-else-if="!session.user" class="login-shell">
    <header class="login-header">
      <a href="#" class="login-brand"><AdminMark />A哥后台 - 机器人</a>
      <span class="login-header-label"><span class="login-header-mark"></span>工作空间</span>
    </header>
    <section class="login-form" aria-labelledby="login-title">
      <div class="login-platforms" aria-label="支持的平台">
        <img src="/wsl-logo.png" alt="旺商聊" />
        <span class="login-platform-divider"></span>
        <img src="/lajiaoliao-logo.png" alt="辣椒聊" />
      </div>
      <h1 id="login-title">登录工作空间</h1>
      <p class="muted">欢迎回来</p>
      <ElForm label-position="top" @submit.prevent="login">
        <ElFormItem label="用户名" for="platform-username"
          ><ElInput
            id="platform-username"
            v-model="credentials.username"
            autocomplete="username"
            placeholder="输入用户名"
            :prefix-icon="User"
            :disabled="busy"
            autofocus
            required
        /></ElFormItem>
        <ElFormItem label="密码" for="platform-password"
          ><ElInput
            id="platform-password"
            v-model="credentials.password"
            type="password"
            show-password
            placeholder="输入密码"
            :prefix-icon="Lock"
            autocomplete="current-password"
            :disabled="busy"
            required
        /></ElFormItem>
        <div aria-live="polite">
          <ElAlert v-if="loginError" :title="loginError" type="error" :closable="false" />
        </div>
        <ElButton
          native-type="submit"
          type="primary"
          :loading="busy"
          :disabled="!credentials.username.trim() || !credentials.password"
          class="login-submit"
          >登录<ElIcon v-if="!busy"><ArrowRight /></ElIcon
        ></ElButton>
      </ElForm>
      <ElButton v-if="invitationToken" text :icon="ArrowRight" @click="invitationVisible = true"
        >返回客户邀请</ElButton
      >
      <div class="login-note"
        ><ElIcon><Lock /></ElIcon>仅限已授权的平台账号</div
      >
    </section>
  </main>
  <div v-else class="platform-shell" :class="{ 'nav-collapsed': sidebarCollapsed }">
    <a class="skip-navigation" href="#workspace-content" @click.prevent="focusWorkspace"
      >跳到工作区</a
    >
    <aside
      id="primary-sidebar"
      class="sidebar"
      :class="{ open: sidebarOpen }"
      @keydown.esc="closeSidebar"
    >
      <div class="brand">
        <AdminMark />
        <span class="brand-name">{{ isUserCenter ? '用户中心' : '管理后台' }}</span>
        <WorkspaceSwitcher
          :items="session.panels"
          :selected="session.workspace"
          :console="session.console"
          @select="switchWorkbench"
        />
        <ElButton
          class="sidebar-close"
          text
          :icon="Close"
          aria-label="收起导航"
          @click="closeSidebar"
        />
      </div>
      <button
        v-if="isAdmin"
        type="button"
        class="console-switch"
        :aria-label="isUserCenter ? '切换到管理后台' : '切换到用户中心'"
        @click="switchConsole(isUserCenter ? 'admin' : 'user')"
      >
        <ElIcon><component :is="isUserCenter ? Setting : User" /></ElIcon>
        <span>{{ isUserCenter ? '管理后台' : '用户中心' }}</span>
        <ElIcon><ArrowRight /></ElIcon>
      </button>
      <button
        v-if="visibleNavigation.some((item) => item.key === 'assistant')"
        type="button"
        class="sidebar-assistant"
        :class="{ active: page === 'assistant' }"
        :aria-current="page === 'assistant' ? 'page' : undefined"
        @click="navigate('assistant')"
      >
        <span class="sidebar-assistant-icon"><AdminMark /></span>
        <span class="sidebar-assistant-copy"
          ><strong>群管助手</strong><small>管理我的群组</small></span
        >
        <ElIcon><ArrowRight /></ElIcon>
      </button>
      <nav aria-label="主导航">
        <section
          v-for="group in navigationGroups"
          :key="group.key"
          class="nav-group"
          :aria-label="group.label"
        >
          <h2 class="nav-group-title">
            <button
              type="button"
              :aria-expanded="!collapsedGroups[group.key]"
              :aria-controls="`nav-items-${group.key}`"
              @click="collapsedGroups[group.key] = !collapsedGroups[group.key]"
              >{{ group.label }}<ElIcon><ArrowDown /></ElIcon
            ></button>
          </h2>
          <div
            :id="`nav-items-${group.key}`"
            v-show="!collapsedGroups[group.key] || sidebarCollapsed"
            class="nav-group-items"
          >
            <button
              v-for="item in group.items"
              :key="item.key"
              class="nav-link"
              :title="sidebarCollapsed ? item.label : undefined"
              :aria-label="item.label"
              :aria-current="
                page === item.key ||
                (item.key === 'features' && ['ledger', 'activities'].includes(page))
                  ? 'page'
                  : undefined
              "
              :class="{
                active:
                  page === item.key ||
                  (item.key === 'features' && ['ledger', 'activities'].includes(page))
              }"
              @click="navigate(item.key)"
              ><ElIcon><component :is="item.icon" /></ElIcon><span>{{ item.label }}</span>
              <span v-if="item.key === 'claims' && overview.pending_claims" class="nav-count">{{
                overview.pending_claims
              }}</span>
            </button>
          </div>
        </section>
      </nav>
    </aside>
    <div v-if="sidebarOpen" class="sidebar-scrim" @click="closeSidebar"></div>
    <section
      class="main-area"
      :class="{
        'messages-layout': page === 'messages' && isAdmin && !isUserCenter,
        'assistant-layout': page === 'assistant'
      }"
    >
      <header class="topbar">
        <ElTooltip
          :content="sidebarCollapsed ? '展开侧栏' : '折叠侧栏'"
          :disabled="mobileNavigation"
          :enterable="false"
        >
          <ElButton
            class="desktop-sidebar-toggle"
            text
            :aria-label="sidebarCollapsed ? '展开侧栏' : '折叠侧栏'"
            aria-controls="primary-sidebar"
            :aria-expanded="!sidebarCollapsed"
            @click="sidebarCollapsed = !sidebarCollapsed"
            ><ElIcon><Fold v-if="!sidebarCollapsed" /><Expand v-else /></ElIcon
          ></ElButton>
        </ElTooltip>
        <ElButton
          class="mobile-menu"
          text
          aria-label="展开导航"
          aria-controls="primary-sidebar"
          :aria-expanded="sidebarOpen"
          @click="sidebarOpen = !sidebarOpen"
          ><ElIcon><Menu /></ElIcon
        ></ElButton>
        <div class="breadcrumb"
          >{{ isUserCenter ? '用户中心' : '管理后台' }}<span>/</span
          ><strong>{{ pageTitle }}</strong></div
        >
        <strong
          v-if="isUserCenter || !accountScopedPages.includes(page)"
          class="mobile-page-title"
          >{{ pageTitle }}</strong
        >
        <div class="topbar-actions">
          <ElPopover v-if="!isUserCenter" trigger="click" width="260" placement="bottom-end">
            <template #reference>
              <ElButton text class="acceptance-status" aria-label="查看验收状态">
                <span class="status-dot warning"></span><span>验收状态</span>
              </ElButton>
            </template>
            <div class="acceptance-popover">
              <strong>运行验收</strong>
              <div><span>原生接入</span><ElTag type="warning" size="small">未验收</ElTag></div>
              <div><span>AI 隔离</span><ElTag type="warning" size="small">未验收</ElTag></div>
            </div>
          </ElPopover>
          <AssistantDrawer
            v-if="
              ['wsl', 'lajiaoliao'].includes(session.workspace) &&
              isUserCenter &&
              can('assistant:use') &&
              page !== 'assistant'
            "
            :accounts="data.accounts"
            :available-groups="data.groups"
            @platform="switchAssistantPlatform"
          />
          <ElSelect
            v-if="!isUserCenter && accountScopedPages.includes(page)"
            :model-value="session.selectedAccount || '__all__'"
            placeholder="选择机器人面板"
            aria-label="机器人面板"
            class="account-select"
            @change="session.selectAccount($event === '__all__' ? '' : $event)"
          >
            <ElOption label="工作台总览" value="__all__" />
            <ElOption
              v-for="a in data.accounts"
              :key="a.id"
              :value="a.id"
              :label="accountOptionLabel(a)"
            >
              <span>{{ accountOptionLabel(a) }}</span
              ><span class="account-option-status">{{ connectionLabel(a) }}</span>
            </ElOption>
          </ElSelect>
          <ElButton
            v-if="isAdmin && !isUserCenter"
            text
            class="desktop-action"
            aria-label="机器人连接状态"
            :icon="Connection"
            @click="connectionsOpen = true"
          />
          <ElDrawer
            v-if="isAdmin && !isUserCenter"
            v-model="connectionsOpen"
            title="机器人连接状态"
            size="min(440px, 100vw)"
            append-to-body
          >
            <div class="connection-popover">
              <div v-for="account in data.accounts" :key="account.id" class="connection-row">
                <button class="account-name-button" @click="session.selectAccount(account.id)">{{
                  accountOptionLabel(account)
                }}</button>
                <ElTag :type="account.runtime_state === 'connected' ? 'success' : 'info'">{{
                  connectionLabel(account)
                }}</ElTag>
                <ElButton
                  :icon="Key"
                  :loading="nativePreparing === account.id"
                  :disabled="!!nativePreparing"
                  @click="openNativeLogin(account)"
                  >{{ account.auth_state === 'authenticated' ? '重新登录' : '登录' }}</ElButton
                >
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
              <NativeDeployment v-if="session.workspace !== 'lajiaoliao'" @updated="refresh" />
            </div>
          </ElDrawer>
          <ElButton
            v-if="isAdmin && !isUserCenter"
            type="primary"
            :icon="Key"
            class="native-login-entry desktop-action"
            @click="openNativeLogin(null)"
            >注册 / 登录</ElButton
          >
          <ElTooltip content="刷新"
            ><ElButton
              class="desktop-action"
              text
              :loading="refreshing"
              aria-label="刷新"
              @click="refresh"
              ><ElIcon><Refresh /></ElIcon></ElButton
          ></ElTooltip>
          <ElDropdown class="desktop-action" @command="logout">
            <button class="user-menu" aria-label="用户菜单"
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
          <ElDropdown class="mobile-overflow" trigger="click" @command="mobileCommand">
            <ElButton text :icon="MoreFilled" aria-label="更多操作" title="更多操作" />
            <template #dropdown>
              <ElDropdownMenu>
                <ElDropdownItem v-if="isAdmin && !isUserCenter" command="login" :icon="Key"
                  >注册 / 登录</ElDropdownItem
                >
                <ElDropdownItem
                  v-if="isAdmin && !isUserCenter"
                  command="connections"
                  :icon="Connection"
                  >机器人连接状态</ElDropdownItem
                >
                <ElDropdownItem command="refresh" :icon="Refresh" :disabled="refreshing"
                  >刷新</ElDropdownItem
                >
                <ElDropdownItem command="logout" divided>退出登录</ElDropdownItem>
              </ElDropdownMenu>
            </template>
          </ElDropdown>
        </div>
      </header>
      <div
        id="workspace-content"
        class="content"
        tabindex="-1"
        :key="`${session.user.id}:${session.console}:${session.workspace}`"
        :class="{ 'messages-content': page === 'messages' && isAdmin && !isUserCenter }"
      >
        <div
          v-if="page !== 'assistant' && (page !== 'messages' || !isAdmin || isUserCenter)"
          class="page-heading"
          ><div
            ><h1>{{ pageTitle }}</h1
            ><p
              >{{
                isUserCenter
                  ? isAdmin
                    ? '演示测试套餐'
                    : '我的服务'
                  : workspaceNames[session.workspace]
              }}<span class="separator">/</span>{{ selectedAccountName }}</p
            ></div
          >
        </div>
        <ElAlert
          v-if="loadError"
          :title="loadError"
          type="error"
          :closable="false"
          class="load-error"
        />
        <TenantMembers
          v-if="page === 'members' && canManageMembers"
          :key="session.user?.id"
          :users="data.users"
          :initial-tenant="memberTenant"
        />
        <BillingCenter
          v-else-if="!isUserCenter && page === 'billing'"
          :key="session.user?.id"
          :users="data.users"
        />
        <AIGateway v-else-if="page === 'gateway' && isAdmin" />
        <AssistantGovernance
          v-else-if="
            page === 'assistant-admin' &&
            isAdmin &&
            ['wsl', 'lajiaoliao'].includes(session.workspace)
          "
          :audit-events="can('audit:read') ? data.audit : undefined"
          :audit-sampled-at="lastRefreshed"
          @gateway="navigate('gateway')"
        />
        <AssistantDrawer
          v-else-if="
            page === 'assistant' &&
            can('assistant:use') &&
            ['wsl', 'lajiaoliao'].includes(session.workspace)
          "
          :key="`${session.user.id}:${session.workspace}:${assistantInitialGroup}`"
          :accounts="data.accounts"
          :available-groups="data.groups"
          :initial-group="assistantInitialGroup"
          embedded
          @platform="switchAssistantPlatform"
        />
        <UserCenter
          v-else-if="
            isUserCenter &&
            (['overview', 'groups', 'operations', 'billing'].includes(page) ||
              (page === 'features' && !featureGroup && can('groups:read')))
          "
          :view="page === 'billing' ? 'usage' : page === 'features' ? 'groups' : page"
          :refresh-token="lastRefreshed"
          :onboarding-request="onboardingRequest"
          @onboarding-consumed="consumeOnboardingRequest"
          @group="openUserGroup"
          @assistant="openUserAssistant"
          @manage="openUserManagement"
          @view="navigate"
          @invite="customerInvitationOpen = true"
        />
        <ElTabs
          v-if="!isUserCenter && !isMaiBot && ['features', 'ledger', 'activities'].includes(page)"
          :model-value="page === 'features' ? featureView : 'modules'"
          @update:model-value="selectFeatureSection(String($event))"
        >
          <ElTabPane v-if="can('groups:read')" label="原生功能表" name="native" />
          <ElTabPane
            v-if="session.user.role === 'superadmin'"
            label="账号与人工操作"
            name="manual"
          />
        </ElTabs>
        <FeatureCenter
          v-if="
            (page === 'features' || isBusinessDetail) &&
            can('groups:read') &&
            (!isUserCenter || featureGroup)
          "
          :key="`${session.console}:${session.workspace}:${featureGroup}`"
          :account="isUserCenter ? '' : session.selectedAccount"
          :groups="featureGroups"
          :initial-group="featureGroup"
          :view="featureView"
          @selected="featureGroup = $event"
          @detail="openFeatureDetail"
          @ai="openGroupAI"
          @updated="refresh"
        />
        <section
          v-else-if="
            (page === 'features' || isBusinessDetail) && (!isUserCenter || !can('groups:read'))
          "
          class="business-directory"
          aria-label="业务模块"
        >
          <button
            v-for="entry in businessEntries"
            :key="entry.key"
            class="business-entry"
            @click="openFeatureDetail(entry.key)"
          >
            <ElIcon><component :is="entry.icon" /></ElIcon>
            <strong>{{ entry.label }}</strong>
            <ElIcon><ArrowRight /></ElIcon>
          </button>
        </section>
        <MessageCenter
          v-else-if="!isUserCenter && page === 'messages' && isAdmin"
          :account="session.selectedAccount"
          :workspace="session.workspace"
        />
        <TestTools
          v-else-if="!isUserCenter && page === 'tests' && isAdmin"
          :key="session.workspace"
          :workspace="session.workspace"
          :account="session.selectedAccount"
          :accounts="data.accounts"
          :groups="data.groups"
          :windows="data.tests"
          @create="openCreateTest"
          @cancel="cancelTest"
          @updated="refresh"
          @onboard="openControlledOnboarding"
        />
        <AccountDashboard
          v-else-if="!isUserCenter && page === 'overview' && selectedAccount"
          :account="selectedAccount"
          :workspace="session.workspace"
          :owner="session.user.id"
          :admin="isAdmin"
          :auth-preparing="nativePreparing === selectedAccount.id"
          :refresh-token="lastRefreshed"
          @remove="openAccountRemoval"
          @reauth="openNativeLogin"
          @messages="navigate('messages')"
        />
        <RobotWorkbench
          v-else-if="
            !isUserCenter &&
            page === 'overview' &&
            (session.workspace === 'lajiaoliao' || session.workspace === 'maibot')
          "
          :workspace="session.workspace"
          :accounts="data.accounts"
          :groups="data.groups"
          :personas="data.personas"
          :operations="!isMaiBot && can('operations:read') ? scopedOperations : undefined"
          :overview="overview"
          :permissions="session.permissions"
          :admin="isAdmin"
          :loading="refreshing"
          :preparing="nativePreparing"
          :sampled-at="lastRefreshed"
          @navigate="navigate"
          @account="session.selectAccount"
          @login="openNativeLogin(null)"
          @reauth="openNativeLogin"
        />
        <template v-else-if="!isUserCenter && page === 'overview'">
          <div class="metrics">
            <div v-for="metric in metrics" :key="metric.label" class="metric"
              ><span class="metric-label"
                ><ElIcon :style="{ color: metric.color }"><component :is="metric.icon" /></ElIcon
                >{{ metric.label }}</span
              ><strong
                >{{ metric.value ?? '—'
                }}<small v-if="metric.limit">/ {{ metric.limit }}</small></strong
              ></div
            >
          </div>
          <WorkspaceInsights
            :groups="data.groups"
            :operations="can('operations:read') ? scopedOperations : undefined"
            :overview="overview"
            :sampled-at="lastRefreshed"
          />
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
                      accountOptionLabel(row)
                    }}</button>
                  </template></ElTableColumn
                ><ElTableColumn label="连接"
                  ><template #default="{ row }"
                    ><ElTag :type="row.runtime_state === 'connected' ? 'success' : 'info'">{{
                      connectionLabel(row)
                    }}</ElTag></template
                  ></ElTableColumn
                >
                <ElTableColumn label="登录" min-width="135"
                  ><template #default="{ row }">
                    <ElButton
                      v-if="isAdmin"
                      type="primary"
                      plain
                      :icon="Key"
                      :aria-label="`${row.auth_state === 'authenticated' ? '重新登录' : '登录'} ${row.name}`"
                      :loading="nativePreparing === row.id"
                      :disabled="!!nativePreparing"
                      @click="openNativeLogin(row)"
                      >{{
                        row.auth_state === 'authenticated' ? '重新登录' : '注册 / 登录'
                      }}</ElButton
                    >
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
          <details class="capacity-section">
            <summary
              ><span>容量验收目标</span><ElTag type="info" effect="plain" size="small">待验证</ElTag
              ><ElIcon><ArrowDown /></ElIcon
            ></summary>
            <div class="capacity-grid"
              ><div
                ><strong>100<span>条/秒</span></strong
                ><p>聚合消息持续接入</p></div
              ><div
                ><strong>25<span>ms</span></strong
                ><p>可靠落盘 P95</p></div
              ><div
                ><strong>1<span>个引擎</span></strong
                ><p>最多 300 个独立聊天群，容量待验收</p></div
              ><div
                ><strong>8<span>核</span> / 32<span>GB</span></strong
                ><p>生产资源预算</p></div
              ></div
            >
          </details>
        </template>
        <SystemMonitor v-else-if="page === 'system-monitor'" :key="session.workspace" />
        <MaiBotConsole
          v-else-if="isMaiBot && page.startsWith('ai-')"
          :account="session.selectedAccount"
          :section="page.slice(3)"
          :title="pageTitle"
          @navigate="navigate"
          @updated="refresh"
        />
        <section
          v-else-if="
            !isUserCenter &&
            ![
              'members',
              'billing',
              'gateway',
              'assistant',
              'assistant-admin',
              'ledger',
              'activities'
            ].includes(page)
          "
          class="data-section"
        >
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
              <ElButton
                v-if="page === 'users' && isAdmin && can('users:manage')"
                :icon="Link"
                type="primary"
                @click="customerInvitationOpen = true"
                >邀请客户</ElButton
              >
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
              <ElTableColumn label="平台" min-width="90"
                ><template #default="{ row }">{{
                  row.platform === 'lajiaoliao' ? '辣椒聊' : '旺商聊'
                }}</template></ElTableColumn
              >
              <ElTableColumn label="群内热群机器人" min-width="180">
                <template #default="{ row }">{{ boundAccountLabel(row, true) }}</template>
              </ElTableColumn>
              <ElTableColumn label="状态" min-width="160"
                ><template #default="{ row }">{{ aiStateLabel(row) }}</template></ElTableColumn
              >
              <ElTableColumn v-if="isAdmin" label="操作" width="130"
                ><template #default="{ row }"
                  ><ElButton link type="primary" @click="assignmentGroup = row"
                    >分配账号</ElButton
                  ></template
                ></ElTableColumn
              >
            </template>
            <template v-else-if="page === 'groups'">
              <ElTableColumn prop="name" label="群组" min-width="170" />
              <ElTableColumn label="原生群管账号" min-width="160">
                <template #default="{ row }">{{ boundAccountLabel(row) }}</template>
              </ElTableColumn>
              <ElTableColumn label="管理" width="100"
                ><template #default="{ row }"
                  ><ElTag :type="row.management_enabled ? 'success' : 'info'">{{
                    row.management_enabled ? '已启用' : '已停用'
                  }}</ElTag></template
                ></ElTableColumn
              >
              <ElTableColumn label="群内热群机器人" min-width="160"
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
                    v-if="isAdmin && row.active"
                    link
                    type="primary"
                    @click="assignmentGroup = row"
                    >分配账号</ElButton
                  ><ElButton
                    v-if="!isMaiBot && row.active && can('groups:read')"
                    link
                    type="primary"
                    @click="openUserGroup({ ...row, platform: row.platform || session.workspace })"
                    >进入用户中心</ElButton
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
                    ><ElButton link type="primary" @click="openUserAccess(row)">网站权限</ElButton
                    ><ElButton
                      link
                      type="primary"
                      :disabled="!row.tenant_id"
                      @click="capabilityTenant = row"
                      >能力上限</ElButton
                    ><ElButton
                      link
                      type="primary"
                      :disabled="!row.tenant_id"
                      @click="openTenantMembers(row)"
                      >成员</ElButton
                    ><ElButton
                      v-if="row.owner_id !== row.id && !row.disabled"
                      link
                      type="primary"
                      @click="confirmOwner(row)"
                      >设为主账号</ElButton
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
              /><ElTableColumn prop="ai_group_limit" label="热群群名额" width="120" /><ElTableColumn
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
              <ElTableColumn label="群内热群机器人" min-width="180">
                <template #default="{ row }">{{
                  personaAccountLabel(row)
                }}</template> </ElTableColumn
              ><ElTableColumn prop="nickname" label="人设名称" min-width="120" />
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
        <footer v-if="!isUserCenter && (page !== 'messages' || !isAdmin)" class="page-footer"
          ><span>{{
            session.workspace === 'lajiaoliao' ? '辣椒聊管理平台' : '旺商聊管理平台'
          }}</span
          ><span>{{ lastRefreshed ? `更新于 ${date(lastRefreshed)}` : '尚未同步' }}</span></footer
        >
      </div>
    </section>
    <BusinessModuleDialog
      v-if="isBusinessDetail && canPage(page)"
      :open="true"
      :kind="page"
      :title="pageTitle"
      :scope="businessScope"
      v-model:group="businessGroup"
      :groups="businessGroups"
      :rows="filteredRows"
      :create-label="createLabel"
      :can-draw="can('activities:draw')"
      :loading="refreshing || businessLoading"
      :date="date"
      :money="money"
      :status-label="statusLabel"
      @close="selectFeatureSection('modules')"
      @create="openCreate"
      @draw="draw"
    />
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
              ><ElInputNumber v-model="form.group_limit" :min="0" :max="300" /></ElFormItem
            ><ElFormItem label="热群群名额"
              ><ElInputNumber v-model="form.ai_group_limit" :min="0" :max="300" /></ElFormItem
          ></div>
          <ElFormItem label="月度 AI Token"
            ><ElInputNumber
              v-model="form.monthly_ai_tokens"
              :min="0"
              :max="1000000000"
              :step="10000"
          /></ElFormItem>
          <ElFormItem label="月度群管助手 Token"
            ><ElInputNumber
              v-model="form.monthly_assistant_tokens"
              :min="0"
              :max="1000000000000"
              :step="10000"
          /></ElFormItem>
          <ElFormItem label="功能模块"
            ><ElCheckboxGroup v-model="form.modules"
              ><ElCheckbox v-for="(label, value) in moduleLabels" :key="value" :value="value">{{
                label
              }}</ElCheckbox></ElCheckboxGroup
            ></ElFormItem
          >
          <ElFormItem label="可用平台"
            ><ElCheckboxGroup v-model="form.allowed_platforms">
              <ElCheckbox value="wsl">旺商聊</ElCheckbox
              ><ElCheckbox value="lajiaoliao">辣椒聊</ElCheckbox>
            </ElCheckboxGroup></ElFormItem
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
          <ElFormItem label="向套餐用户发布"
            ><ElCheckbox v-model="form.published"
              >允许本平台授权群选择此人设</ElCheckbox
            ></ElFormItem
          >
          <ElFormItem label="群内热群机器人"
            ><ElSelect
              v-model="form.account_id"
              :disabled="Boolean(dialog.id)"
              @change="selectPersonaAccount"
              ><ElOption
                v-for="a in data.accounts"
                :key="a.id"
                :value="a.id"
                :label="accountOptionLabel(a)" /></ElSelect
          ></ElFormItem>
          <ElFormItem v-if="personaAccount" label="原生账号">
            <span
              >{{ personaAccount.platform === 'lajiaoliao' ? '辣椒聊' : '旺商聊' }} ·
              {{ personaAccount.native_uid || '尚未登录' }}</span
            >
          </ElFormItem>
          <ElFormItem label="人设名称"
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
            ><ElFormItem label="群内热群机器人"
              ><ElTag>{{ form.ai_enabled ? '已启用' : '未启用' }}</ElTag>
              <span>{{ form.heatbot_account_label }}</span></ElFormItem
            ></div
          >
          <ElButton
            v-if="!isMaiBot && can('groups:read')"
            :icon="Setting"
            @click="openFeatures({ group_id: dialog.id })"
            >功能管理</ElButton
          >
          <ElFormItem label="群内热群发言冷却 / 秒"
            ><ElInputNumber
              v-model="form.ai_cooldown_seconds"
              :min="0"
              :max="86400"
              :disabled="!can('groups:ai')"
          /></ElFormItem>
          <ElTag type="info">配置版本 {{ form.version }}</ElTag>
        </template>
        <template v-else-if="dialog.kind === 'group-ai'">
          <ElFormItem label="申请启用群内热群机器人"
            ><ElSwitch v-model="form.enabled"
          /></ElFormItem>
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
            ><ElSelect v-model="form.group_id" aria-label="业务操作群组"
              ><ElOption
                v-for="g in dialog.kind === 'test' ? testGroupChoices : businessGroups"
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
            <ElFormItem label="授权用途">
              <ElSelect
                v-model="form.admission_mode"
                aria-label="授权用途"
                @change="form.actions = ['mute_member', 'unmute_member']"
              >
                <ElOption label="原生调试（独立于生产业务开关）" value="native_debug" />
                <ElOption label="生产套餐校验" value="package" />
              </ElSelect>
            </ElFormItem>
            <ElFormItem label="测试账号"
              ><ElSelect
                v-model="form.account_ids"
                multiple
                aria-label="测试窗口账号"
                :multiple-limit="
                  form.actions.some((a: string) => ['friend_delete', 'friend_approve'].includes(a))
                    ? 1
                    : 0
                "
                ><ElOption
                  v-for="a in data.accounts"
                  :key="a.id"
                  :label="accountOptionLabel(a)"
                  :value="a.id" /></ElSelect
            ></ElFormItem>
            <ElFormItem label="允许动作"
              ><ElCheckbox
                v-if="isAdmin && form.admission_mode === 'native_debug'"
                :model-value="form.actions.includes('friend_approve')"
                @change="selectFriendApproveTest"
                >同意指定好友申请（独立授权，一次尝试）</ElCheckbox
              ><ElCheckbox
                v-if="isAdmin && form.admission_mode === 'native_debug'"
                :model-value="form.actions.includes('friend_delete')"
                @change="selectFriendDeleteTest"
                >删除好友（独立授权，一次尝试）</ElCheckbox
              ><ElCheckboxGroup
                v-if="
                  !form.actions.some((a: string) => ['friend_delete', 'friend_approve'].includes(a))
                "
                v-model="form.actions"
                ><ElCheckbox value="mute_member">成员禁言</ElCheckbox
                ><ElCheckbox value="unmute_member">成员解禁</ElCheckbox
                ><ElCheckbox value="mute_group">全群禁言</ElCheckbox
                ><ElCheckbox value="unmute_group">解除全群禁言</ElCheckbox
                ><ElCheckbox value="kick_member">移出成员</ElCheckbox
                ><ElCheckbox v-if="session.workspace === 'lajiaoliao'" value="kick_member_ban"
                  >移出并永久禁止申请进群</ElCheckbox
                ><ElCheckbox v-if="session.workspace === 'lajiaoliao'" value="remove_group_ban"
                  >解除原生进群限制</ElCheckbox
                ><ElCheckbox value="publish_notice">发布公告</ElCheckbox
                ><ElCheckbox value="grant_admin">设置群管理员（群主）</ElCheckbox
                ><ElCheckbox value="revoke_admin">撤销群管理员（群主）</ElCheckbox
                ><ElCheckbox v-if="session.workspace === 'lajiaoliao'" value="member_nickname"
                  >修改群内昵称</ElCheckbox
                ><ElCheckbox v-if="session.workspace === 'lajiaoliao'" value="group_info"
                  >修改群名称</ElCheckbox
                ><ElCheckbox value="group_invite">拉入群组</ElCheckbox
                ><ElCheckbox value="group_accept"
                  >接受本人入群邀请（单独授权，目标填本人 UID）</ElCheckbox
                ><ElCheckbox value="group_leave">退出群组（授权目标填本人 UID）</ElCheckbox
                ><ElCheckbox
                  v-if="
                    session.workspace === 'lajiaoliao' && form.admission_mode === 'native_debug'
                  "
                  value="send_text"
                  >发送群文本（目标填机器人 UID）</ElCheckbox
                ><ElCheckbox
                  v-if="
                    session.workspace === 'lajiaoliao' && form.admission_mode === 'native_debug'
                  "
                  value="recall_message"
                  >撤回消息（目标填消息发送人 UID）</ElCheckbox
                ><ElCheckbox
                  v-if="session.workspace === 'wsl' && form.admission_mode === 'package'"
                  value="send_text"
                  >文本（待验收）</ElCheckbox
                ><ElCheckbox
                  v-if="session.workspace === 'wsl' && form.admission_mode === 'package'"
                  value="send_image"
                  >图片（待验收）</ElCheckbox
                ><ElCheckbox
                  v-if="session.workspace === 'wsl' && form.admission_mode === 'package'"
                  value="send_private_text"
                  >私信文本（待验收）</ElCheckbox
                ><ElCheckbox
                  v-if="session.workspace === 'wsl' && form.admission_mode === 'package'"
                  value="send_private_image"
                  >私信图片（待验收）</ElCheckbox
                ><ElCheckbox
                  v-if="session.workspace === 'wsl' && form.admission_mode === 'package'"
                  value="send_voice"
                  >语音（待验收）</ElCheckbox
                ><ElCheckbox
                  v-if="session.workspace === 'wsl' && form.admission_mode === 'package'"
                  value="send_video"
                  >视频（待验收）</ElCheckbox
                ><ElCheckbox
                  v-if="session.workspace === 'wsl' && form.admission_mode === 'package'"
                  value="send_file"
                  >文件（待验收）</ElCheckbox
                ></ElCheckboxGroup
              ></ElFormItem
            >
            <ElFormItem
              :label="
                form.actions.includes('friend_approve')
                  ? '好友申请人 UID'
                  : form.actions.includes('friend_delete')
                    ? '删除目标好友 UID'
                    : '测试目标 UID（一行一个）'
              "
              ><ElInput
                v-model="form.targets"
                :aria-label="
                  form.actions.includes('friend_approve')
                    ? '好友申请人 UID'
                    : form.actions.includes('friend_delete')
                      ? '删除目标好友 UID'
                      : '测试目标 UID'
                "
                :type="
                  form.actions.some((a: string) => ['friend_delete', 'friend_approve'].includes(a))
                    ? 'text'
                    : 'textarea'
                "
            /></ElFormItem>
            <ElFormItem v-if="form.actions.includes('friend_approve')" label="好友申请编号">
              <ElInput
                v-model="form.friend_application_id"
                aria-label="好友申请编号"
                maxlength="200"
              />
            </ElFormItem>
            <ElFormItem>
              <ElCheckbox v-model="form.confirm_isolated"
                >确认所选群为隔离测试群，并将其标记为测试用途</ElCheckbox
              >
            </ElFormItem>
            <div class="form-row"
              ><ElFormItem label="有效期 / 秒"
                ><ElInputNumber v-model="form.seconds" :min="1" :max="300" /></ElFormItem
              ><ElFormItem label="动作次数上限"
                ><ElInputNumber
                  v-model="form.budget"
                  :min="1"
                  :max="
                    form.actions.some((a: string) =>
                      ['friend_delete', 'friend_approve'].includes(a)
                    )
                      ? 1
                      : 10
                  "
                  :disabled="
                    form.actions.some((a: string) =>
                      ['friend_delete', 'friend_approve'].includes(a)
                    )
                  " /></ElFormItem
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
      :panels="session.panels"
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
    <GroupAssignment
      v-if="isAdmin && assignmentGroup"
      :key="`${session.workspace}:${assignmentGroup.group_id || assignmentGroup.id}`"
      :group="assignmentGroup"
      :workspace="session.workspace"
      @close="assignmentGroup = null"
      @saved="refresh"
    />
    <CustomerInvitationDialog
      v-if="isAdmin && customerInvitationOpen"
      :key="session.user.id"
      :users="data.users"
      @close="customerInvitationOpen = false"
      @created="refresh"
    />
    <TenantCapabilities
      v-if="isAdmin && capabilityTenant"
      :key="`${session.user.id}:${capabilityTenant.tenant_id}`"
      :tenant="capabilityTenant"
      :groups="data.groups"
      @close="capabilityTenant = null"
      @saved="refresh"
    />
    <GroupBusinessRuntime
      v-if="runtimeGroup"
      :key="`${session.user.id}:${runtimeGroup.platform}:${runtimeGroup.group_id}`"
      :group="runtimeGroup"
      @close="runtimeGroup = null"
      @saved="refresh"
    />
    <CustomerBindingReview
      v-if="isAdmin && bindingReview"
      :key="`${session.user.id}:${bindingReview.platform}:${bindingReview.onboarding_challenge_id}:${bindingReview.id}`"
      :claim="bindingReview"
      :tenants="data.users"
      @close="bindingReview = null"
      @reviewed="refresh"
      @manage="openReviewedRuntime"
    />
  </div>
  <WebCaptcha ref="webCaptcha" />
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
  import { ElMessage, ElMessageBox } from 'element-plus'
  import {
    Connection,
    Close,
    Loading,
    Fold,
    Expand,
    Menu,
    MoreFilled,
    Refresh,
    Search,
    Plus,
    ArrowDown,
    ArrowRight,
    Monitor,
    User,
    Tickets,
    ChatDotRound,
    ChatLineRound,
    Clock,
    Setting,
    Document,
    Bell,
    Coin,
    Present,
    Checked,
    Key,
    Lock,
    Delete,
    Link
  } from '@element-plus/icons-vue'
  import { api as request, ApiError, errorText, type Row } from './api'
  import { accountOptionLabel } from './accountIdentity'
  import { useSession } from './store'
  import NativeDeployment from './NativeDeployment.vue'
  import NativeLogin from './AccountLogin.vue'
  import MaiBotConsole from './MaiBotConsole.vue'
  import SystemMonitor from './SystemMonitor.vue'
  import GroupAssignment from './GroupAssignment.vue'
  import AccountRemoval from './AccountRemoval.vue'
  import AccountDashboard from './AccountDashboard.vue'
  import FeatureCenter from './FeatureCenter.vue'
  import BusinessModuleDialog from './BusinessModuleDialog.vue'
  import MessageCenter from './MessageCenter.vue'
  import TestTools from './TestTools.vue'
  import AssistantDrawer from './assistant/AssistantDrawer.vue'
  import AdminMark from './AdminMark.vue'
  import WorkspaceSwitcher from './WorkspaceSwitcher.vue'
  import UserCenter from './UserCenter.vue'
  import TenantMembers from './TenantMembers.vue'
  import BillingCenter from './BillingCenter.vue'
  import AIGateway from './AIGateway.vue'
  import RobotWorkbench from './RobotWorkbench.vue'
  import WorkspaceInsights from './WorkspaceInsights.vue'
  import AssistantGovernance from './assistant/AssistantGovernance.vue'
  import CustomerInvitation from './CustomerInvitation.vue'
  import CustomerInvitationDialog from './CustomerInvitationDialog.vue'
  import TenantCapabilities from './TenantCapabilities.vue'
  import GroupBusinessRuntime from './GroupBusinessRuntime.vue'
  import CustomerBindingReview from './CustomerBindingReview.vue'
  import WebCaptcha from './WebCaptcha.vue'
  import { workspaceNames, type Console, type Workspace } from './workspaces'
  import {
    controlledOnboardingActive,
    controlledOnboardingSelectionKey,
    type CustomerPlatform,
    type ControlledOnboardingScope
  } from './customer'

  const session = useSession()
  const invitationToken = ref(''),
    invitationVisible = ref(false)
  function readCustomerInvitation() {
    if (!location.hash.startsWith('#customer-invitation')) return false
    const token = new URLSearchParams(location.hash.split('?')[1] || '').get('token') || ''
    invitationToken.value = /^[A-Za-z0-9_-]{43}$/.test(token) ? token : ''
    invitationVisible.value = true
    history.replaceState(history.state, '', `${location.pathname}${location.search}#overview`)
    return true
  }
  readCustomerInvitation()
  function closeCustomerInvitation() {
    invitationVisible.value = false
    invitationToken.value = ''
  }
  async function finishCustomerInvitation() {
    closeCustomerInvitation()
    if (session.user) {
      await session.loadPanels()
      session.selectConsole('user')
      navigate('overview')
      await refresh()
    }
  }
  const api = <T,>(path: string, method = 'GET', body?: unknown, signal?: AbortSignal) =>
    request<T>(path, method, body, signal, session.workspace)
  const isMaiBot = computed(() => session.console === 'admin' && session.workspace === 'maibot')
  const isUserCenter = computed(() => session.console === 'user')
  const can = (permission: string) => session.permissions.includes(permission)
  const canManageMembers = computed(
    () =>
      can('members:manage') &&
      (session.user?.role === 'superadmin' || session.user?.tenant_role === 'owner')
  )
  const customerInvitationOpen = ref(false),
    capabilityTenant = ref<Row | null>(null),
    runtimeGroup = ref<Row | null>(null),
    bindingReview = ref<Row | null>(null),
    assistantInitialGroup = ref(''),
    memberTenant = ref('')
  const onboardingRequest = ref('')
  let onboardingSequence = 0
  const accessCatalog = ref<Row>({})
  const featureGroup = ref('')
  const assignmentGroup = ref<Row | null>(null)
  const featureViewScope = computed(
    () =>
      `${session.console}:${session.workspace}:${session.user?.id}:${session.user?.tenant_id || 'platform'}:${session.selectedAccount}:feature-view`
  )
  const featureView = computed<'native' | 'manual' | 'modules'>({
    get: () =>
      isUserCenter.value
        ? 'modules'
        : session.user?.role === 'superadmin' && session.drafts[featureViewScope.value] === 'manual'
          ? 'manual'
          : 'native',
    set: (value) => session.saveDraft(featureViewScope.value, value)
  })
  const featureGroups = computed(() =>
    data.groups.filter(
      (group) =>
        isUserCenter.value ||
        !session.selectedAccount ||
        group.primary_account_id === session.selectedAccount
    )
  )
  const isBusinessDetail = computed(() => ['ledger', 'activities'].includes(page.value))
  const businessScope = computed(
    () =>
      `${session.console}:${session.workspace}:${session.user?.id}:${session.user?.tenant_id || 'platform'}:${session.selectedAccount}:business-group`
  )
  const businessGroup = computed<string>({
    get: () => {
      const id = session.drafts[businessScope.value]
      return typeof id === 'string' && businessGroups.value.some((group) => group.group_id === id)
        ? id
        : ''
    },
    set: (id) => session.saveDraft(businessScope.value, id)
  })
  const businessGroups = computed(() => {
    const groups = new Map(featureGroups.value.map((group) => [group.group_id, group]))
    if (!session.selectedAccount)
      for (const row of [...data.ledger, ...data.activities])
        if (!groups.has(row.group_id))
          groups.set(row.group_id, { group_id: row.group_id, name: row.group_id })
    return [...groups.values()]
  })
  const nativeTarget = ref<Row | null>(null),
    nativeLoginOpen = ref(false)
  const nativePreparing = ref('')
  const nativeLogin = ref<InstanceType<typeof NativeLogin>>()
  const removalTarget = ref<Row | null>(null),
    removalOpen = ref(false)
  const credentials = reactive({ username: '', password: '' })
  const webCaptcha = ref<InstanceType<typeof WebCaptcha>>()
  let loginGeneration = 0,
    loginController: AbortController | undefined
  const busy = ref(false),
    loginError = ref(''),
    refreshing = ref(false),
    loadError = ref(''),
    saveError = ref(''),
    saving = ref(false)
  const connectionsOpen = ref(false)
  function mobileCommand(command: string) {
    if (command === 'connections') connectionsOpen.value = true
    else if (command === 'login') void openNativeLogin(null)
    else if (command === 'refresh') void refresh()
    else if (command === 'logout') void logout()
  }
  const sidebarOpen = ref(false),
    sidebarCollapsed = ref(false),
    page = ref('overview'),
    search = ref(''),
    lastRefreshed = ref('')
  const collapsedGroups = reactive<Record<string, boolean>>({})
  const navigationMedia = window.matchMedia('(max-width: 760px)')
  const mobileNavigation = ref(navigationMedia.matches)
  function updateNavigationViewport() {
    mobileNavigation.value = navigationMedia.matches
    if (!mobileNavigation.value) sidebarOpen.value = false
  }
  onMounted(() => navigationMedia.addEventListener('change', updateNavigationViewport))
  onUnmounted(() => navigationMedia.removeEventListener('change', updateNavigationViewport))
  function focusWorkspace() {
    document.getElementById('workspace-content')?.focus()
  }
  async function closeSidebar() {
    sidebarOpen.value = false
    await nextTick()
    document.querySelector<HTMLButtonElement>('.mobile-menu')?.focus()
  }
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
  const testGroupChoices = ref<Row[]>([])
  const isAdmin = computed(() => session.user?.role === 'superadmin')
  const extensionPages = ['ai-learning', 'ai-memory', 'ai-people', 'ai-emoji']
  const accountScopedPages = [
    'overview',
    'accounts',
    'groups',
    'features',
    'assistant',
    'messages',
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
    members: 'members:manage',
    billing: 'overview:read',
    gateway: 'plans:manage',
    overview: 'overview:read',
    accounts: 'accounts:read',
    groups: 'groups:read',
    features: 'groups:read',
    assistant: 'assistant:use',
    'assistant-admin': 'overview:read',
    messages: 'accounts:read',
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
    { key: 'members', label: '成员与权限', icon: User },
    { key: 'billing', label: '套餐与积分', icon: Coin },
    { key: 'gateway', label: 'AI 网关', icon: Connection, admin: true },
    { key: 'groups', label: '群组管理', icon: ChatDotRound },
    { key: 'messages', label: '消息中心', icon: ChatDotRound, admin: true },
    { key: 'features', label: '功能管理', icon: Setting },
    { key: 'assistant', label: '群管助手', icon: ChatDotRound },
    { key: 'assistant-admin', label: '助手审计', icon: Checked, admin: true },
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
    { key: 'ai-plugins', label: '扩展中心', icon: Setting },
    { key: 'ai-logs', label: '引擎操作记录', icon: Document },
    { key: 'ai-backups', label: '备份与迁移', icon: Tickets },
    { key: 'ai-system', label: '引擎与升级', icon: Setting }
  ]
  const businessEntries = computed(() =>
    navigation.filter((entry) => ['ledger', 'activities'].includes(entry.key) && canPage(entry.key))
  )
  const moduleLabels: Record<string, string> = {
    management_assistant: 'Web 群管助手',
    moderation: '群管规则',
    replies: '自动回复',
    schedules: '定时任务',
    rankings: '排行',
    cards: '名片',
    friends: '好友审批',
    activities: '活动',
    finance: '财务',
    ai: '群内热群机器人'
  }
  const visibleNavigation = computed(() =>
    navigation
      .filter(
        (n) =>
          (!n.admin || isAdmin.value) &&
          !extensionPages.includes(n.key) &&
          (!['assistant', 'assistant-admin'].includes(n.key) ||
            ['wsl', 'lajiaoliao'].includes(session.workspace)) &&
          (isUserCenter.value
            ? [
                'overview',
                'groups',
                'members',
                'billing',
                'features',
                'assistant',
                'operations'
              ].includes(n.key)
            : n.key !== 'assistant' && n.key !== 'members' && n.key !== 'billing') &&
          (n.key !== 'members' || canManageMembers.value) &&
          (session.workspace !== 'lajiaoliao' || !['ledger', 'activities'].includes(n.key)) &&
          (n.key !== 'messages' || isAdmin.value) &&
          (n.key === 'features'
            ? can('groups:read') ||
              (session.workspace === 'wsl' && ['activities:read', 'ledger:read'].some(can))
            : can(n.key.startsWith('ai-') ? 'workspaces:maibot' : navigationPermissions[n.key])) &&
          !['ledger', 'activities'].includes(n.key) &&
          (isMaiBot.value
            ? ['overview', 'personas', 'groups', 'messages', 'system-monitor', 'gateway'].includes(
                n.key
              ) || n.key.startsWith('ai-')
            : n.key !== 'personas' && !n.key.startsWith('ai-'))
      )
      .map((n) => {
        if (isUserCenter.value && n.key === 'groups') return { ...n, label: '授权群组' }
        if (isUserCenter.value && n.key === 'features') return { ...n, label: '群功能配置' }
        if (isUserCenter.value && n.key === 'billing') return { ...n, label: '套餐与费用' }
        if (!isUserCenter.value && n.key === 'features') return { ...n, label: '功能准入与协议' }
        return isMaiBot.value && n.key === 'groups' ? { ...n, label: '聊天群组' } : n
      })
  )
  const navigationGroups = computed(() => {
    const definitions = isUserCenter.value
      ? [
          { key: 'personal', label: '我的服务', pages: ['overview', 'groups', 'features'] },
          { key: 'account', label: '组织与用量', pages: ['members', 'operations', 'billing'] }
        ]
      : [
          { key: 'work', label: '工作空间', pages: ['overview', 'groups', 'messages'] },
          {
            key: 'business',
            label: isMaiBot.value ? '聊天服务' : '群管与服务',
            pages: ['features', 'claims', 'personas', 'ai-chat', 'ai-models']
          },
          {
            key: 'extensions',
            label: '可选扩展',
            pages: ['ai-plugins']
          },
          {
            key: 'records',
            label: '记录与分析',
            pages: ['operations', 'audit', 'ai-statistics', 'ai-logs']
          },
          {
            key: 'organization',
            label: '组织与订阅',
            pages: ['members', 'billing', 'users', 'plans']
          },
          {
            key: 'platform',
            label: '平台运维',
            pages: [
              'assistant-admin',
              'gateway',
              'tests',
              'system-monitor',
              'ai-backups',
              'ai-system'
            ]
          }
        ]
    return definitions
      .map((group) => ({
        ...group,
        items: group.pages.flatMap((key) =>
          visibleNavigation.value.filter((item) => item.key === key)
        )
      }))
      .filter((group) => group.items.length)
  })
  function canPage(key: string) {
    if (extensionPages.includes(key))
      return isMaiBot.value && isAdmin.value && can('workspaces:maibot')
    if (key === 'features')
      return (
        !isMaiBot.value &&
        (can('groups:read') ||
          (session.workspace === 'wsl' && ['activities:read', 'ledger:read'].some(can)))
      )
    if (!isMaiBot.value && ['ledger', 'activities'].includes(key))
      return isUserCenter.value && session.workspace === 'wsl' && can(navigationPermissions[key])
    return visibleNavigation.value.some((item) => item.key === key)
  }
  const pageTitle = computed(() =>
    isUserCenter.value && page.value === 'overview'
      ? '用户中心'
      : page.value === 'overview' && session.workspace === 'lajiaoliao'
        ? '辣椒聊工作台'
        : page.value === 'overview' && isMaiBot.value
          ? '热群工作台'
          : isMaiBot.value && page.value === 'groups'
            ? '聊天群组'
            : page.value === 'assistant'
              ? '群管助手'
              : visibleNavigation.value.find((n) => n.key === page.value)?.label ||
                navigation.find((n) => n.key === page.value)?.label ||
                '工作台'
  )
  const selectedAccountName = computed(() =>
    isUserCenter.value
      ? isAdmin.value
        ? '不限额 · 平台承担演示费用'
        : '我的授权群与套餐'
      : !accountScopedPages.includes(page.value)
        ? '平台设置'
        : session.selectedAccount
          ? accountOptionLabel(data.accounts.find((a) => a.id === session.selectedAccount))
          : '全部账号'
  )
  const selectedAccount = computed(() =>
    data.accounts.find((account) => account.id === session.selectedAccount)
  )
  const scopedOperations = computed(() =>
    data.operations.filter(
      (o) => !session.selectedAccount || o.account_id === session.selectedAccount
    )
  )
  const metrics = computed(() => [
    {
      label: isMaiBot.value ? '聊天机器人' : '群管机器人',
      value: overview.value.accounts,
      limit: overview.value.limits?.accounts,
      color: '#5274a3',
      icon: Connection
    },
    {
      label: '已授权群组',
      value: overview.value.groups,
      limit: overview.value.limits?.groups,
      color: '#2b826c',
      icon: ChatDotRound
    },
    {
      label: '热群服务群',
      value: overview.value.ai_groups,
      limit: overview.value.limits?.ai_groups,
      color: '#7c853c',
      icon: ChatLineRound
    },
    {
      label: isMaiBot.value ? '待启用热群' : '待审核申请',
      value: isMaiBot.value ? overview.value.ai_requests : overview.value.pending_claims,
      limit: 0,
      color: '#b5863c',
      icon: Clock
    }
  ])
  const filteredRows = computed(() =>
    (data[page.value] || []).filter((r) => {
      const account = session.selectedAccount
      if (isBusinessDetail.value && businessGroup.value && r.group_id !== businessGroup.value)
        return false
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
          tests: '创建授权窗口',
          ledger: '新增记录',
          activities: '创建活动'
        }[page.value] || ''
      : ''
  )
  let controller: AbortController | null = null
  let businessController: AbortController | undefined
  const businessLoading = ref(false)
  const businessReadScope = computed(() =>
    JSON.stringify([
      businessScope.value,
      session.user?.role,
      page.value,
      session.drafts[businessScope.value] || '',
      session.permissions
    ])
  )
  let refreshGeneration = 0
  let mutationGeneration = 0
  let interval: ReturnType<typeof setInterval> | undefined
  const viewSearch = new Map<string, string>()
  const workspaceViews = new Map<
    string,
    { page: string; data: Record<string, Row[]>; overview: Row; sampledAt: string }
  >()
  function cancelLogin() {
    loginGeneration++
    loginController?.abort()
    webCaptcha.value?.cancel()
    busy.value = false
  }
  watch(
    () => [
      session.user?.id,
      session.user?.tenant_id,
      session.user?.role,
      session.console,
      session.workspace,
      session.selectedAccount,
      invitationVisible.value
    ],
    cancelLogin,
    { flush: 'sync' }
  )
  watch(
    () => [credentials.username, credentials.password],
    () => {
      if (busy.value) cancelLogin()
    },
    { flush: 'sync' }
  )
  watch(
    () => dialog.open,
    (open) => {
      if (!open) webCaptcha.value?.cancel()
    },
    { flush: 'sync' }
  )
  watch(
    () => `${session.console}:${session.workspace}:${page.value}:${session.selectedAccount}`,
    (next, previous) => {
      if (previous) viewSearch.set(previous, search.value)
      search.value = viewSearch.get(next) || ''
    }
  )
  watch(businessReadScope, () => void loadBusinessRows())
  watch(
    () => `${session.console}:${session.workspace}`,
    (next, previous) => {
      workspaceViews.set(previous, {
        page: page.value,
        data: clone(data),
        overview: clone(overview.value),
        sampledAt: lastRefreshed.value
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
      assignmentGroup.value = null
      capabilityTenant.value = null
      runtimeGroup.value = null
      bindingReview.value = null
      customerInvitationOpen.value = false
      assistantInitialGroup.value = ''
      onboardingRequest.value = ''
      featureGroup.value = ''
      memberTenant.value = ''
      saving.value = false
      const stored = workspaceViews.get(next)
      for (const key of Object.keys(data)) data[key] = stored?.data[key] || []
      overview.value = stored?.overview || {}
      lastRefreshed.value = stored?.sampledAt || ''
      page.value =
        stored?.page || (next.split(':')[0] === previous.split(':')[0] ? page.value : 'overview')
      if (!canPage(page.value)) page.value = 'overview'
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
      lastRefreshed.value = ''
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
      capabilityTenant.value = null
      runtimeGroup.value = null
      bindingReview.value = null
      customerInvitationOpen.value = false
      assistantInitialGroup.value = ''
      onboardingRequest.value = ''
      featureGroup.value = ''
      memberTenant.value = ''
    }
  )

  function navigate(key: string) {
    key = resolvePage(key)
    if (!canPage(key)) key = 'overview'
    page.value = key
    sidebarOpen.value = false
    location.hash = key
  }
  function onHashChange() {
    if (readCustomerInvitation()) return
    const original = location.hash.slice(1)
    const hash = resolvePage(original)
    if (hash !== original) {
      navigate(hash)
      return
    }
    if (hash && canPage(hash)) page.value = hash
  }
  function resolvePage(key: string) {
    return key === 'platform-assistant'
      ? canPage('assistant-admin')
        ? 'assistant-admin'
        : 'overview'
      : key
  }
  function openFeatures(group: Row) {
    if (dialog.open) {
      persistDraft()
      dialog.open = false
    }
    featureGroup.value = group.group_id
    featureView.value = 'modules'
    navigate('features')
  }
  function selectFeatureSection(key: string) {
    if (
      key === 'native' ||
      key === 'modules' ||
      (key === 'manual' && session.user?.role === 'superadmin')
    ) {
      if (key === 'modules' && businessGroup.value) featureGroup.value = businessGroup.value
      featureView.value = key
      navigate('features')
    } else navigate(key)
  }
  function openFeatureDetail(key: string, group = '') {
    if (['ledger', 'activities'].includes(key)) {
      businessGroup.value = group
    }
    navigate(key)
  }
  function switchWorkbench(value: Workspace) {
    session.selectWorkspace(value)
    sidebarOpen.value = false
  }
  async function switchAssistantPlatform(value: 'wsl' | 'lajiaoliao') {
    if (
      !isUserCenter.value ||
      !can('assistant:use') ||
      !session.panels.some((panel) => panel.id === value)
    )
      return
    const actor = session.user?.id
    session.selectWorkspace(value)
    await nextTick()
    if (session.user?.id !== actor || !isUserCenter.value || session.workspace !== value) return
    navigate('assistant')
  }
  function switchConsole(value: Console) {
    session.selectConsole(value)
    sidebarOpen.value = false
  }
  async function openControlledOnboarding(scope: ControlledOnboardingScope) {
    const actor = session.user?.id,
      tenant = session.user?.tenant_id
    if (
      !isAdmin.value ||
      scope.actor_id !== actor ||
      !controlledOnboardingActive(scope, Date.now()) ||
      !session.panels.some((panel) => panel.id === scope.platform)
    )
      return
    session.selectConsole('user')
    session.selectWorkspace(scope.platform)
    await nextTick()
    if (
      session.user?.id !== actor ||
      session.user?.tenant_id !== tenant ||
      !isAdmin.value ||
      session.console !== 'user' ||
      session.workspace !== scope.platform ||
      !controlledOnboardingActive(scope, Date.now())
    )
      return
    try {
      sessionStorage.setItem(
        controlledOnboardingSelectionKey(actor!, tenant, scope.platform),
        scope.id
      )
    } catch {
      ElMessage.error('浏览器暂无法保留本轮测试范围，请重新打开接入入口')
      return
    }
    navigate('groups')
    onboardingRequest.value = `${scope.id}:${++onboardingSequence}`
  }
  async function openUserGroup(group: Row) {
    const platform = group.platform || session.workspace
    if (!session.panels.some((entry) => entry.id === platform)) return
    const actor = session.user?.id
    session.selectConsole('user')
    session.selectWorkspace(platform)
    await nextTick()
    if (session.user?.id !== actor || session.console !== 'user' || session.workspace !== platform)
      return
    data.groups = [{ ...group, platform }]
    featureGroup.value = group.group_id || group.id
    featureView.value = 'modules'
    navigate('features')
  }
  async function openUserAssistant(group: Row) {
    const platform = group.platform || session.workspace
    if (
      !can('assistant:use') ||
      !group.active ||
      !session.panels.some((entry) => entry.id === platform)
    )
      return
    const actor = session.user?.id
    session.selectConsole('user')
    session.selectWorkspace(platform)
    await nextTick()
    if (session.user?.id !== actor || session.console !== 'user' || session.workspace !== platform)
      return
    data.groups = [{ ...group, platform }]
    assistantInitialGroup.value = group.group_id || group.id
    navigate('assistant')
  }
  function consumeOnboardingRequest(request: string) {
    if (onboardingRequest.value === request) onboardingRequest.value = ''
  }
  async function openUserManagement(key: string, platform?: CustomerPlatform) {
    if (!isAdmin.value || (platform && !session.panels.some((panel) => panel.id === platform)))
      return
    const actor = session.user?.id
    session.selectConsole('admin')
    if (platform) session.selectWorkspace(platform)
    await nextTick()
    if (
      !isAdmin.value ||
      session.user?.id !== actor ||
      session.console !== 'admin' ||
      (platform && session.workspace !== platform)
    )
      return
    navigate(key)
  }
  async function openTenantMembers(tenant: Row) {
    if (!isAdmin.value || !tenant.tenant_id || !canManageMembers.value) return
    session.selectConsole('user')
    await nextTick()
    memberTenant.value = tenant.tenant_id
    navigate('members')
  }
  async function openNativeLogin(row: Row | null) {
    if (nativePreparing.value) return
    connectionsOpen.value = false
    const workspace = session.workspace,
      owner = session.user?.id,
      selected = session.selectedAccount,
      started = mutationGeneration
    const current = () =>
      workspace === session.workspace &&
      owner === session.user?.id &&
      selected === session.selectedAccount &&
      started === mutationGeneration
    nativePreparing.value = row?.id || '__new__'
    try {
      if (row) {
        const receiver = await request<Row>(
          `/accounts/${row.id}/receiver`,
          'GET',
          undefined,
          undefined,
          workspace
        )
        if (!current()) return
        if (receiver.enabled) {
          await request(
            `/accounts/${row.id}/receiver`,
            'PUT',
            { enabled: false, revision: receiver.revision },
            undefined,
            workspace
          )
        }
      }
      if (!current()) return
      nativeTarget.value = row
      nativeLoginOpen.value = true
    } catch (e) {
      if (current()) ElMessage.error((e as Error).message)
    } finally {
      nativePreparing.value = ''
    }
  }
  function accountCreated(account: Row) {
    session.rememberAccount(account.id, account.workspace || session.workspace)
    void refresh()
  }
  function openAccountRemoval(account: Row) {
    connectionsOpen.value = false
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
  function boundAccountLabel(group: Row, chat = false) {
    const id = chat ? group.ai_account_id : group.primary_account_id
    const account = data.accounts.find((a) => a.id === id)
    return account ? accountOptionLabel(account) : group.account_name || '未分配账号'
  }
  function personaAccountLabel(row: Row) {
    const account = data.accounts.find((a) => a.id === row.account_id)
    return account
      ? accountOptionLabel(account)
      : row.native_nickname || row.account_name || '未绑定机器人'
  }
  const personaAccount = computed(() => data.accounts.find((a) => a.id === form.value.account_id))
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
    if (
      busy.value ||
      session.user ||
      invitationVisible.value ||
      !credentials.username.trim() ||
      !credentials.password
    )
      return
    const frozen = { ...credentials },
      generation = ++loginGeneration
    const active = new AbortController()
    loginController = active
    const current = () =>
      generation === loginGeneration &&
      !active.signal.aborted &&
      !session.user &&
      !invitationVisible.value &&
      credentials.username === frozen.username &&
      credentials.password === frozen.password
    busy.value = true
    loginError.value = ''
    try {
      const ticket = await webCaptcha.value?.open({ purpose: 'login', username: frozen.username })
      if (!ticket || !current()) return
      const result = await request<{ user: any }>(
        '/auth/login',
        'POST',
        { ...frozen, captcha_ticket: ticket },
        active.signal,
        'wsl'
      )
      if (!current()) return
      busy.value = false
      session.setUser(result.user)
      credentials.password = ''
      const sameUser = () => session.user?.id === result.user.id
      await session.restore()
      if (!sameUser()) return
      if (invitationToken.value) invitationVisible.value = true
      await refresh()
    } catch (e) {
      if (current()) loginError.value = (e as Error).message
    } finally {
      if (generation === loginGeneration) busy.value = false
    }
  }
  async function logout() {
    cancelLogin()
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
  async function loadBusinessRows() {
    businessController?.abort()
    const active = new AbortController(),
      scope = businessReadScope.value,
      kind = page.value === 'ledger' ? 'ledger' : page.value === 'activities' ? 'activities' : null,
      workspace = session.workspace
    businessController = active
    const current = () => !active.signal.aborted && scope === businessReadScope.value
    data.ledger = []
    data.activities = []
    businessLoading.value = false
    if (!session.user || !isUserCenter.value || !kind || !canPage(kind)) return
    businessLoading.value = true
    loadError.value = ''
    try {
      const [rows, groups] = await Promise.all([
        request<Row[]>(
          kind === 'ledger' ? '/ledger' : '/activities',
          'GET',
          undefined,
          active.signal,
          workspace
        ),
        can('groups:read')
          ? request<Row[]>('/groups', 'GET', undefined, active.signal, workspace)
          : Promise.resolve(null)
      ])
      if (!current()) return
      if (groups) data.groups = groups
      data[kind] = rows
    } catch (e) {
      if (!current()) return
      if (e instanceof ApiError && e.status === 401) session.clear()
      else loadError.value = (e as Error).message
    } finally {
      if (current()) businessLoading.value = false
    }
  }
  async function refresh() {
    if (!session.user) return
    const owner = session.user.id
    const workspace = session.workspace
    const console = session.console
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
        workspace !== session.workspace ||
        console !== session.console
      )
        return
      session.permissions = me.permissions
      session.user = me.user
      if (me.user.role !== 'superadmin' && session.console !== 'user') {
        session.selectConsole('user')
        return
      }
      await session.loadPanels(signal)
      if (workspace !== session.workspace || console !== session.console) return
      if (console === 'user') {
        overview.value = {}
        if (page.value === 'members' && isAdmin.value && can('users:manage')) {
          const users = await request<Row[]>('/users', 'GET', undefined, signal, 'wsl')
          if (
            generation !== refreshGeneration ||
            owner !== session.user?.id ||
            workspace !== session.workspace ||
            console !== session.console
          )
            return
          data.users = users
        }
        if (isBusinessDetail.value) await loadBusinessRows()
        lastRefreshed.value = new Date().toISOString()
        return
      }
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
          .filter(
            ([key, [, permission]]) =>
              can(permission) &&
              (workspace !== 'lajiaoliao' || !['ledger', 'activities'].includes(key))
          )
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
        workspace !== session.workspace ||
        console !== session.console
      )
        return
      for (const key of Object.keys(data)) if (!(key in endpoints)) data[key] = []
      for (const [key, value] of results) data[key] = value
      overview.value = nextOverview
      if (session.selectedAccount && !data.accounts.some((a) => a.id === session.selectedAccount))
        session.selectAccount('')
      if (!canPage(page.value)) page.value = visibleNavigation.value[0]?.key || 'overview'
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
    webCaptcha.value?.cancel()
    mutationGeneration++
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
      plan: {
        name: '',
        group_limit: 1,
        allowed_platforms: ['wsl'],
        ai_group_limit: 0,
        monthly_ai_tokens: 0,
        monthly_assistant_tokens: 0,
        modules: []
      },
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
        friend_application_id: '',
        actions: ['mute_member', 'unmute_member'],
        admission_mode: 'native_debug',
        confirm_isolated: false,
        seconds: 60,
        budget: 2
      },
      ledger: {
        group_id: businessGroup.value,
        member_id: '',
        kind: 'reward',
        amount: '',
        external_key: crypto.randomUUID(),
        note: ''
      },
      activity: { group_id: businessGroup.value, name: '', closes_at: '', winners: 1 }
    }
    openDialog(kind, createLabel.value, initial[kind])
    if (kind === 'persona' && form.value.account_id) selectPersonaAccount(form.value.account_id)
  }
  function openCreateTest(scope: Row) {
    testGroupChoices.value = (scope.groups || data.groups).filter(
      (group: Row) => !group.platform || group.platform === session.workspace
    )
    openCreate()
    form.value.account_ids = scope.account_id ? [scope.account_id] : []
    form.value.group_id = scope.group_id || ''
    form.value.seconds = 300
  }
  function selectFriendDeleteTest(selected: string | number | boolean) {
    form.value.actions = selected ? ['friend_delete'] : ['mute_member', 'unmute_member']
    form.value.budget = selected ? 1 : 2
    if (selected) {
      form.value.account_ids = form.value.account_ids.slice(0, 1)
      form.value.targets = ''
    }
    form.value.friend_application_id = ''
  }
  function selectFriendApproveTest(selected: string | number | boolean) {
    form.value.actions = selected ? ['friend_approve'] : ['mute_member', 'unmute_member']
    form.value.budget = selected ? 1 : 2
    form.value.friend_application_id = ''
    if (selected) {
      form.value.account_ids = form.value.account_ids.slice(0, 1)
      form.value.targets = ''
    }
  }
  function editGroup(row: Row) {
    openDialog(
      'group',
      row.name,
      {
        version: row.version,
        management_enabled: row.management_enabled,
        ai_enabled: row.ai_enabled,
        heatbot_account_label: row.ai_account_id
          ? data.accounts.find((a) => a.id === row.ai_account_id)
            ? accountOptionLabel(data.accounts.find((a) => a.id === row.ai_account_id))
            : row.ai_account_name || '已分配聊天账号'
          : '未分配聊天账号',
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
      `${row.name} · 群内热群机器人`,
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
      `${personaAccountLabel(row)} · 热群人设`,
      {
        account_id: row.account_id,
        nickname: row.nickname,
        personality: row.personality,
        reply_style: row.reply_style,
        published: row.published,
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
          published: row.published,
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
    if (!isAdmin.value) return
    if (['authenticated_message', 'controlled_test'].includes(row.proof_method)) {
      bindingReview.value = { ...row, platform: row.platform || session.workspace }
      return
    }
    openDialog(
      'review',
      `${row.group_name} · 授权审核`,
      { approve: true, identity_verified: false, note: '' },
      row.id
    )
  }
  function openReviewedRuntime(group: Row) {
    if (
      !isAdmin.value ||
      !bindingReview.value ||
      group.group_id !== bindingReview.value.group_id ||
      group.platform !== bindingReview.value.platform ||
      group.tenant_id !== bindingReview.value.tenant_id ||
      group.primary_account_id !== bindingReview.value.account_id
    )
      return
    bindingReview.value = null
    runtimeGroup.value = { ...group }
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
    if (saving.value || !dialog.open) return
    saving.value = true
    saveError.value = ''
    const owner = session.user?.id
    const workspace = session.workspace
    const tenant = session.user?.tenant_id,
      consoleView = session.console,
      account = session.selectedAccount
    const generation = ++mutationGeneration
    const frozen = { ...dialog },
      input = clone(form.value)
    const current = () =>
      owner === session.user?.id &&
      tenant === session.user?.tenant_id &&
      generation === mutationGeneration &&
      workspace === session.workspace &&
      consoleView === session.console &&
      account === session.selectedAccount &&
      dialog.open &&
      frozen.kind === dialog.kind &&
      frozen.id === dialog.id
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
          if (
            input.actions.includes('friend_approve') &&
            (session.user?.role !== 'superadmin' ||
              input.admission_mode !== 'native_debug' ||
              input.actions.length !== 1 ||
              input.account_ids.length !== 1 ||
              lines(input.targets).length !== 1 ||
              input.budget !== 1 ||
              !input.friend_application_id)
          )
            throw new Error(errorText('native_friend_approve_single_scope_required'))
          if (
            input.actions.includes('friend_delete') &&
            (session.user?.role !== 'superadmin' ||
              input.admission_mode !== 'native_debug' ||
              input.actions.length !== 1 ||
              input.account_ids.length !== 1 ||
              lines(input.targets).length !== 1 ||
              input.budget !== 1)
          )
            throw new Error(errorText('native_friend_delete_single_scope_required'))
          path = '/test-windows'
          body = {
            group_id: input.group_id,
            account_ids: input.account_ids,
            target_ids: lines(input.targets),
            actions: input.actions,
            admission_mode: input.admission_mode,
            confirm_isolated: !!input.confirm_isolated,
            seconds: input.seconds,
            budget: input.budget,
            friend_application_id: input.actions.includes('friend_approve')
              ? input.friend_application_id
              : ''
          }
          break
        case 'ledger':
          path = '/ledger'
          break
        case 'activity':
          path = '/activities'
          break
      }
      let captchaTicket: string | undefined
      if (frozen.kind === 'claim') {
        if (workspace !== 'wsl' && workspace !== 'lajiaoliao')
          throw new ApiError(400, 'invalid_workspace')
        const ticket = await webCaptcha.value?.open({ purpose: 'group_claim', platform: workspace })
        if (!ticket || !current()) return
        captchaTicket = ticket
      }
      await request(path, method, body, undefined, workspace, undefined, captchaTicket)
      if (!current()) return
      if (frozen.scope) session.saveDraft(frozen.scope, {})
      dialog.scope = ''
      dialog.open = false
      ElMessage.success(frozen.kind === 'group-ai' ? '申请已保存，尚未执行拉群或启用' : '已保存')
      await refresh()
    } catch (e) {
      if (!current()) return
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
  async function confirmOwner(row: Row) {
    try {
      await ElMessageBox.confirm(
        `将 ${row.username} 设为此租户主账号？原主账号会改为管理员，相关网站会话将撤销。`,
        '确认主账号',
        { type: 'warning' }
      )
      await api(`/tenants/${row.tenant_id}/owner`, 'POST', {
        user_id: row.id,
        previous_owner_id: row.owner_id || ''
      })
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
    document.title = 'A哥后台 - 机器人'
    window.addEventListener('hashchange', onHashChange)
    try {
      const hash = location.hash.slice(1)
      await session.restore()
      const target = resolvePage(hash)
      if (canPage(target)) page.value = target
      if (location.hash.slice(1) !== page.value) location.hash = page.value
      await refresh()
    } catch {
      /* An unauthenticated browser starts at login. */
    }
    interval = setInterval(() => {
      if (
        !invitationVisible.value &&
        !dialog.open &&
        !document.hidden &&
        page.value !== 'messages' &&
        !(page.value === 'overview' && selectedAccount.value)
      )
        void refresh()
    }, 15000)
  })
  onUnmounted(() => {
    cancelLogin()
    controller?.abort()
    businessController?.abort()
    clearInterval(interval)
    window.removeEventListener('hashchange', onHashChange)
  })
</script>

<style scoped>
  .login-shell {
    grid-template-rows: auto 1fr;
  }
  .console-switch {
    display: flex;
    flex-shrink: 0;
    align-items: center;
    gap: 10px;
    margin: 0 16px 16px;
    padding: 10px 12px;
    border: 1px solid var(--el-border-color-light);
    border-radius: 6px;
    background: var(--el-bg-color);
    color: var(--el-text-color-primary);
    font: inherit;
    font-size: 13px;
    cursor: pointer;
  }
  .console-switch span {
    flex: 1;
    text-align: left;
  }
  .console-switch:hover {
    background: var(--el-color-primary-light-9);
    border-color: var(--el-color-primary-light-5);
  }
  .console-switch:focus-visible {
    outline: 2px solid var(--el-color-primary);
    outline-offset: 2px;
  }
</style>
