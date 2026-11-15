<template>
  <div class="robot-workbench" :aria-busy="loading" :data-workbench="workspace">
    <div class="workbench-actions">
      <ElButton v-if="admin" type="primary" :icon="Plus" @click="emit('login')"
        >添加机器人</ElButton
      >
      <ElButton v-if="can('groups:read')" :icon="ChatDotRound" @click="emit('navigate', 'groups')">
        {{ chat ? '聊天群组' : '群组管理' }}
      </ElButton>
      <ElButton
        v-if="chat && can('personas:read')"
        :icon="User"
        @click="emit('navigate', 'personas')"
        >人设管理</ElButton
      >
      <ElButton
        v-if="!chat && can('groups:read')"
        :icon="Setting"
        @click="emit('navigate', 'features')"
        >原生能力与模块</ElButton
      >
      <ElButton
        v-if="!chat && can('claims:read')"
        :icon="Checked"
        @click="emit('navigate', 'claims')"
        >授权申请</ElButton
      >
    </div>

    <div class="metrics">
      <div v-for="item in metrics" :key="item.label" class="metric">
        <span class="metric-label"
          ><ElIcon :style="{ color: item.color }"><component :is="item.icon" /></ElIcon
          >{{ item.label }}</span
        >
        <strong>{{ item.value ?? '—' }}</strong>
      </div>
    </div>

    <WorkspaceInsights
      :groups="groups"
      :operations="operations"
      :overview="overview"
      :sampled-at="sampledAt"
    />

    <section class="data-section">
      <div class="section-title">
        <h2>{{ chat ? '聊天机器人状态' : '群管机器人状态' }}</h2>
        <ElTag type="info" effect="plain">{{ chat ? '独立聊天账号' : '辣椒聊接入器' }}</ElTag>
      </div>
      <div v-if="!accounts.length" class="workbench-empty">
        <ElEmpty :description="chat ? '暂无聊天机器人' : '暂无辣椒聊机器人'" :image-size="56" />
      </div>
      <div v-else class="robot-list" role="list" aria-label="机器人列表">
        <div v-for="account in accounts" :key="account.id" class="robot-row" role="listitem">
          <button
            class="robot-identity"
            type="button"
            :aria-label="`查看账号 ${account.name}`"
            @click="emit('account', account.id)"
          >
            <img
              class="robot-platform-logo"
              :src="
                account.platform === 'lajiaoliao' ? workspaceLogos.lajiaoliao : workspaceLogos.wsl
              "
              alt=""
            />
            <span
              ><strong>{{ accountOptionLabel(account) }}</strong
              ><small
                >{{ platformName(account.platform) }} ·
                {{ bindings(account.id) }} 个列表内授权群</small
              ></span
            >
            <ElIcon class="robot-chevron"><ArrowRight /></ElIcon>
          </button>
          <div class="robot-state">
            <ElTag :type="account.auth_state === 'authenticated' ? 'success' : 'info'">{{
              account.auth_state === 'authenticated' ? '已登录' : '未登录'
            }}</ElTag>
            <span>{{ connectionLabel(account.runtime_state) }}</span>
            <ElButton
              v-if="admin"
              text
              :icon="Key"
              :loading="preparing === account.id"
              :disabled="!!preparing"
              :aria-label="`登录 ${account.name}`"
              @click="emit('reauth', account)"
              >登录</ElButton
            >
          </div>
        </div>
      </div>
    </section>

    <div class="workbench-sections">
      <section class="workbench-section">
        <div class="section-title"
          ><h2>{{ chat ? '热群服务' : '接入与运行' }}</h2
          ><ElTag type="warning" effect="plain">待验收</ElTag></div
        >
        <dl class="workbench-facts">
          <div><dt>原生接入</dt><dd>未完成验收</dd></div>
          <template v-if="chat">
            <div><dt>自动发言</dt><dd>未开放</dd></div>
            <div
              ><dt>已配置人设</dt><dd>{{ personas.length }}</dd></div
            >
            <div
              ><dt>已发布人设</dt><dd>{{ personas.filter((p) => p.published).length }}</dd></div
            >
            <div><dt>模型用量与延迟</dt><dd>未采集</dd></div>
            <div><dt>模型调度上限</dt><dd>全局 4 / 每群 1</dd></div>
            <div><dt>群管 / 财务执行</dt><dd>无权限</dd></div>
          </template>
          <template v-else>
            <div
              ><dt>连接记录</dt><dd>{{ recordedConnections }} / {{ accounts.length }}</dd></div
            >
            <div
              ><dt>已保存群管启用配置</dt
              ><dd>{{ groups.filter((g) => g.active && g.management_enabled).length }}</dd></div
            >
            <div><dt>收包 / 解码 / 堵塞</dt><dd>按账号查看</dd></div>
            <div><dt>确认方式</dt><dd>序号持久化，无网络 ACK</dd></div>
            <div><dt>账号资料</dt><dd>昵称 / 头像 · 人工操作</dd></div>
          </template>
          <div
            ><dt>数据更新</dt><dd>{{ date(sampledAt) }}</dd></div
          >
        </dl>
        <div v-if="chat && admin" class="workbench-links">
          <ElButton link type="primary" :icon="Setting" @click="emit('navigate', 'ai-chat')"
            >聊天配置</ElButton
          >
          <ElButton link type="primary" :icon="Connection" @click="emit('navigate', 'ai-models')"
            >模型服务</ElButton
          >
          <ElButton link type="primary" :icon="Document" @click="emit('navigate', 'ai-statistics')"
            >能力验收</ElButton
          >
        </div>
      </section>
      <section class="workbench-section">
        <div class="section-title"
          ><h2>{{ chat ? '跨平台群分配' : '授权群组' }}</h2>
          <ElButton
            v-if="can('groups:read')"
            link
            type="primary"
            :icon="ArrowRight"
            @click="emit('navigate', 'groups')"
            >全部群组</ElButton
          >
        </div>
        <ElEmpty v-if="!activeGroups.length" description="暂无有效授权群组" :image-size="48" />
        <ul v-else class="workbench-groups">
          <li v-for="group in activeGroups.slice(0, 6)" :key="group.group_id">
            <div
              ><strong>{{ group.name }}</strong
              ><small
                >{{ platformName(group.platform) }} ·
                {{ group.account_name || '未分配账号' }}</small
              ></div
            >
            <ElTag :type="group.ai_enabled ? 'success' : 'info'">{{
              chat ? aiLabel(group) : group.management_enabled ? '配置已启用' : '配置已停用'
            }}</ElTag>
          </li>
        </ul>
        <p class="workbench-note">{{
          chat
            ? '两平台共享聊天池名额；仅绑定同平台已授权群。'
            : '群目录不等于有效授权；停用功能不释放账号绑定名额。'
        }}</p>
      </section>
    </div>
    <div class="workbench-limits" aria-label="准入策略">
      <span>{{ chat ? '聊天池' : '群管池' }}：两平台共用 10 个账号</span>
      <span>每账号最多 30 群</span>
      <span>全平台最多 300 个管理授权群</span>
      <span>准入上限，非实测容量</span>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import { ElButton, ElEmpty, ElIcon, ElTag } from 'element-plus'
  import {
    ArrowRight,
    ChatDotRound,
    Checked,
    Connection,
    Document,
    Key,
    Plus,
    Setting,
    User
  } from '@element-plus/icons-vue'
  import type { Row } from './api'
  import { accountOptionLabel } from './accountIdentity'
  import { workspaceLogos } from './workspaces'
  import WorkspaceInsights from './WorkspaceInsights.vue'
  const props = defineProps<{
    workspace: 'lajiaoliao' | 'maibot'
    accounts: Row[]
    groups: Row[]
    operations?: Row[]
    personas: Row[]
    overview: Row
    permissions: string[]
    admin: boolean
    loading: boolean
    preparing: string
    sampledAt: string
  }>()
  const emit = defineEmits<{
    navigate: [page: string]
    account: [id: string]
    login: []
    reauth: [account: Row]
  }>()
  const chat = computed(() => props.workspace === 'maibot')
  const activeGroups = computed(() => props.groups.filter((g) => g.active))
  const recordedConnections = computed(
    () => props.accounts.filter((a) => a.runtime_state === 'connected').length
  )
  const can = (permission: string) => props.permissions.includes(permission)
  const platformName = (value: string) =>
    ({ wsl: '旺商聊', lajiaoliao: '辣椒聊' })[value] || '平台未提供'
  const date = (value: string) =>
    value
      ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
      : '尚未同步'
  const connectionLabel = (value: string) =>
    ({
      connected: '连接记录：已连接',
      reconnecting: '连接记录：重连中',
      connecting: '连接记录：连接中',
      disconnected: '连接记录：未连接'
    })[value] || '连接记录：未采集'
  const bindings = (id: string) =>
    activeGroups.value.filter((g) => (chat.value ? g.ai_account_id : g.primary_account_id) === id)
      .length
  const aiLabel = (group: Row) =>
    group.ai_enabled ? '已启用' : group.ai_requested ? '待验收 / 未启用' : '未申请'
  const metrics = computed(() => [
    {
      label: chat.value ? '聊天机器人' : '辣椒聊机器人',
      value: props.overview.accounts,
      color: '#5274a3',
      icon: Connection
    },
    {
      label: chat.value ? '热群关联群' : '已授权群组',
      value: props.overview.groups,
      color: '#2b826c',
      icon: ChatDotRound
    },
    {
      label: chat.value ? '已启用聊天群' : '热群服务群',
      value: props.overview.ai_groups,
      color: '#7c853c',
      icon: User
    },
    {
      label: chat.value ? '待启用聊天群' : '待审核申请',
      value: chat.value ? props.overview.ai_requests : props.overview.pending_claims,
      color: '#b5863c',
      icon: Checked
    }
  ])
</script>

<style scoped>
  .workbench-actions,
  .workbench-links {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .workbench-actions {
    margin-bottom: 20px;
  }
  .workbench-actions .el-button,
  .workbench-links .el-button {
    margin: 0;
  }
  .robot-list {
    padding: 0 22px;
  }
  .robot-row {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 14px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .robot-row:last-child {
    border-bottom: 0;
  }
  .robot-identity {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    cursor: pointer;
    text-align: left;
    font: inherit;
  }
  .robot-platform-logo {
    flex-shrink: 0;
    width: 32px;
    height: 32px;
    object-fit: contain;
  }
  .robot-identity > span {
    min-width: 0;
  }
  .robot-identity strong,
  .workbench-groups strong {
    display: block;
    font-size: 14px;
    font-weight: 500;
    overflow-wrap: anywhere;
  }
  .robot-identity small,
  .workbench-groups small {
    display: block;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    margin-top: 4px;
    overflow-wrap: anywhere;
  }
  .robot-chevron {
    color: var(--el-text-color-placeholder);
    font-size: 14px;
    flex-shrink: 0;
  }
  .robot-state {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .workbench-sections {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 24px;
    margin: 24px 0;
  }
  .workbench-section {
    min-width: 0;
    border-top: 1px solid var(--el-border-color-light);
  }
  .workbench-section .section-title {
    padding: 18px 0;
  }
  .workbench-facts {
    margin: 0 0 14px;
    font-size: 13px;
  }
  .workbench-facts > div {
    display: flex;
    gap: 18px;
    justify-content: space-between;
    padding: 10px 0;
  }
  .workbench-facts dt {
    color: var(--el-text-color-secondary);
  }
  .workbench-facts dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
  }
  .workbench-groups {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .workbench-groups li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .workbench-groups li > div {
    min-width: 0;
  }
  .workbench-note,
  .workbench-limits {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    line-height: 1.7;
  }
  .workbench-note {
    margin: 16px 0 0;
  }
  .workbench-limits {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 22px;
    border-top: 1px solid var(--el-border-color-light);
    padding-top: 14px;
  }
  @media (max-width: 760px) {
    .workbench-sections {
      grid-template-columns: 1fr;
      gap: 14px;
    }
    .robot-row {
      align-items: flex-start;
      flex-direction: column;
      gap: 10px;
    }
    .robot-identity {
      width: 100%;
    }
    .robot-state {
      width: 100%;
      flex-wrap: wrap;
    }
    .robot-state .el-button {
      margin-left: auto;
    }
    .robot-list {
      padding: 0 14px;
    }
    .workbench-actions .el-button {
      font-size: 12px;
      padding: 8px 10px;
    }
  }
</style>
