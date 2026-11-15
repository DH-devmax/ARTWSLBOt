<template>
  <section class="user-center">
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSkeleton v-if="loading && !result" :rows="6" animated />
    <template v-if="result">
      <section v-if="view === 'overview'" class="uc-start" aria-label="工作台接入指引">
        <div class="uc-start-heading">
          <div class="uc-start-copy">
            <div class="uc-start-platform">
              <img
                :src="groupPlatform === 'lajiaoliao' ? '/lajiaoliao-logo.png' : '/wsl-logo.png'"
                alt=""
              />
              <span>{{ platformName(groupPlatform) }} · {{ workspace?.name || '我的工作台' }}</span>
            </div>
            <h2>{{ startState.title }}</h2>
            <p>{{ startState.description }}</p>
          </div>
          <div class="uc-start-actions">
            <ElButton
              v-if="canOnboardPlatform"
              type="primary"
              :icon="Plus"
              @click="openOnboarding()"
              >{{ isDemo ? '绑定群组' : '接入新群组' }}</ElButton
            >
            <ElButton
              v-else-if="isOwner && otherGroupPlatform"
              :icon="Switch"
              @click="groupPlatform = otherGroupPlatform"
              >切换到{{ platformName(otherGroupPlatform) }}</ElButton
            >
            <ElButton
              v-if="permissions.includes('groups:read')"
              :icon="ArrowRight"
              @click="$emit('view', 'groups')"
              >查看授权群</ElButton
            >
            <ElButton
              v-if="isDemo && permissions.includes('users:manage')"
              text
              :icon="User"
              @click="$emit('invite')"
              >邀请客户</ElButton
            >
          </div>
        </div>
        <ol v-if="canOnboardPlatform || isDemo" class="uc-start-steps">
          <li>
            <span>01</span>
            <div><strong>准备群聊</strong><p>在 App 中创建或选择自己的群</p></div>
          </li>
          <li>
            <span>02</span>
            <div><strong>邀请机器人</strong><p>将对应平台的机器人加入群聊</p></div>
          </li>
          <li>
            <span>03</span>
            <div>
              <strong>核验并绑定</strong>
              <p>完成群归属核验，确认接入</p>
            </div>
          </li>
        </ol>
      </section>
      <ElAlert
        v-if="!planActive"
        :title="
          subscription
            ? '套餐未生效或已到期，暂不可申请授权或修改热群配置'
            : '尚未开通套餐，请联系平台管理员'
        "
        type="warning"
        :closable="false"
      />
      <div v-if="view === 'overview' || view === 'usage'" class="uc-summary">
        <div
          ><span>当前套餐</span><strong>{{ plan?.name || '未开通' }}</strong>
          <ElTag :type="planActive ? 'success' : 'warning'">{{
            unlimited ? '不限额' : planActive ? '有效' : '未生效 / 已到期'
          }}</ElTag></div
        >
        <div
          ><span>已授权群</span
          ><strong
            >{{
              result.quotas?.groups?.used ??
              result.active_group_count ??
              result.group_count ??
              result.groups?.length ??
              totalGroups
            }}
            / {{ quotaLimit('group') }}</strong
          ></div
        >
        <div
          ><span>助手调用</span
          ><strong
            >{{ usage?.call_count ?? usage?.calls ?? '—' }} / {{ quotaLimit('assistant') }}</strong
          ></div
        >
        <div
          ><span>AI 可用积分</span
          ><strong>{{
            unlimited
              ? '不限额'
              : result.credit_wallet
                ? (result.credit_wallet.available_micro / 1000000).toLocaleString('zh-CN', {
                    maximumFractionDigits: 6
                  })
                : '—'
          }}</strong>
          <small>{{
            isDemo
              ? '平台承担费用，本人演示与后台用量分别统计'
              : result.credit_wallet
                ? `冻结 ${(result.credit_wallet.held_micro / 1000000).toLocaleString()} 积分`
                : '明细见套餐与积分'
          }}</small></div
        >
      </div>
      <dl v-if="view === 'usage'" class="uc-package">
        <dt>工作空间</dt><dd>{{ workspace?.name || '当前工作空间' }}</dd> <dt>有效期</dt
        ><dd>{{
          plan?.permanent || isDemo
            ? '永久有效'
            : date(plan?.expires_at || subscription?.expires_at)
        }}</dd>
        <dt>可用平台</dt><dd>{{ availablePlatforms.map(platformName).join('、') || '无' }}</dd>
        <dt>功能模块</dt
        ><dd>{{
          isDemo
            ? '群功能配置、群管助手与单次群管操作'
            : (plan?.modules || subscription?.modules || [])
                .map((key: string) => modules[key] || key)
                .join('、') || '无'
        }}</dd>
        <dt>成员额度</dt><dd>{{ quotaLimit('member') }}</dd> <dt>费用来源</dt
        ><dd>{{ billingLabel }}</dd> <dt>操作权限</dt
        ><dd class="uc-grants"
          ><ElTag v-for="item in availableActions" :key="item.key" type="info" size="small">{{
            item.name
          }}</ElTag
          ><span v-if="!availableActions.length">无</span></dd
        >
        <dt>页面同步</dt><dd>{{ date(sampledAt) }}</dd>
      </dl>
      <section v-if="view === 'usage'" class="uc-usage" aria-label="我的实际用量">
        <div class="uc-toolbar"
          ><h2>{{ isDemo ? '演示空间用量' : '我的使用量' }}</h2>
          <ElButton
            text
            :icon="Refresh"
            :loading="usageLoading"
            aria-label="刷新实际用量"
            @click="loadUsage"
          />
        </div>
        <ElAlert v-if="usageError" :title="usageError" type="warning" :closable="false" />
        <section aria-label="本人操作用量">
          <h3 v-if="isDemo">本人操作</h3>
          <dl class="uc-usage-values">
            <div
              ><dt>调用次数</dt><dd>{{ usage?.call_count ?? usage?.calls ?? '—' }}</dd></div
            >
            <div
              ><dt>实际费用</dt
              ><dd>{{
                money(
                  usage?.actual_micro ?? usage?.actual_cost_micro_usd ?? usage?.actual_cost_micro
                )
              }}</dd></div
            >
            <div
              ><dt>冻结费用</dt
              ><dd>{{
                money(
                  usage?.held_micro ??
                    usage?.held_cost_micro_usd ??
                    usage?.frozen_cost_micro ??
                    usage?.held_cost_micro
                )
              }}</dd></div
            >
          </dl>
          <p class="uc-list-count"
            >{{ usage?.period_label || usage?.month || '当前统计周期' }} ·
            {{ date(usage?.sampled_at) }}</p
          >
          <p class="uc-usage-source">{{
            billingSourceLabel(usage?.billing_source || billingSource)
          }}</p>
        </section>
        <section v-if="isDemo" class="uc-background-usage" aria-label="演示空间后台用量">
          <h3>演示空间后台用量</h3>
          <template v-if="backgroundUsage">
            <dl class="uc-usage-values">
              <div
                ><dt>调用次数</dt><dd>{{ backgroundUsage.call_count ?? '—' }}</dd></div
              >
              <div
                ><dt>实际费用</dt
                ><dd>{{ money(backgroundUsage.actual_micro, backgroundUsage) }}</dd></div
              >
              <div
                ><dt>冻结费用</dt
                ><dd>{{ money(backgroundUsage.held_micro, backgroundUsage) }}</dd></div
              >
              <div
                ><dt>已结算调用</dt><dd>{{ backgroundUsage.settled_call_count ?? '—' }}</dd></div
              >
              <div
                ><dt>待结算调用</dt><dd>{{ backgroundUsage.pending_call_count ?? '—' }}</dd></div
              >
              <div
                ><dt>额度</dt><dd>{{ backgroundUsage.unlimited ? '不限额' : '—' }}</dd></div
              >
            </dl>
            <p class="uc-usage-source">{{ billingSourceLabel(backgroundUsage.billing_source) }}</p>
          </template>
          <p v-else class="uc-list-count">暂无演示空间后台用量记录</p>
          <p class="uc-list-count">本人操作与空间后台分别统计，旧记录保持原归属，不合并计算。</p>
        </section>
      </section>
      <section v-if="view === 'overview' || view === 'groups'" class="uc-group-section">
        <div class="uc-toolbar">
          <h2>{{ isSuperadmin && groupWorkspace === 'all' ? '全部授权群' : '我的群组' }}</h2>
          <ElButton v-if="canOnboardPlatform" :icon="Plus" type="primary" @click="openOnboarding()"
            >接入群组</ElButton
          >
          <ElButton :icon="Refresh" :loading="loading" aria-label="刷新用户中心" @click="load" />
        </div>
        <ElRadioGroup
          v-if="isSuperadmin"
          v-model="groupWorkspace"
          class="uc-workspace-filter"
          aria-label="群组工作空间"
        >
          <ElRadioButton value="mine">我的工作空间</ElRadioButton>
          <ElRadioButton value="all">全部工作空间</ElRadioButton>
        </ElRadioGroup>
        <div
          class="uc-filters"
          :class="{ 'uc-filters-all': isSuperadmin && groupWorkspace === 'all' }"
        >
          <ElInput
            v-model="groupSearch"
            :prefix-icon="Search"
            clearable
            placeholder="搜索群组"
            aria-label="搜索我的群组"
          />
          <ElSelect v-model="groupPlatform" aria-label="筛选群组平台">
            <ElOption
              v-for="value in groupPlatforms"
              :key="value"
              :label="platformName(value)"
              :value="value"
            />
          </ElSelect>
          <ElSelect
            v-if="isSuperadmin && groupWorkspace === 'all'"
            v-model="groupCustomer"
            clearable
            filterable
            placeholder="全部客户"
            aria-label="筛选所属客户"
          >
            <ElOption
              v-for="customer in customers"
              :key="customer.id || customer.tenant_id"
              :value="customer.id || customer.tenant_id"
              :label="customer.name || customer.customer_name"
            />
          </ElSelect>
        </div>
        <p class="uc-list-count"
          >{{ totalGroups }} 个群组<span v-if="isSuperadmin">
            ·
            {{
              groupWorkspace === 'mine' ? workspace?.name || '我的工作空间' : '全部工作空间'
            }}</span
          ><span v-else-if="!isOwner"> · 仅显示分配给我的群</span></p
        >
        <ElAlert v-if="groupsError" :title="groupsError" type="error" :closable="false" />
        <section
          v-if="!loading && !groupsLoading && !error && !groupsError && !groups.length"
          class="uc-empty"
          aria-label="群组接入指引"
        >
          <div class="uc-empty-intro">
            <ElIcon class="uc-empty-symbol"><ChatDotRound /></ElIcon>
            <div class="uc-empty-copy">
              <h3>{{ emptyGroupState.title }}</h3>
              <p>{{ emptyGroupState.description }}</p>
              <div class="uc-empty-actions">
                <ElButton v-if="hasGroupFilters" :icon="Refresh" @click="clearGroupFilters"
                  >清除筛选</ElButton
                >
                <ElButton
                  v-else-if="canOnboardPlatform"
                  type="primary"
                  :icon="Plus"
                  @click="openOnboarding()"
                  >接入第一个群组</ElButton
                >
                <ElButton
                  v-else-if="isOwner && otherGroupPlatform"
                  @click="groupPlatform = otherGroupPlatform"
                  >切换到{{ platformName(otherGroupPlatform) }}</ElButton
                >
              </div>
            </div>
          </div>
          <ol v-if="!hasGroupFilters && canOnboardPlatform" class="uc-empty-steps">
            <li
              ><span>1</span
              ><div
                ><strong>准备群聊</strong
                ><p>在{{ platformName(groupPlatform) }} App 中创建或选好自己的群。</p></div
              ></li
            >
            <li
              ><span>2</span
              ><div
                ><strong>加入机器人</strong><p>开始接入后，将分配给你的机器人邀请入群。</p></div
              ></li
            >
            <li
              ><span>3</span
              ><div
                ><strong>核验并绑定</strong><p>在群内完成身份核验，确认后群组将出现在这里。</p></div
              ></li
            >
          </ol>
        </section>
        <ElTable
          v-if="groups.length || groupsLoading"
          v-loading="groupsLoading"
          :data="groups"
          empty-text="暂无匹配的授权群组"
          class="uc-groups"
        >
          <ElTableColumn label="群组" min-width="160"
            ><template #default="{ row }"
              ><strong class="uc-group-name">{{ row.name }}</strong
              ><small class="uc-robot">{{ robotLabel(row) }}</small></template
            ></ElTableColumn
          >
          <ElTableColumn label="平台" min-width="95"
            ><template #default="{ row }">{{ platformName(row.platform) }}</template></ElTableColumn
          >
          <ElTableColumn label="所属工作空间" min-width="130">
            <template #default="{ row }">{{
              row.workspace?.name || row.customer_name || row.tenant_name || '—'
            }}</template>
          </ElTableColumn>
          <ElTableColumn label="授权" min-width="80"
            ><template #default="{ row }"
              ><ElTag :type="row.active ? 'success' : 'info'">{{
                row.active ? '有效' : '已撤销'
              }}</ElTag
              ><template v-if="canTestClaim(row) && row.demo_claim?.state === 'active'"
                ><ElTag class="uc-demo-verified" type="success" size="small">已核验</ElTag
                ><small class="uc-robot"
                  >到期 {{ date(row.demo_claim.expires_at) }}</small
                ></template
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="群业务" min-width="110"
            ><template #default="{ row }"
              ><ElTag
                :type="(row.business_enabled ?? row.business_runtime_enabled) ? 'success' : 'info'"
                effect="plain"
                >{{
                  typeof (row.business_enabled ?? row.business_runtime_enabled) === 'boolean'
                    ? (row.business_enabled ?? row.business_runtime_enabled)
                      ? '已启用'
                      : '未启用'
                    : '待查询'
                }}</ElTag
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="热群状态" min-width="140"
            ><template #default="{ row }">{{
              row.ai_enabled ? '已启用' : row.ai_requested ? '待验收 / 未启用' : '未申请'
            }}</template></ElTableColumn
          >
          <ElTableColumn label="操作" min-width="320"
            ><template #default="{ row }">
              <ElTooltip
                :content="canUseAssistant(row) ? '进入本群对话' : '需要 PRO 套餐及本群助手权限'"
                ><span
                  ><ElButton
                    link
                    type="primary"
                    :disabled="!canUseAssistant(row)"
                    @click="$emit('assistant', row)"
                    ><span class="uc-assistant-icon"><AdminMark /></span>群管助手</ElButton
                  ></span
                ></ElTooltip
              >
              <ElButton
                v-if="permissions.includes('groups:read')"
                link
                type="primary"
                :disabled="!row.active"
                @click="actionGroup = row"
                >单次群管</ElButton
              >
              <ElButton
                v-if="permissions.includes('groups:read')"
                link
                type="primary"
                :disabled="!canOpenGroup(row)"
                @click="$emit('group', row)"
                >群功能</ElButton
              >
              <ElButton
                v-if="permissions.includes('groups:read')"
                link
                type="primary"
                :disabled="!row.active"
                @click="runtimeGroup = row"
                >运行与权限</ElButton
              >
              <ElButton
                v-if="permissions.includes('groups:ai')"
                link
                type="primary"
                :disabled="!canConfigureAI(row)"
                @click="edit(row)"
                >热群配置</ElButton
              >
              <ElButton
                v-if="canReverify(row)"
                link
                type="primary"
                :icon="Refresh"
                @click="reverify(row)"
                >重新核验</ElButton
              >
              <ElButton
                v-if="canTestClaim(row)"
                link
                type="primary"
                :icon="CircleCheck"
                @click="openDemoClaim(row)"
                >测试认领</ElButton
              >
            </template></ElTableColumn
          >
        </ElTable>
        <div v-if="groups.length" class="uc-mobile-groups" aria-label="我的群组列表">
          <div v-for="group in groups" :key="group.group_id" class="uc-group-row">
            <div class="uc-group-heading"
              ><strong>{{ group.name }}</strong
              ><ElTag :type="group.active ? 'success' : 'info'" size="small">{{
                group.active ? '有效' : '已撤销'
              }}</ElTag></div
            >
            <p
              >{{ platformName(group.platform) }} · {{ robotLabel(group) }} ·
              {{
                group.ai_enabled
                  ? '热群已启用'
                  : group.ai_requested
                    ? '热群待验收 / 未启用'
                    : '未申请热群'
              }}</p
            >
            <p
              >所属工作空间：{{
                group.workspace?.name || group.customer_name || group.tenant_name || '—'
              }}</p
            >
            <p v-if="canTestClaim(group) && group.demo_claim?.state === 'active'">
              <ElTag type="success" size="small">已核验</ElTag>
              到期 {{ date(group.demo_claim.expires_at) }}
            </p>
            <p
              v-if="
                group.customer_blocked_reason ||
                group.business_blocked_reason ||
                group.blocked_reason
              "
              >自动业务：{{
                customerReason(
                  group.customer_blocked_reason ||
                    group.business_blocked_reason ||
                    group.blocked_reason
                )
              }}</p
            >
            <div class="uc-group-actions">
              <ElButton
                v-if="permissions.includes('groups:read')"
                link
                type="primary"
                :disabled="!group.active"
                @click="actionGroup = group"
                >单次群管</ElButton
              >
              <ElButton
                link
                type="primary"
                :disabled="!canUseAssistant(group)"
                @click="$emit('assistant', group)"
                ><span class="uc-assistant-icon"><AdminMark /></span>群管助手</ElButton
              >
              <ElButton
                v-if="permissions.includes('groups:read')"
                link
                type="primary"
                :icon="Setting"
                :disabled="!canOpenGroup(group)"
                @click="$emit('group', group)"
                >群功能</ElButton
              >
              <ElButton
                v-if="permissions.includes('groups:read')"
                link
                type="primary"
                :disabled="!group.active"
                :icon="Setting"
                @click="runtimeGroup = group"
                >运行与权限</ElButton
              >
              <ElButton
                v-if="permissions.includes('groups:ai')"
                link
                type="primary"
                :icon="ChatDotRound"
                :disabled="!canConfigureAI(group)"
                @click="edit(group)"
                >热群配置</ElButton
              >
              <ElButton
                v-if="canReverify(group)"
                link
                type="primary"
                :icon="Refresh"
                @click="reverify(group)"
                >重新核验</ElButton
              >
              <ElButton
                v-if="canTestClaim(group)"
                link
                type="primary"
                :icon="CircleCheck"
                @click="openDemoClaim(group)"
                >测试认领</ElButton
              >
            </div>
          </div>
        </div>
        <ElPagination
          v-if="totalGroups > pageSize"
          v-model:current-page="groupPage"
          :page-size="pageSize"
          :total="totalGroups"
          layout="prev, pager, next"
          small
          class="uc-pagination"
        />
      </section>
      <section
        v-if="view === 'overview' && permissions.includes('operations:read')"
        class="uc-activity"
        aria-label="最近操作"
      >
        <div class="uc-activity-heading">
          <h2>最近操作</h2>
          <ElButton link type="primary" @click="$emit('view', 'operations')">
            查看全部<ElIcon><ArrowRight /></ElIcon>
          </ElButton>
        </div>
        <ul v-if="recentOperations.length" class="uc-activity-list">
          <li v-for="(item, index) in recentOperations" :key="item.id || index">
            <div class="uc-activity-detail">
              <strong>{{ actionLabels[item.kind] || item.kind }}</strong>
              <span>{{
                groups.find((group) => group.group_id === item.group_id)?.name ||
                item.name ||
                '已授权群记录'
              }}</span>
            </div>
            <span class="uc-activity-source">{{
              sourceLabels[item.source] || item.source || '页面操作'
            }}</span>
            <span class="uc-activity-state">{{ stateLabel(item.status) }}</span>
            <time :datetime="item.created_at">{{ date(item.created_at) }}</time>
          </li>
        </ul>
        <p v-else class="uc-activity-empty">暂无操作记录</p>
      </section>
      <ElTabs
        v-if="
          view === 'operations' &&
          (permissions.includes('claims:read') || permissions.includes('operations:read'))
        "
        class="uc-records"
      >
        <ElTabPane v-if="!isDemo && permissions.includes('claims:read')" label="授权申请">
          <ElTable :data="result.claims || []" empty-text="暂无申请">
            <ElTableColumn prop="name" label="群组" min-width="130" />
            <ElTableColumn label="平台" min-width="100"
              ><template #default="{ row }">{{
                platformName(row.platform)
              }}</template></ElTableColumn
            >
            <ElTableColumn label="状态" min-width="110"
              ><template #default="{ row }">{{ stateLabel(row.state) }}</template></ElTableColumn
            >
            <ElTableColumn label="时间" min-width="170"
              ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
            >
          </ElTable>
        </ElTabPane>
        <ElTabPane v-if="permissions.includes('operations:read')" label="操作记录">
          <ElTable :data="result.operations || []" empty-text="暂无操作">
            <ElTableColumn label="群组" min-width="130"
              ><template #default="{ row }">{{
                groups.find((group) => group.group_id === row.group_id)?.name ||
                row.name ||
                '已授权群记录'
              }}</template></ElTableColumn
            >
            <ElTableColumn label="来源" min-width="100"
              ><template #default="{ row }"
                >{{ sourceLabels[row.source] || row.source || '页面操作'
                }}<ElTag
                  v-if="row.execution_mode === 'operator_demo' || row.operator_demo"
                  type="info"
                  size="small"
                  >超管演示</ElTag
                ><ElTag v-else-if="row.execution_mode === 'workspace_demo'" type="info" size="small"
                  >演示空间后台</ElTag
                ></template
              ></ElTableColumn
            >
            <ElTableColumn label="操作" min-width="120"
              ><template #default="{ row }">{{
                actionLabels[row.kind] || row.kind
              }}</template></ElTableColumn
            >
            <ElTableColumn label="状态" min-width="100"
              ><template #default="{ row }">{{ stateLabel(row.status) }}</template></ElTableColumn
            >
            <ElTableColumn prop="result_code" label="结果" min-width="140" />
            <ElTableColumn label="时间" min-width="170"
              ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
            >
          </ElTable>
        </ElTabPane>
      </ElTabs>
    </template>
    <UserGroupAction
      v-if="actionGroup"
      :key="`${session.user?.id}:${actionGroup.platform}:${actionGroup.group_id}`"
      :group="actionGroup"
      @close="actionGroup = null"
      @updated="load"
    />
    <OperatorDemoClaim
      v-if="demoClaimGroup && canTestClaim(demoClaimGroup)"
      :key="`${session.user?.id}:${demoClaimGroup.platform}:${demoClaimGroup.group_id}:${demoClaimGroup.version}`"
      :group="demoClaimGroup"
      :is-demo="isDemo"
      @close="demoClaimGroup = null"
      @changed="loadGroups"
    />
    <CustomerOnboarding
      v-if="onboardingOpen && canStartOnboarding"
      :key="`${session.user?.id}:${session.user?.tenant_id}:${groupPlatform}:${onboardingGroup?.group_id || 'new'}:${onboardingGroup?.version ?? 0}`"
      :platforms="availablePlatforms"
      :operator-demo="isDemo"
      :workspace="workspace"
      :initial-platform="groupPlatform"
      :initial-group="onboardingGroup"
      @close="onboardingOpen = false"
      @bound="load"
      @manage="openBoundRuntime"
      @controlled-tools="emit('manage', 'tests', $event)"
    />
    <GroupBusinessRuntime
      v-if="runtimeGroup"
      :key="`${session.user?.id}:${runtimeGroup.platform}:${runtimeGroup.group_id}`"
      :group="runtimeGroup"
      @close="runtimeGroup = null"
      @saved="load"
    />
    <ElDialog
      v-model="claimOpen"
      title="群授权申请"
      width="460px"
      class="uc-dialog"
      align-center
      append-to-body
      @closed="clearClaim"
    >
      <ElForm label-position="top" :disabled="claimSaving" @submit.prevent="claim">
        <ElFormItem label="平台"
          ><ElSelect v-model="claimPlatform">
            <ElOption
              v-for="p in subscription?.platforms || []"
              :key="p"
              :value="p"
              :label="platformName(p)"
            /> </ElSelect
        ></ElFormItem>
        <ElFormItem label="群授权码"
          ><ElInput v-model="claimCode" maxlength="43" autocomplete="off"
        /></ElFormItem>
        <ElFormItem label="本人的原生账号身份"
          ><ElInput v-model="claimIdentity" maxlength="200" autocomplete="off"
        /></ElFormItem>
        <ElAlert v-if="claimError" :title="claimError" type="error" :closable="false" />
      </ElForm>
      <template #footer
        ><ElButton
          :loading="claimSaving"
          :disabled="!subscription?.active || !permissions.includes('claims:create')"
          type="primary"
          @click="claim"
          >提交申请</ElButton
        ></template
      >
    </ElDialog>
    <ElDialog
      v-model="dialog"
      title="本群热群配置"
      width="480px"
      align-center
      append-to-body
      class="uc-dialog"
      @closed="saveError = ''"
    >
      <ElForm
        v-if="target"
        label-position="top"
        :disabled="!canConfigureAI(target)"
        @submit.prevent="save"
      >
        <ElFormItem label="群组"
          ><span>{{ target.name }} · {{ platformName(target.platform) }}</span></ElFormItem
        >
        <ElFormItem label="所属客户"
          ><span
            >{{ target.customer_name || target.tenant_name || '当前客户' }} ·
            {{ robotLabel(target) }}</span
          ></ElFormItem
        >
        <ElFormItem label="已发布人设">
          <ElSelect v-model="form.persona_id" clearable placeholder="使用账号默认人设">
            <ElOption
              v-for="item in personas.filter((p) => p.platform === target?.platform)"
              :key="item.id"
              :value="item.id"
              :label="item.name"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="活跃度"
          ><ElSlider v-model="form.activity" :min="0" :max="100" show-input
        /></ElFormItem>
        <ElFormItem label="活跃时段（北京时间）"
          ><div class="uc-time"
            ><input v-model="form.start" type="time" aria-label="开始时间" /><span>至</span
            ><input v-model="form.end" type="time" aria-label="结束时间" /></div
        ></ElFormItem>
        <ElFormItem
          v-if="!isDemo && subscription?.billing_mode !== 'credits'"
          label="本群月度 Token 上限"
          ><ElInputNumber
            v-model="form.monthly_token_limit"
            :min="0"
            :max="subscription?.monthly_ai_tokens || 0"
            :step="1000"
        /></ElFormItem>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
        <ElAlert
          v-if="!canConfigureAI(target)"
          title="本群授权、套餐或操作权限已变化，当前不可保存"
          type="warning"
          :closable="false"
        />
        <div v-if="chatRuntime" class="uc-chat-runtime" aria-live="polite">
          <ElTag
            :type="chatRuntime.runtime_state === 'running' ? 'success' : 'info'"
            effect="plain"
          >
            {{ runtimeLabel(chatRuntime.runtime_state) }}
          </ElTag>
          <p v-if="chatRuntime.robot_nickname">机器人：{{ chatRuntime.robot_nickname }}</p>
          <p v-if="chatRuntime.effective_settings">
            随机参与 {{ Math.round(chatRuntime.effective_settings.reply_probability * 100) }}% ·
            冷却 {{ chatRuntime.effective_settings.cooldown_seconds }} 秒 · 模型
            {{ chatRuntime.effective_settings.model_alias }}
          </p>
          <p v-if="chatRuntime.effective_settings">
            北京时间：群 {{ time(chatRuntime.effective_settings.group_start_minute) }} 至
            {{ time(chatRuntime.effective_settings.group_end_minute) }}； 账号
            {{ chatRuntime.effective_settings.account_start_hour }}:00 至
            {{ chatRuntime.effective_settings.account_end_hour }}:00
          </p>
          <p v-if="chatRuntime.blocked_reason">{{ runtimeReason(chatRuntime.blocked_reason) }}</p>
        </div>
        <ElAlert v-if="runtimeError" :title="runtimeError" type="warning" :closable="false" />
      </ElForm>
      <template #footer>
        <ElButton
          v-if="!isDemo"
          :disabled="saving || !target || !canConfigureAI(target)"
          @click="requestAI"
          >{{ target?.ai_requested ? '取消启用申请' : '申请启用' }}</ElButton
        >
        <ElButton
          :loading="saving"
          :disabled="!target || !canConfigureAI(target)"
          type="primary"
          @click="save"
          >保存配置</ElButton
        >
      </template>
    </ElDialog>
    <WebCaptcha ref="webCaptcha" />
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDialog,
    ElIcon,
    ElForm,
    ElFormItem,
    ElInput,
    ElInputNumber,
    ElMessage,
    ElOption,
    ElPagination,
    ElRadioButton,
    ElRadioGroup,
    ElSelect,
    ElSkeleton,
    ElSlider,
    ElTable,
    ElTableColumn,
    ElTag,
    ElTabs,
    ElTabPane,
    ElTooltip
  } from 'element-plus'
  import {
    ArrowRight,
    ChatDotRound,
    CircleCheck,
    Plus,
    Refresh,
    Search,
    Setting,
    Switch,
    User
  } from '@element-plus/icons-vue'
  import { api, type Row } from './api'
  import { useSession } from './store'
  import CustomerOnboarding from './CustomerOnboarding.vue'
  import GroupBusinessRuntime from './GroupBusinessRuntime.vue'
  import UserGroupAction from './UserGroupAction.vue'
  import OperatorDemoClaim from './OperatorDemoClaim.vue'
  import WebCaptcha from './WebCaptcha.vue'
  import AdminMark from './AdminMark.vue'
  import {
    actionLabels,
    billingSourceLabel,
    customerReason,
    operatorPlan,
    robotLabel,
    sourceLabels,
    type CustomerPlatform
  } from './customer'
  const props = withDefaults(
    defineProps<{ view?: string; refreshToken?: string; onboardingRequest?: string }>(),
    { view: 'overview', refreshToken: '', onboardingRequest: '' }
  )
  const view = computed(() => props.view)
  const emit = defineEmits<{
    group: [Row]
    assistant: [Row]
    manage: [page: string, platform?: CustomerPlatform]
    'onboarding-consumed': [request: string]
    view: [page: string]
    invite: []
  }>()
  const session = useSession()
  const result = ref<Row | null>(null),
    error = ref(''),
    saveError = ref('')
  const loading = ref(false),
    saving = ref(false),
    dialog = ref(false),
    target = ref<Row | null>(null)
  const form = ref<Row>({})
  const chatRuntime = ref<Row | null>(null),
    runtimeError = ref('')
  let runtimeRequest = 0
  let configRequest = 0,
    configDraftScope = ''
  const sampledAt = ref('')
  const onboardingOpen = ref(false),
    onboardingGroup = ref<Row>(),
    runtimeGroup = ref<Row | null>(null),
    actionGroup = ref<Row | null>(null),
    demoClaimGroup = ref<Row | null>(null)
  function openBoundRuntime(group: Row) {
    if (!isOwner.value || (!isDemo.value && group.tenant_id !== session.user?.tenant_id)) return
    onboardingOpen.value = false
    runtimeGroup.value = { ...group }
  }
  const groupPage = ref(1),
    pageSize = 20
  const isOwner = computed(
    () => session.user?.role === 'superadmin' || session.user?.tenant_role === 'owner'
  )
  const claimOpen = ref(false),
    claimSaving = ref(false),
    claimCode = ref(''),
    claimIdentity = ref(''),
    claimPlatform = ref('wsl'),
    claimError = ref('')
  const webCaptcha = ref<InstanceType<typeof WebCaptcha>>()
  let claimGeneration = 0
  const subscription = computed(() => result.value?.subscription)
  const entitlement = computed(() => result.value?.entitlement)
  const workspace = computed<Row | undefined>(() => result.value?.workspace)
  const plan = computed<Row | undefined>(
    () => result.value?.effective_plan || entitlement.value || subscription.value
  )
  const isSuperadmin = computed(() => session.user?.role === 'superadmin')
  const isDemo = computed(() => operatorPlan(result.value))
  const unlimited = computed(() => plan.value?.unlimited === true)
  const planActive = computed(
    () =>
      isDemo.value ||
      (result.value?.effective_plan?.active ??
        entitlement.value?.active ??
        subscription.value?.active) === true
  )
  function quotaLimit(kind: string) {
    const quotas = result.value?.quotas
    const key = `${kind}_limit`
    const quotaKey = {
      group: 'groups',
      member: 'members',
      assistant: 'assistant',
      credit: 'credits'
    }[kind]
    const quota = quotas?.[quotaKey || kind]
    const value =
      quotas && key in quotas
        ? quotas[key]
        : quota !== undefined
          ? quota && typeof quota === 'object'
            ? quota.limit
            : quota
          : plan.value && key in plan.value
            ? plan.value[key]
            : subscription.value?.[key]
    return unlimited.value || value === null ? '不限额' : (value ?? '—')
  }
  const availablePlatforms = computed<string[]>(() => {
    if (isDemo.value && !plan.value?.platforms?.length) return ['wsl', 'lajiaoliao']
    return plan.value?.platforms || subscription.value?.platforms || []
  })
  const groups = ref<Row[]>([]),
    totalGroups = ref(0),
    customers = ref<Row[]>([]),
    groupsLoading = ref(false),
    groupsError = ref(''),
    usage = ref<Row | null>(null),
    usageLoading = ref(false),
    usageError = ref('')
  const backgroundUsage = computed<Row | null>(() => usage.value?.background_usage || null)
  const personas = computed<Row[]>(() => result.value?.personas || [])
  const recentOperations = computed<Row[]>(() => (result.value?.operations || []).slice(0, 5))
  const billingSource = computed(
    () =>
      result.value?.billing_source ||
      plan.value?.billing_source ||
      entitlement.value?.billing_source ||
      ''
  )
  const billingLabel = computed(() => billingSourceLabel(billingSource.value))
  const permissions = computed<string[]>(
    () =>
      result.value?.permissions ||
      plan.value?.permissions ||
      entitlement.value?.permissions ||
      session.permissions
  )
  const canStartOnboarding = computed(
    () => isOwner.value && permissions.value.includes('claims:create') && planActive.value
  )
  const canReverify = (group: Row) =>
    canStartOnboarding.value &&
    availablePlatforms.value.includes(group.platform) &&
    ['unverified', 'paused'].includes(group.ownership_state)
  const canTestClaim = (group: Row) =>
    isDemo.value &&
    session.user?.role === 'superadmin' &&
    group.active === true &&
    Boolean(group.group_id) &&
    ['wsl', 'lajiaoliao'].includes(group.platform)
  function openDemoClaim(group: Row) {
    if (canTestClaim(group)) demoClaimGroup.value = { ...group }
  }
  function openOnboarding() {
    if (!canStartOnboarding.value) return
    onboardingGroup.value = undefined
    onboardingOpen.value = true
  }
  function reverify(group: Row) {
    if (!canReverify(group)) return
    onboardingGroup.value = { ...group }
    onboardingOpen.value = true
  }
  const filterKey = computed(
    () =>
      `user:${session.user?.id}:${session.user?.tenant_id || 'platform'}:${session.workspace}:group-filters`
  )
  const filters = computed<Row>(() => (session.drafts[filterKey.value] as Row) || {})
  const setFilter = (key: string, value: string) =>
    session.saveDraft(filterKey.value, { ...filters.value, [key]: value })
  const groupSearch = computed({
    get: () => String(filters.value.search || ''),
    set: (value) => setFilter('search', value)
  })
  const groupPlatform = computed({
    get: () => (session.workspace === 'lajiaoliao' ? 'lajiaoliao' : 'wsl'),
    set: (value) => session.selectWorkspace(value === 'lajiaoliao' ? 'lajiaoliao' : 'wsl')
  })
  const groupCustomer = computed({
    get: () => String(filters.value.customer || ''),
    set: (value) => setFilter('customer', value)
  })
  const groupWorkspace = computed({
    get: () => (isSuperadmin.value && filters.value.workspace === 'all' ? 'all' : 'mine'),
    set: (value: string) =>
      session.saveDraft(filterKey.value, { ...filters.value, workspace: value, customer: '' })
  })
  const hasGroupFilters = computed(
    () =>
      Boolean(groupSearch.value.trim()) || (isSuperadmin.value && groupWorkspace.value === 'all')
  )
  const canOnboardPlatform = computed(
    () => canStartOnboarding.value && availablePlatforms.value.includes(groupPlatform.value)
  )
  let consumedOnboardingRequest = ''
  watch(
    [() => props.onboardingRequest, canOnboardPlatform],
    ([request, available]) => {
      if (!request || request === consumedOnboardingRequest || !available || !isSuperadmin.value)
        return
      consumedOnboardingRequest = request
      openOnboarding()
      emit('onboarding-consumed', request)
    },
    { immediate: true }
  )
  const otherGroupPlatform = computed(() =>
    planActive.value
      ? availablePlatforms.value.find((platform) => platform !== groupPlatform.value)
      : undefined
  )
  const startState = computed(() => {
    const platform = platformName(groupPlatform.value)
    if (isDemo.value)
      return {
        title: '把群聊接入你的工作台',
        description:
          '默认接入自己的工作空间，也可明确选择代客户接入。添加机器人好友后，邀请入群并核验群归属。'
      }
    if (!isOwner.value)
      return {
        title: '你的群组工作台',
        description: '群组由负责人分配。还没有授权群时，请联系负责人添加。'
      }
    if (!planActive.value)
      return {
        title: '准备接入你的群组',
        description: `请联系平台管理员开通包含${platform}的套餐，再开始群组接入。`
      }
    if (!canOnboardPlatform.value)
      return {
        title: '准备接入你的群组',
        description: availablePlatforms.value.includes(groupPlatform.value)
          ? '当前账号尚未获得群接入权限，请联系平台管理员。'
          : `当前套餐未包含${platform}，请切换到已开通的平台。`
      }
    return {
      title: '把群聊接入你的工作台',
      description: `已有${platform}群聊可直接接入；还没有群聊，先在 App 中创建。`
    }
  })
  const emptyGroupState = computed(() => {
    const platform = platformName(groupPlatform.value)
    if (hasGroupFilters.value)
      return { title: '没有找到匹配的群组', description: '试试其他群名，或清除当前筛选。' }
    const title = `还没有${platform}授权群组`
    if (isDemo.value)
      return {
        title,
        description: '接入自己的工作空间，添加机器人好友并邀请入群，完成群归属核验后绑定。'
      }
    if (!isOwner.value)
      return {
        title,
        description: '请联系客户负责人，将对应群组分配给你。完成授权后会显示在这里。'
      }
    if (!planActive.value)
      return { title, description: `请联系平台管理员开通包含${platform}的套餐，再接入自己的群组。` }
    if (!availablePlatforms.value.includes(groupPlatform.value))
      return {
        title,
        description: `当前套餐未包含${platform}，可切换到已开通的平台，或联系平台管理员调整套餐。`
      }
    if (!canStartOnboarding.value)
      return { title, description: '当前账号没有群接入权限，请联系平台管理员。' }
    return {
      title: `接入你的第一个${platform}群组`,
      description: `已有群聊可直接接入；还没有群聊，请先在${platform} App 中创建，再完成机器人邀请与群归属核验。`
    }
  })
  function clearGroupFilters() {
    session.saveDraft(filterKey.value, {
      ...filters.value,
      search: '',
      customer: '',
      workspace: 'mine'
    })
  }
  const groupPlatforms = computed(() =>
    ['wsl', 'lajiaoliao'].filter(
      (platform) =>
        session.panels.some((panel) => panel.id === platform) ||
        availablePlatforms.value.includes(platform)
    )
  )
  const canUseAssistant = (group: Row) =>
    group.active &&
    (isDemo.value ||
      (planActive.value &&
        (plan.value?.modules || subscription.value?.modules)?.includes('management_assistant'))) &&
    permissions.value.includes('assistant:use') &&
    session.panels.some((panel) => panel.id === group.platform)
  const availableActions = computed(() =>
    [
      { key: 'groups:read', name: '查看群功能' },
      { key: 'groups:configure', name: '修改群配置' },
      { key: 'groups:ai', name: '热群配置' },
      { key: 'claims:create', name: '申请群授权' },
      { key: 'operations:read', name: '查看操作记录' }
    ].filter((item) => permissions.value.includes(item.key))
  )
  const canOpenGroup = (group: Row) =>
    group.active && session.panels.some((p) => p.id === group.platform)
  const canConfigureAI = (group: Row) =>
    group.active &&
    (isDemo.value ||
      (planActive.value &&
        availablePlatforms.value.includes(group.platform) &&
        (plan.value?.modules || subscription.value?.modules)?.includes('ai'))) &&
    permissions.value.includes('groups:ai')
  const modules: Record<string, string> = {
    moderation: '群管规则',
    replies: '关键词回复',
    schedules: '定时任务',
    ai: '热群服务',
    management_assistant: 'Web 群管助手',
    rankings: '排行',
    cards: '名片',
    friends: '好友审批',
    finance: '财务',
    activities: '活动'
  }
  const platformName = (id: string) => ({ wsl: '旺商聊', lajiaoliao: '辣椒聊' })[id] || id
  const date = (value?: string) =>
    value
      ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
      : '—'
  const legacyMoneyUnits: Record<string, { scale: number; currency: string }> = {
    micro_credit: { scale: 1000000000, currency: 'USDT' },
    micro_usdt: { scale: 1000000, currency: 'USDT' },
    micro_usd: { scale: 1000000, currency: 'USD' }
  }
  const money = (value?: number | null, record: Row | null = usage.value) => {
    const legacy = legacyMoneyUnits[String(record?.unit || '')]
    const scale = record?.currency_scale ?? legacy?.scale
    const currency = record?.currency || legacy?.currency
    if (
      typeof value !== 'number' ||
      !Number.isFinite(value) ||
      typeof scale !== 'number' ||
      !Number.isFinite(scale) ||
      scale <= 0 ||
      !currency
    )
      return '—'
    return `${(value / scale).toLocaleString('zh-CN', {
      maximumFractionDigits: Math.min(12, Math.max(0, Math.ceil(Math.log10(scale))))
    })} ${currency}`
  }
  const stateLabel = (value: string) =>
    ({
      pending: '待审核',
      approved: '已批准',
      rejected: '已拒绝',
      queued: '排队中',
      running: '执行中',
      succeeded: '已提交',
      failed: '失败',
      cancelled: '已取消',
      expired: '已过期',
      unknown: '结果未知'
    })[value] || value
  const time = (minute: number) =>
    `${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`
  const minute = (value: string) => {
    const [h, m] = value.split(':').map(Number)
    return h * 60 + m
  }
  function clearClaim() {
    claimCode.value = ''
    claimIdentity.value = ''
    claimError.value = ''
  }
  function cancelClaimVerification() {
    claimGeneration++
    webCaptcha.value?.cancel()
    claimSaving.value = false
  }
  let generation = 0
  let controller: AbortController | undefined
  let groupGeneration = 0,
    usageGeneration = 0
  let groupController: AbortController | undefined,
    usageController: AbortController | undefined,
    runtimeController: AbortController | undefined
  let searchTimer: ReturnType<typeof setTimeout> | undefined
  const currentScope = () =>
    `${session.user?.id}:${session.user?.tenant_id}:${session.user?.role}:${session.console}:${session.workspace}:${session.selectedAccount}`
  async function loadGroups() {
    if (!result.value || session.console !== 'user') return
    const started = ++groupGeneration,
      scope = currentScope()
    groupController?.abort()
    const request = new AbortController()
    groupController = request
    groupsLoading.value = true
    groupsError.value = ''
    groups.value = []
    const query = new URLSearchParams({
      page: String(groupPage.value),
      page_size: String(pageSize),
      platform: groupPlatform.value
    })
    if (groupSearch.value.trim()) query.set('search', groupSearch.value.trim())
    if (isSuperadmin.value) {
      query.set('workspace', groupWorkspace.value)
      if (groupWorkspace.value === 'all' && groupCustomer.value)
        query.set('tenant_id', groupCustomer.value)
    }
    try {
      const next = await api<Row>(
        `/user-center/groups?${query}`,
        'GET',
        undefined,
        request.signal,
        groupPlatform.value as 'wsl' | 'lajiaoliao'
      )
      if (started !== groupGeneration || scope !== currentScope() || request.signal.aborted) return
      groups.value = next.items || next.groups || []
      totalGroups.value = Number(next.total || 0)
      if (Array.isArray(next.customers)) customers.value = next.customers
      if (groupPage.value > 1 && !groups.value.length && totalGroups.value > 0)
        groupPage.value = Math.max(1, Math.ceil(totalGroups.value / pageSize))
    } catch (e) {
      if (started === groupGeneration && scope === currentScope() && !request.signal.aborted)
        groupsError.value = (e as Error).message
    } finally {
      if (started === groupGeneration) groupsLoading.value = false
    }
  }
  async function loadUsage() {
    if (session.console !== 'user') return
    const started = ++usageGeneration,
      scope = currentScope()
    usageController?.abort()
    const request = new AbortController()
    usageController = request
    usageLoading.value = true
    usageError.value = ''
    try {
      const next = await api<Row>(
        '/user-center/usage',
        'GET',
        undefined,
        request.signal,
        groupPlatform.value as 'wsl' | 'lajiaoliao'
      )
      if (started === usageGeneration && scope === currentScope() && !request.signal.aborted)
        usage.value = next
    } catch (e) {
      if (started === usageGeneration && scope === currentScope() && !request.signal.aborted)
        usageError.value = (e as Error).message
    } finally {
      if (started === usageGeneration) usageLoading.value = false
    }
  }
  async function load() {
    const started = ++generation,
      owner = session.user?.id
    controller?.abort()
    const request = new AbortController(),
      scope = currentScope()
    controller = request
    loading.value = true
    error.value = ''
    try {
      const next = await api<Row>(
        '/user-center',
        'GET',
        undefined,
        request.signal,
        groupPlatform.value as 'wsl' | 'lajiaoliao'
      )
      if (
        generation === started &&
        session.user?.id === owner &&
        scope === currentScope() &&
        !request.signal.aborted
      ) {
        result.value = next
        usage.value = null
        sampledAt.value = new Date().toISOString()
        if (!next.subscription?.platforms?.includes(claimPlatform.value))
          claimPlatform.value = next.subscription?.platforms?.[0] || 'wsl'
        await Promise.all([loadGroups(), loadUsage()])
      }
    } catch (e) {
      if (generation === started && scope === currentScope() && !request.signal.aborted)
        error.value = (e as Error).message
    } finally {
      if (generation === started) loading.value = false
    }
  }
  function edit(group: Row) {
    if (!canConfigureAI(group)) return
    configRequest++
    target.value = { ...group }
    configDraftScope = `user:${session.user?.id}:${group.tenant_id || session.user?.tenant_id || 'platform'}:${group.platform}:${group.primary_account_id || group.account_id || 'bound'}:${group.group_id}:chat-preferences`
    const draft = session.drafts[configDraftScope] as Row | undefined
    form.value = {
      ...group,
      persona_id: group.persona_id || '',
      start: time(group.start_minute),
      end: time(group.end_minute),
      ...(draft || {})
    }
    saveError.value = ''
    dialog.value = true
    void loadRuntime(group)
  }
  const runtimeLabel = (state: string) =>
    ({
      running: '运行中',
      outside_hours: '非活跃时段',
      blocked: '等待启用'
    })[state] || '等待观测'
  const runtimeReason = (reason: string) =>
    ({
      heat_chat_platform_not_admitted: '本平台基础聊天尚未开放',
      heat_chat_account_disabled: '聊天账号未开启',
      heat_chat_group_not_enabled: '本群启用申请尚未生效',
      heat_chat_receiver_unavailable: '聊天账号接收连接尚未就绪',
      heat_chat_native_role_unavailable: '本群原生成员身份尚未核实',
      heat_chat_scope_unavailable: '请先申请启用，并分配已登录的聊天账号',
      heat_chat_status_unavailable: '暂时无法读取运行状态'
    })[reason] || '当前配置尚未满足运行条件'
  async function loadRuntime(group: Row) {
    const request = ++runtimeRequest,
      actor = session.user?.id
    runtimeController?.abort()
    const active = new AbortController()
    runtimeController = active
    chatRuntime.value = null
    runtimeError.value = ''
    try {
      const data = await api<Row>(
        `/groups/${group.group_id}/chat-runtime`,
        'GET',
        undefined,
        active.signal,
        group.platform
      )
      if (
        request === runtimeRequest &&
        !active.signal.aborted &&
        dialog.value &&
        target.value?.group_id === group.group_id &&
        actor === session.user?.id
      )
        chatRuntime.value = data
    } catch (e) {
      if (
        request === runtimeRequest &&
        !active.signal.aborted &&
        dialog.value &&
        target.value?.group_id === group.group_id &&
        actor === session.user?.id
      )
        runtimeError.value = (e as Error).message
    }
  }
  async function mutate(path: string, payload: Row) {
    if (!target.value || saving.value || !canConfigureAI(target.value)) return
    const owner = session.user?.id,
      started = generation,
      request = configRequest,
      selectedGroup = target.value.group_id,
      selectedPlatform = target.value.platform
    const current = () =>
      owner === session.user?.id &&
      started === generation &&
      request === configRequest &&
      selectedGroup === target.value?.group_id &&
      selectedPlatform === target.value?.platform
    saving.value = true
    saveError.value = ''
    try {
      await api(path, 'PUT', payload, undefined, selectedPlatform)
      if (!current()) return
      session.saveDraft(configDraftScope, {})
      dialog.value = false
      ElMessage.success('配置已保存')
      await load()
    } catch (e) {
      if (current()) saveError.value = (e as Error).message
    } finally {
      if (request === configRequest) saving.value = false
    }
  }
  function save() {
    if (!target.value) return
    if (
      !/^\d{2}:\d{2}$/.test(form.value.start || '') ||
      !/^\d{2}:\d{2}$/.test(form.value.end || '')
    ) {
      saveError.value = '请填写有效的开始和结束时间'
      return
    }
    void mutate(`/groups/${target.value.group_id}/chat-preferences`, {
      version: target.value.version,
      preference_version: target.value.preference_version,
      persona_id: form.value.persona_id || '',
      activity: form.value.activity,
      start_minute: minute(form.value.start),
      end_minute: minute(form.value.end),
      monthly_token_limit: form.value.monthly_token_limit
    })
  }
  function requestAI() {
    if (target.value)
      void mutate(`/groups/${target.value.group_id}/ai-service`, {
        version: target.value.version,
        enabled: !target.value.ai_requested,
        invite: false
      })
  }
  async function claim() {
    if (!claimOpen.value || claimSaving.value) return
    if (!subscription.value?.active || !permissions.value.includes('claims:create')) {
      claimError.value = '套餐或操作权限已变化，当前不可申请群授权'
      return
    }
    if (saving.value || claimCode.value.length !== 43 || !claimIdentity.value.trim()) {
      claimError.value = '请填写完整授权码和原生身份'
      return
    }
    const selected = claimPlatform.value
    if (selected !== 'wsl' && selected !== 'lajiaoliao') {
      claimError.value = '请选择有效的群组平台'
      return
    }
    const scope = currentScope(),
      request = ++claimGeneration,
      input = { code: claimCode.value, native_identity: claimIdentity.value.trim() }
    const current = () =>
      request === claimGeneration &&
      claimOpen.value &&
      scope === currentScope() &&
      selected === claimPlatform.value &&
      subscription.value?.active &&
      permissions.value.includes('claims:create')
    claimSaving.value = true
    claimError.value = ''
    try {
      const ticket = await webCaptcha.value?.open({ purpose: 'group_claim', platform: selected })
      if (!ticket || !current()) return
      await api('/claims', 'POST', input, undefined, selected, undefined, ticket)
      if (!current()) return
      claimOpen.value = false
      onboardingOpen.value = false
      runtimeGroup.value = null
      ElMessage.success('申请已提交，等待管理员核验')
      await load()
    } catch (e) {
      if (current()) claimError.value = (e as Error).message
    } finally {
      if (request === claimGeneration) claimSaving.value = false
    }
  }
  watch(
    [
      () => claimOpen.value,
      () => claimPlatform.value,
      () => claimCode.value,
      () => claimIdentity.value,
      currentScope,
      () => session.user?.tenant_role,
      () => JSON.stringify([...session.permissions].sort()),
      () => permissions.value.includes('claims:create'),
      () => subscription.value?.active
    ],
    cancelClaimVerification,
    { flush: 'sync' }
  )
  watch(
    () => session.user?.id,
    () => {
      result.value = null
      configRequest++
      runtimeRequest++
      saving.value = false
      sampledAt.value = ''
      dialog.value = false
      claimOpen.value = false
      onboardingOpen.value = false
      onboardingGroup.value = undefined
      target.value = null
      void load()
    },
    { immediate: true }
  )
  watch(
    () => props.refreshToken,
    () => {
      if (session.user?.id) void load()
    }
  )
  watch(
    [
      () => session.user?.id,
      () => session.user?.tenant_id,
      () => session.user?.role,
      () => session.user?.tenant_role,
      () => JSON.stringify([...session.permissions].sort()),
      () => session.console,
      () => session.workspace,
      () => session.selectedAccount
    ],
    () => {
      demoClaimGroup.value = null
    },
    { flush: 'sync' }
  )
  watch([groupSearch, groupCustomer, groupWorkspace], () => {
    groupController?.abort()
    groupGeneration++
    groups.value = []
    groupsLoading.value = true
    clearTimeout(searchTimer)
    if (groupPage.value !== 1) {
      groupPage.value = 1
      return
    }
    searchTimer = setTimeout(() => void loadGroups(), 250)
  })
  watch(groupPage, () => {
    clearTimeout(searchTimer)
    void loadGroups()
  })
  watch(
    form,
    () => {
      if (dialog.value && configDraftScope) session.saveDraft(configDraftScope, form.value)
    },
    { deep: true }
  )
  watch(dialog, (value) => {
    if (!value) {
      configRequest++
      runtimeRequest++
      runtimeController?.abort()
      chatRuntime.value = null
      saving.value = false
    }
  })
  onBeforeUnmount(() => {
    cancelClaimVerification()
    generation++
    groupGeneration++
    usageGeneration++
    runtimeRequest++
    configRequest++
    clearTimeout(searchTimer)
    claimOpen.value = false
    controller?.abort()
    groupController?.abort()
    usageController?.abort()
    runtimeController?.abort()
  })
</script>

<style scoped>
  .uc-start {
    padding-bottom: 28px;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .uc-start-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
  }
  .uc-start-copy {
    min-width: 0;
  }
  .uc-start-platform {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .uc-start-platform img {
    width: 22px;
    height: 22px;
    object-fit: contain;
  }
  .uc-start h2 {
    font-size: 22px;
    line-height: 1.5;
    font-weight: 600;
    margin: 0;
    overflow-wrap: anywhere;
  }
  .uc-start p {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    line-height: 1.7;
    margin: 8px 0 0;
    overflow-wrap: anywhere;
  }
  .uc-start-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 8px;
    flex-shrink: 0;
    max-width: 44%;
  }
  .uc-start-actions .el-button {
    margin: 0;
    max-width: 100%;
    height: auto;
    min-height: 38px;
    white-space: normal;
    line-height: 1.5;
  }
  .uc-start-steps {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    list-style: none;
    margin: 26px 0 0;
    padding: 0;
    gap: 20px;
  }
  .uc-start-steps li {
    display: flex;
    gap: 12px;
    min-width: 0;
  }
  .uc-start-steps li > span {
    display: grid;
    place-items: center;
    flex: 0 0 30px;
    height: 30px;
    border-radius: 6px;
    background: var(--ui-soft);
    font-family: var(--ui-mono);
    font-size: 11px;
    color: var(--el-text-color-secondary);
  }
  .uc-start-steps strong {
    font-size: 13px;
    font-weight: 600;
  }
  .uc-start-steps p {
    margin-top: 3px;
    font-size: 12px;
  }
  .uc-summary {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    border-bottom: 1px solid var(--el-border-color-light);
    gap: 0;
    padding: 24px 0;
    margin: 0 0 24px;
  }
  .user-center {
    min-width: 0;
  }
  .uc-usage {
    padding-bottom: 16px;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .uc-usage-values {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 16px;
  }
  .uc-usage-values dd {
    margin: 8px 0 0;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .uc-summary > div {
    display: flex;
    min-width: 0;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    padding: 0 24px;
    border-left: 1px solid var(--el-border-color-light);
  }
  .uc-summary > div:first-child {
    padding-left: 0;
    border-left: 0;
  }
  .uc-summary strong {
    font-size: 20px;
    line-height: 1.6;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .uc-summary span,
  .uc-summary small,
  dt {
    color: var(--el-text-color-secondary);
  }
  .uc-package {
    display: grid;
    grid-template-columns: 100px minmax(0, 1fr);
    gap: 14px;
    padding: 20px 0;
  }
  .uc-package dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .uc-toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    align-items: center;
    margin: 0 0 18px;
  }
  .uc-toolbar h2 {
    flex: 1;
  }
  .uc-grants,
  .uc-admin-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .uc-admin-actions {
    margin: 20px 0;
  }
  .uc-admin-actions .el-button {
    margin: 0;
  }
  .uc-filters {
    display: grid;
    grid-template-columns: minmax(150px, 1fr) 130px;
    gap: 10px;
    padding: 16px;
    background: var(--ui-soft);
    border-block: 1px solid var(--el-border-color-light);
  }
  .uc-filters-all {
    grid-template-columns: minmax(150px, 1fr) 130px 160px;
  }
  .uc-workspace-filter {
    display: flex;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .uc-workspace-filter :deep(.el-radio-button__inner) {
    white-space: normal;
    line-height: 1.5;
  }
  .uc-usage-source {
    color: var(--el-text-color-regular);
    font-size: 13px;
    line-height: 1.7;
    overflow-wrap: anywhere;
  }
  .uc-usage h3 {
    margin: 0 0 16px;
    font-size: 15px;
  }
  .uc-background-usage {
    margin-top: 24px;
    padding-top: 24px;
    border-top: 1px solid var(--el-border-color-light);
  }
  .uc-list-count {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    margin-block: 14px;
  }
  .uc-empty {
    padding: 32px 0;
    border-block: 1px solid var(--el-border-color-light);
  }
  .uc-empty-intro {
    display: flex;
    align-items: flex-start;
    gap: 16px;
  }
  .uc-empty-symbol {
    flex: 0 0 44px;
    width: 44px;
    height: 44px;
    border-radius: 8px;
    background: var(--ui-accent-soft, #f1f4e8);
    color: var(--ui-accent, #3b4810);
    font-size: 24px;
  }
  .uc-empty-copy {
    min-width: 0;
    max-width: 640px;
  }
  .uc-empty h3 {
    margin: 0;
    color: var(--el-text-color-primary);
    font-size: 18px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .uc-empty p {
    margin: 8px 0 0;
    color: var(--el-text-color-secondary);
    line-height: 1.7;
    overflow-wrap: anywhere;
  }
  .uc-empty-actions:not(:empty) {
    margin-top: 20px;
  }
  .uc-empty-actions .el-button {
    max-width: 100%;
    height: auto;
    min-height: 36px;
    white-space: normal;
    line-height: 1.5;
  }
  .uc-empty-steps {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 24px;
    list-style: none;
    padding: 24px 0 0;
    margin: 28px 0 0;
    border-top: 1px solid var(--el-border-color-light);
  }
  .uc-empty-steps li {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-width: 0;
    font-size: 13px;
  }
  .uc-empty-steps li > span {
    display: grid;
    place-items: center;
    flex: 0 0 24px;
    height: 24px;
    border: 1px solid var(--el-border-color);
    border-radius: 50%;
    font-size: 12px;
  }
  .uc-empty-steps strong {
    display: block;
    line-height: 24px;
  }
  .uc-empty-steps p {
    margin-top: 4px;
  }
  .uc-group-name {
    font-weight: 600;
  }
  .uc-assistant-icon {
    width: 16px;
    height: 16px;
    margin-right: 6px;
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
  }
  .uc-assistant-icon :deep(.admin-mark) {
    width: 16px;
    height: 16px;
    flex-basis: 16px;
  }
  .uc-robot {
    display: block;
    color: var(--el-text-color-secondary);
    margin-top: 4px;
  }
  .uc-demo-verified {
    margin-top: 6px;
  }
  .uc-pagination {
    justify-content: flex-end;
    padding-block: 20px;
  }
  .uc-groups :deep(.el-button + .el-button) {
    margin-left: 0;
  }
  .uc-groups :deep(.el-table__cell:last-child .cell) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 14px;
    padding-block: 10px;
  }
  .uc-records {
    margin-top: 24px;
    border-top: 1px solid var(--el-border-color-light);
    padding-top: 16px;
  }
  .uc-activity {
    margin-top: 28px;
    padding-top: 24px;
    border-top: 1px solid var(--el-border-color-light);
  }
  .uc-activity-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 12px;
  }
  .uc-activity-heading :deep(.el-icon) {
    margin-left: 6px;
  }
  .uc-activity-list {
    padding: 0;
    margin: 0;
    list-style: none;
  }
  .uc-activity-list li {
    display: grid;
    grid-template-columns: minmax(160px, 1fr) minmax(90px, 0.4fr) 88px 160px;
    align-items: center;
    gap: 16px;
    min-height: 70px;
    padding-block: 12px;
    border-bottom: 1px solid var(--el-border-color-lighter);
    font-size: 13px;
  }
  .uc-activity-detail {
    display: grid;
    gap: 4px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .uc-activity-detail strong {
    font-size: 14px;
    font-weight: 500;
  }
  .uc-activity-detail span,
  .uc-activity-source,
  .uc-activity-list time,
  .uc-activity-empty {
    color: var(--el-text-color-secondary);
  }
  .uc-activity-list time {
    text-align: right;
    font-size: 12px;
  }
  .uc-activity-empty {
    margin: 0;
    padding-block: 24px;
    font-size: 14px;
  }
  .uc-mobile-groups {
    display: none;
  }
  h2 {
    font-size: 17px;
    margin: 0;
  }
  .uc-time {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
  }
  .uc-time input {
    min-width: 0;
    width: 125px;
    padding: 8px;
    border: 1px solid var(--el-border-color);
    border-radius: 4px;
    background: var(--el-bg-color);
    color: inherit;
  }
  @media (max-width: 1000px) {
    .uc-activity-list li {
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 6px 12px;
      padding-block: 14px;
    }
    .uc-activity-detail {
      grid-row: span 2;
    }
    .uc-activity-source {
      grid-column: 1;
      grid-row: 3;
      font-size: 12px;
    }
    .uc-activity-state {
      grid-column: 2;
      grid-row: 1;
      text-align: right;
    }
    .uc-activity-list time {
      grid-column: 2;
      grid-row: 3;
      max-width: 138px;
    }
    .uc-start-heading {
      display: block;
    }
    .uc-start-actions {
      justify-content: flex-start;
      max-width: none;
      margin-top: 18px;
    }
    .uc-start h2 {
      font-size: 20px;
    }
    .uc-start-steps {
      grid-template-columns: 1fr;
      gap: 14px;
      margin-top: 20px;
    }
    .uc-start-steps li {
      align-items: center;
    }
    .uc-summary {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 20px 0;
    }
    .uc-summary > div {
      padding-inline: 16px;
    }
    .uc-summary > div:nth-child(odd) {
      padding-left: 0;
      border-left: 0;
    }
    .uc-usage-values {
      grid-template-columns: 1fr;
      gap: 12px;
    }
  }
  @media (max-width: 1000px) {
    .uc-empty {
      padding-block: 24px;
    }
    .uc-empty-intro {
      gap: 12px;
    }
    .uc-empty-steps {
      grid-template-columns: 1fr;
      gap: 20px;
    }
    .uc-filters {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      padding: 12px;
    }
    .uc-filters > .el-input {
      grid-column: 1 / -1;
    }
    .uc-groups {
      display: none;
    }
    .uc-mobile-groups {
      display: block;
    }
    .uc-group-row {
      padding: 20px 0;
      border-bottom: 1px solid var(--el-border-color-light);
    }
    .uc-group-heading {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 12px;
    }
    .uc-group-heading strong {
      font-size: 14px;
      font-weight: 500;
      overflow-wrap: anywhere;
    }
    .uc-group-row p {
      font-size: 12px;
      color: var(--el-text-color-secondary);
      margin: 8px 0 12px;
    }
    .uc-group-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 18px;
    }
    .uc-group-actions .el-button {
      margin: 0;
    }
    .uc-package {
      grid-template-columns: 76px minmax(0, 1fr);
      gap: 12px;
      font-size: 13px;
    }
  }
</style>
<style>
  .uc-dialog {
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 32px);
    margin: 16px auto;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .uc-dialog > .el-dialog__body {
    min-height: 0;
    overflow-y: auto;
  }
  .uc-dialog > .el-dialog__header,
  .uc-dialog > .el-dialog__footer {
    flex-shrink: 0;
  }
</style>
