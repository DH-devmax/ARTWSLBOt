<template>
  <section v-if="session.user?.role === 'superadmin'" class="test-tools">
    <div class="tools-toolbar">
      <h3>受控验证</h3>
      <ElButton
        v-if="session.user?.role === 'superadmin'"
        :disabled="!supportedWorkspace || busy || !currentAccount"
        @click="openControlledFriend()"
      >
        <Search :size="16" />受控搜索加好友
      </ElButton>
      <ElButton
        type="primary"
        :disabled="!supportedWorkspace || busy || !currentAccount"
        @click="
          $emit('create', {
            account_id: accountId,
            group_id: preparationGroup,
            groups: testGroups
          })
        "
      >
        <Plus :size="16" />创建授权窗口
      </ElButton>
    </div>
    <ElAlert
      v-if="!supportedWorkspace"
      title="请切换到对应平台的管理工作区进行受控验证。"
      :closable="false"
    />
    <template v-else>
      <div class="tools-account">
        <ElSelect v-model="accountId" aria-label="测试账号" :disabled="busy">
          <ElOption
            v-for="item in accounts.filter(
              (a) => a.platform === workspace && a.purpose === 'management'
            )"
            :key="item.id"
            :value="item.id"
            :label="item.name"
          />
        </ElSelect>
        <ElSwitch
          :model-value="!!currentAccount?.testing_enabled"
          :disabled="busy || !currentAccount"
          active-text="受控测试"
          @change="toggleTesting"
        />
        <ElSwitch
          :model-value="!!currentAccount?.connection_enabled"
          :disabled="busy || !currentAccount?.testing_enabled"
          active-text="动作准入"
          @change="toggleAdmission"
        />
      </div>
      <ControlledOnboarding
        :platform="workspace"
        :account-id="accountId"
        :accounts="accounts"
        @onboard="emit('onboard', $event)"
        @candidates="onboardingGroups = $event"
        :disabled="busy"
      />
      <div v-if="currentAccount" class="tools-preparation">
        <div class="tools-toolbar">
          <span>消息接收：{{ receiverText }}</span>
          <ElButton :disabled="busy" :loading="loadingPreparation" @click="loadPreparation">
            <RefreshCw :size="16" />刷新检查
          </ElButton>
          <ElButton
            v-if="receiver && (!receiver.enabled || receiver.snapshot?.state === 'blocked')"
            :disabled="busy"
            @click="enableReceiver"
          >
            {{ receiver.enabled ? '恢复已有会话接收' : '启用已有会话接收' }}
          </ElButton>
        </div>
        <div class="tools-account">
          <ElSelect v-model="preparationGroup" aria-label="角色核验群组" :disabled="busy">
            <ElOption
              v-for="g in testGroups"
              :key="g.group_id"
              :value="g.group_id"
              :label="g.name"
            />
          </ElSelect>
          <ElButton :disabled="busy || !preparationGroup" @click="observeRole">
            <RefreshCw :size="16" />核验当前角色
          </ElButton>
          <span v-if="roleObservation">
            当前角色：{{ roleName(roleObservation.role) }} · {{ date(roleObservation.observed_at) }}
          </span>
        </div>
        <p v-if="!currentAccount.testing_enabled || !currentAccount.connection_enabled">
          创建窗口前需启用受控测试与动作准入，并核验当前群角色。开关变更会使已有窗口失效。
        </p>
        <ElAlert
          v-if="preparationError"
          :title="preparationError"
          type="warning"
          :closable="false"
        />
      </div>
      <div class="tools-windows">
        <button
          v-for="item in scopedWindows"
          :key="item.id"
          type="button"
          :disabled="busy"
          :class="['tools-grant', { selected: windowId === item.id }]"
          @click="selectWindow(item.id)"
        >
          <strong>{{ groupName(item.group_id) }}</strong>
          <span v-if="isControlledFriendWindow(item)">受控搜索加好友</span>
          <span>{{ usedCount(item) }} / {{ item.budget }} 次 · {{ grantState(item) }}</span>
          <small>{{ date(item.expires_at) }}</small>
        </button>
        <ElEmpty
          v-if="!scopedWindows.length"
          description="此账号暂无本人创建的授权窗口"
          :image-size="48"
        />
      </div>
      <div v-if="grant && !isControlledFriendWindow(grant)" class="tools-scope">
        <div class="tools-toolbar">
          <h4>{{ groupName(grant.group_id) }}</h4>
          <ElButton
            v-if="!grant.cancelled"
            type="danger"
            link
            :disabled="busy"
            @click="$emit('cancel', grant)"
            >取消授权</ElButton
          >
        </div>
        <p
          >{{ grant.admission_mode === 'native_debug' ? '原生调试授权' : '生产套餐校验' }} · 仅限
          {{ grant.actions.map(actionName).join('、') }}；最多 {{ grant.budget }} 次，{{
            date(grant.expires_at)
          }}
          到期。</p
        >
        <ElAlert
          v-if="!validGrant || usedCount(grant) >= grant.budget"
          :title="
            !validGrant
              ? '窗口已关闭或即将到期；原操作仍可在下方查询。'
              : '次数已用完；仍可读取群状态与查询原操作。'
          "
          type="info"
          :closable="false"
        />
        <ElButton
          v-if="hasModeration"
          :disabled="busy || !validGrant"
          :loading="reading"
          @click="readGroup"
        >
          <RefreshCw :size="16" />读取当前群状态
        </ElButton>
        <ElButton
          v-if="hasMembership"
          :disabled="busy || !validGrant"
          :loading="reading"
          @click="readMembership"
        >
          <RefreshCw :size="16" />{{
            action === 'friend_approve'
              ? '读取指定好友申请与群状态'
              : action === 'friend_delete'
                ? '读取当前好友与群状态'
                : '读取入退群状态'
          }}
        </ElButton>
        <ElButton
          v-if="
            supportedWorkspace &&
            !grant?.actions.some((a: string) =>
              ['group_accept', 'friend_delete', 'friend_approve'].includes(a)
            )
          "
          :disabled="busy || !validGrant"
          @click="readInvitations"
        >
          <RefreshCw :size="16" />{{ workspace === 'lajiaoliao' ? '读取入群申请' : '读取邀请记录' }}
        </ElButton>
        <div v-if="snapshot" class="tools-state">
          <span>机器人角色：{{ roleName(snapshot.self_role) }}</span>
          <span>全群禁言：{{ snapshot.policy?.muted ? '是' : '否' }}</span>
          <span
            >个人禁言名单：{{
              snapshot.mutes_known ? `${snapshot.muted_ids.length} 人` : '未取得可靠状态'
            }}</span
          >
          <small>采样：{{ date(snapshot.sampled_at) }}</small>
          <span v-if="snapshot.bans_known">
            原生永久禁入：{{ snapshot.group_bans?.length || 0 }} 人
          </span>
        </div>
        <div v-if="membership" class="tools-state">
          <span
            >本人在群：{{
              membership.directory_known ? (membership.in_group ? '是' : '否') : '未核验'
            }}</span
          >
          <span>当前角色：{{ roleName(membership.self_role) }}</span>
          <span v-if="action === 'friend_delete'">
            当前好友：{{
              completeFriends ? `${membership.friends.length} 人` : '未取得完整好友状态'
            }}
          </span>
          <span v-if="action === 'friend_approve'">
            申请编号：{{ grant.friend_application_id }}
          </span>
          <small>采样：{{ date(membership.sampled_at) }}</small>
        </div>
        <ElAlert
          v-if="grant.actions.includes('friend_approve')"
          :title="
            approveGrantValid
              ? '仅同意此编号与申请人的待处理申请，一次尝试；不会认领群组或启用业务。'
              : '同意申请需独立原生调试窗口、固定申请编号、单个申请人和一次预算。'
          "
          type="info"
          :closable="false"
        />
        <ElAlert
          v-if="grant.actions.includes('friend_delete')"
          :title="
            deleteGrantValid
              ? '删除好友关系，不退群、不拉黑。仅限本窗口的一次授权尝试。'
              : '删除好友仅支持独立原生调试窗口，动作不可混合且预算必须为一次。'
          "
          type="warning"
          :closable="false"
        />
        <div v-if="invitations" class="tools-invitations">
          <p>账号可见窗口 · {{ date(invitations.sampled_at) }} · 不保证完整历史</p>
          <p v-for="record in invitations.records" :key="record.id">
            {{ record.type }} · {{ record.applicant.name || record.applicant.id }} ({{
              record.applicant.id
            }})
            <span v-if="record.inviter"
              >← {{ record.inviter.name || record.inviter.id }} ({{ record.inviter.id }})</span
            >
            · {{ record.state }}
          </p>
          <p v-if="!invitations.records.length">此窗口内无该群可见记录</p>
        </div>
        <ElForm label-position="top" @submit.prevent>
          <p v-if="!(membershipAction ? freshMembership : freshSnapshot)"
            >提交前请读取当前群状态，状态有效期为 30 秒。</p
          >
          <div class="tools-fields">
            <ElFormItem label="验证动作">
              <ElSelect v-model="action" aria-label="验证动作" :disabled="busy">
                <ElOption
                  v-for="kind in availableActions"
                  :key="kind"
                  :value="kind"
                  :label="actionName(kind)"
                />
              </ElSelect>
            </ElFormItem>
            <ElFormItem
              v-if="requiresTarget"
              :label="
                action === 'friend_approve'
                  ? '指定好友申请人'
                  : action === 'friend_delete'
                    ? '授权目标好友'
                    : '授权目标成员'
              "
            >
              <ElSelect
                v-model="target"
                :aria-label="
                  action === 'friend_approve'
                    ? '指定好友申请人'
                    : action === 'friend_delete'
                      ? '授权目标好友'
                      : '授权目标成员'
                "
                :popper-class="
                  ['friend_delete', 'friend_approve'].includes(action)
                    ? 'friend-delete-options'
                    : ''
                "
                :fit-input-width="['friend_delete', 'friend_approve'].includes(action)"
                :disabled="busy"
              >
                <ElOption
                  v-for="id in grant.target_ids"
                  :key="id"
                  :value="id"
                  :label="memberName(id)"
                  :disabled="!eligibleTarget(id)"
                />
              </ElSelect>
            </ElFormItem>
            <ElFormItem v-if="action === 'mute_member'" label="禁言 / 分钟">
              <ElInputNumber v-model="minutes" :min="1" :max="5" :disabled="busy" />
            </ElFormItem>
            <ElFormItem
              v-if="
                workspace === 'lajiaoliao' && ['kick_member', 'kick_member_ban'].includes(action)
              "
              label="进群限制"
            >
              <ElCheckbox
                :model-value="action === 'kick_member_ban'"
                :disabled="
                  busy ||
                  !grant.actions.includes('kick_member_ban') ||
                  !grant.actions.includes('kick_member')
                "
                @change="action = $event ? 'kick_member_ban' : 'kick_member'"
                >永久禁止申请进群</ElCheckbox
              >
            </ElFormItem>
            <ElFormItem v-if="action === 'group_accept'" label="本人的待处理邀请">
              <ElSelect v-model="text" aria-label="本人的待处理邀请" :disabled="busy">
                <ElOption
                  v-for="item in selfInvitations"
                  :key="item.id"
                  :value="item.id"
                  :label="`${item.inviter?.name || item.inviter?.id || '邀请人'} · ${item.id}`"
                />
              </ElSelect>
            </ElFormItem>
          </div>
          <ElFormItem
            v-if="action === 'publish_notice' || action === 'send_text'"
            :label="action === 'send_text' ? '群文本' : '公告正文'"
          >
            <ElInput
              v-model="text"
              type="textarea"
              :maxlength="action === 'send_text' ? 2000 : 600"
              :rows="3"
              :disabled="busy"
            />
          </ElFormItem>
          <ElFormItem v-if="action === 'recall_message'" label="两分钟内的群消息">
            <ElSelect v-model="messageEvent" aria-label="待撤回消息" :disabled="busy">
              <ElOption
                v-for="item in messageCandidates"
                :key="item.event_id"
                :value="item.event_id"
                :label="`${item.sender} · ${item.preview || '消息'}`"
                :disabled="!grant.target_ids.includes(item.sender)"
              />
            </ElSelect>
            <ElButton :disabled="busy || !validGrant" @click="readMessageCandidates"
              >读取近期消息</ElButton
            >
          </ElFormItem>
          <ElFormItem v-if="action === 'member_nickname'" label="群内昵称">
            <ElInput
              v-model="text"
              :maxlength="workspace === 'lajiaoliao' ? 15 : 30"
              :disabled="busy"
            />
          </ElFormItem>
          <ElFormItem v-if="action === 'group_info'" label="群名称">
            <ElInput v-model="text" :maxlength="20" :disabled="busy" />
          </ElFormItem>
          <ElButton type="primary" :disabled="!canRun" :loading="submitting" @click="run">
            <Play :size="16" />确认验证此动作
          </ElButton>
        </ElForm>
      </div>
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <div v-if="pending || result" class="tools-result">
        <strong>{{ resultText }}</strong>
        <p v-if="effect" class="tools-effect">{{ effect }}</p>
        <p v-if="result?.result_code">{{ errorText(result.result_code) }}</p>
        <p class="tools-operation">操作：{{ result?.id || pending?.key }}</p>
        <div class="tools-toolbar">
          <ElButton :disabled="busy" @click="inspect()">查询原操作</ElButton>
          <ElButton v-if="result?.status === 'unknown'" :disabled="busy" @click="reconcile"
            >只读核对结果</ElButton
          >
        </div>
      </div>
      <div class="tools-history">
        <div class="tools-toolbar">
          <h4>本人操作记录</h4>
          <ElButton :disabled="busy || !accountId" @click="loadPreparation">刷新记录</ElButton>
        </div>
        <p>最多显示 50 条，未决操作优先；窗口关闭后仍可查询、只读核对。</p>
        <ElAlert
          v-if="historyPending"
          title="此账号有未决操作，请先查询并核对原结果。"
          type="warning"
          :closable="false"
        />
        <ElEmpty v-if="!history.length" description="暂无操作记录" :image-size="40" />
        <div v-for="op in history" :key="op.id" class="tools-history-row">
          <div>
            <strong>{{ actionName(op.kind) }} · {{ operationState(op.status) }}</strong>
            <p>{{ groupName(op.group_id) }} · {{ date(op.created_at) }}</p>
            <small class="tools-operation">{{ op.id }}</small>
          </div>
          <ElButton :disabled="busy" @click="inspect(op)">查询此操作</ElButton>
        </div>
      </div>
    </template>
    <ElDialog
      v-model="friendOpen"
      title="受控搜索加好友"
      width="620px"
      class="controlled-friend-dialog"
      :close-on-click-modal="false"
      :close-on-press-escape="!friendBusy"
      :show-close="!friendBusy"
      append-to-body
    >
      <p class="friend-scope">
        {{ workspace === 'lajiaoliao' ? '辣椒聊' : '旺商聊' }} ·
        {{ currentAccount?.native_nickname || currentAccount?.name }}
      </p>
      <ElButton
        v-if="friendGateAllowed"
        :icon="Play"
        :disabled="busy"
        @click="friendGateOpen = true"
        >发送准入</ElButton
      >
      <ElForm label-position="top" @submit.prevent>
        <ElFormItem label="隔离群组">
          <ElSelect v-model="friendGroup" aria-label="受控好友隔离群组" :disabled="friendBusy">
            <ElOption
              v-for="g in testGroups"
              :key="g.group_id"
              :value="g.group_id"
              :label="g.name"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="本人超管接入记录">
          <div class="friend-record">
            <ElInput
              :model-value="friend.challengeID"
              aria-label="受控好友接入记录"
              :maxlength="36"
              @update:model-value="setControlledFriendRecord"
            />
            <ElButton :icon="Plus" @click="setControlledFriendRecord('')"> 新接入记录 </ElButton>
            <ElButton
              :disabled="friendBusy || !validFriendID(friend.challengeID)"
              @click="readControlledFriend"
            >
              <RefreshCw :size="16" />读取原记录
            </ElButton>
          </div>
          <p class="friend-scope">
            切换仅选择记录；原窗口和提交状态保留，输入原 ID 可读回，不会续期或重发。
          </p>
        </ElFormItem>
        <template v-if="friend.challenge">
          <p class="friend-scope">
            {{ friend.challenge.customer?.name }} · 版本 {{ friend.challenge.version }} ·
            {{ date(friend.challenge.expires_at) }}
          </p>
          <p v-if="friendApplicant" class="friend-scope" aria-label="受控好友申请方向">
            申请账号 {{ friend.challenge.controlled_scope.claimant.name }} → 接收机器人
            {{ friend.challenge.controlled_scope.robot.name }} ·
            {{ friend.challenge.controlled_scope.robot.native_id }}
          </p>
          <div class="friend-fields">
            <ElFormItem label="目标可搜索账号">
              <ElInput
                v-model="friend.account"
                aria-label="受控好友搜索账号"
                :maxlength="64"
                :disabled="friendBusy || !!friend.window"
              />
            </ElFormItem>
            <ElFormItem label="目标原生 ID">
              <ElInput
                v-model="friend.target"
                aria-label="受控好友目标原生ID"
                :maxlength="200"
                :disabled="friendBusy || !!friend.window || friendApplicant"
              />
            </ElFormItem>
          </div>
          <template v-if="!friend.window">
            <ElCheckbox v-model="friend.isolated" :disabled="friendBusy">
              确认此群为自有隔离测试群
            </ElCheckbox>
            <p class="friend-scope">原生调试授权 · 最长 300 秒 · 一次发送尝试</p>
            <ElButton
              type="primary"
              :disabled="!canCreateFriendWindow || busy"
              @click="createControlledFriendWindow"
            >
              <Plus :size="16" />创建本次授权
            </ElButton>
          </template>
          <div v-if="friendApplicant" class="friend-window" aria-label="受控接入证明窗口">
            <h4>受控接入证明</h4>
            <p class="friend-scope">
              私聊身份：{{ friend.challenge.identity_proven ? '已核验' : '等待真实私聊证据' }}
            </p>
            <div v-for="kind in proofKinds" :key="kind" class="friend-window">
              <ElFormItem :label="`${proofName(kind)}证明原文`">
                <ElInput
                  :model-value="controlledProofText(kind)"
                  :aria-label="`${proofName(kind)}证明原文`"
                  type="textarea"
                  :autosize="{ minRows: 2, maxRows: 4 }"
                  :maxlength="100"
                  :placeholder="
                    kind === 'private'
                      ? '粘贴普通向导的完整「群接入」挑战'
                      : '粘贴普通向导的完整「群绑定」挑战'
                  "
                  :disabled="friendBusy || !!controlledProof(kind)?.key"
                  @update:model-value="setControlledProofText(kind, $event)"
                />
              </ElFormItem>
              <p class="friend-scope">
                {{ friend.challenge.controlled_scope.claimant.name }} →
                {{
                  kind === 'private'
                    ? friend.challenge.controlled_scope.robot.name
                    : friend.challenge.controlled_scope.group.name
                }}
              </p>
              <ElButton
                :disabled="busy || !canCreateControlledProof(kind)"
                @click="createControlledProof(kind)"
              >
                <Plus :size="16" />{{
                  controlledProof(kind)?.key && !controlledProof(kind)?.window
                    ? `重试原${proofName(kind)}窗口请求`
                    : `创建${proofName(kind)}证明窗口`
                }}
              </ElButton>
              <div v-if="controlledProof(kind)?.window" :aria-label="`${proofName(kind)}证明授权`">
                <ElFormItem :label="`${proofName(kind)}证明窗口 ID`">
                  <ElInput
                    :model-value="controlledProof(kind)!.window!.id"
                    :aria-label="`${proofName(kind)}证明窗口ID`"
                    readonly
                  />
                </ElFormItem>
                <p class="friend-scope">
                  发送账号：{{ controlledProof(kind)!.window!.account_id }} · 原生群：{{
                    controlledProof(kind)!.window!.native_group
                  }}
                </p>
                <p class="friend-scope">
                  {{ controlledProofState(kind) }} · 创建时剩余
                  {{ controlledProof(kind)!.window!.remaining }} 次 ·
                  {{ date(controlledProof(kind)!.window!.expires_at) }} 到期
                </p>
                <p class="friend-scope">本次仅创建窗口，未发送证明消息。</p>
                <ElButton :disabled="busy" @click="openControlledProofMessages(kind)">
                  进入该账号消息中心
                </ElButton>
              </div>
              <ElAlert
                v-else-if="controlledProof(kind)?.key"
                title="原窗口请求待核对，重试沿用原请求。"
                type="warning"
                :closable="false"
              />
            </div>
          </div>
        </template>
      </ElForm>
      <div v-if="friendWindow" class="friend-window" aria-label="受控好友授权">
        <p class="friend-scope">
          {{ grantState(friendWindow) }} · {{ usedCount(friendWindow) }} /
          {{ friendWindow.budget }} 次 · {{ date(friendWindow.expires_at) }} 到期
        </p>
        <p class="friend-scope">窗口：{{ friendWindow.id }}</p>
        <div class="tools-toolbar">
          <ElButton
            :disabled="busy || !canPrepareControlledFriend"
            @click="prepareControlledFriend"
          >
            <Search :size="16" />搜索授权目标
          </ElButton>
          <ElButton
            v-if="!friendWindow.cancelled"
            type="danger"
            link
            :disabled="busy"
            @click="cancelControlledFriendWindow"
            >取消本次授权</ElButton
          >
        </div>
      </div>
      <div
        v-if="friend.challenge?.friend_outbound"
        class="friend-window"
        aria-label="受控好友原操作"
      >
        <p class="friend-scope">
          {{ friend.challenge.friend_outbound.target.name }} ·
          {{ friend.challenge.friend_outbound.target.native_id }} ·
          {{ friendState(friend.challenge.friend_outbound.status) }}
        </p>
        <p class="friend-scope">操作：{{ friend.challenge.friend_outbound.id }}</p>
        <p
          v-if="
            friend.challenge.friend_outbound.code &&
            (friend.challenge.friend_outbound.code !== 'native_business_rejected' ||
              friendSubmitDiagnostic)
          "
          class="friend-scope"
        >
          {{ controlledFriendError(friend.challenge.friend_outbound.code) }}
        </p>
        <div v-if="friendSubmitDiagnostic" aria-label="受控好友提交诊断">
          <p class="friend-scope">失败阶段：好友申请</p>
          <p class="friend-scope">上游返回码：{{ friendSubmitDiagnostic.providerCode }}</p>
          <p class="friend-scope">
            采样时间：<time :datetime="friendSubmitDiagnostic.observedAt">{{
              date(friendSubmitDiagnostic.observedAt)
            }}</time>
          </p>
          <p class="friend-scope">此诊断仅供排查，不代表授权或提交结论。</p>
        </div>
        <p v-else-if="friendHistoricalSubmitFailure" class="friend-scope">
          此历史记录未保留具体上游返回码，无法还原；不会自动重发
        </p>
        <ElButton
          v-if="friend.challenge.friend_outbound.can_reconcile"
          :disabled="busy"
          @click="reconcileControlledFriend"
          ><RefreshCw :size="16" />只读核对原结果</ElButton
        >
      </div>
      <ElAlert
        v-if="
          friend.sendKey &&
          ['prepared', 'submitting', 'unknown'].includes(
            friend.challenge?.friend_outbound?.status || 'prepared'
          )
        "
        title="原请求尚待核对，请读取原记录或只读核对结果。"
        type="warning"
        :closable="false"
      />
      <ElAlert v-if="friend.error" :title="friend.error" type="error" :closable="false" />
      <template #footer>
        <div class="friend-actions">
          <ElButton :disabled="friendBusy" @click="friendOpen = false">关闭</ElButton>
          <ElButton
            type="primary"
            :disabled="busy || !canSendControlledFriend"
            @click="sendControlledFriend"
          >
            <Play :size="16" />确认发送好友申请
          </ElButton>
        </div>
      </template>
    </ElDialog>
    <OnboardingFriendGate
      v-if="friendGateAllowed && currentAccount"
      :open="friendGateOpen && friendOpen"
      :account="currentAccount"
      :workspace="workspace"
      :owner="session.user!.id"
      outbound
      @close="friendGateOpen = false"
    />
  </section>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElCheckbox,
    ElDialog,
    ElEmpty,
    ElForm,
    ElFormItem,
    ElInput,
    ElInputNumber,
    ElMessageBox,
    ElOption,
    ElSelect,
    ElSwitch,
    type ElMessageBoxOptions
  } from 'element-plus'
  import { VideoPlay as Play, Plus, Refresh as RefreshCw, Search } from '@element-plus/icons-vue'
  import { api, errorText } from './api'
  import type { NativeModerationSnapshot } from './api'
  import { useSession } from './store'
  import {
    controlledOnboardingActive,
    controlledOnboardingScope,
    robotAddAccount
  } from './customer'
  import ControlledOnboarding from './ControlledOnboarding.vue'
  import OnboardingFriendGate from './OnboardingFriendGate.vue'
  import type { Workspace } from './workspaces'

  type Row = Record<string, any>
  const props = defineProps<{
    workspace: Workspace
    account: string
    accounts: Row[]
    groups: Row[]
    windows: Row[]
  }>()
  const supportedWorkspace = computed(() => ['wsl', 'lajiaoliao'].includes(props.workspace))
  const emit = defineEmits(['create', 'cancel', 'updated', 'onboard'])
  const session = useSession()
  const accountId = ref(props.account || '')
  const onboardingGroups = ref<Row[]>([])
  const testGroups = computed(() => {
    const groups = new Map(
      props.groups
        .filter((item) => !item.platform || item.platform === props.workspace)
        .map((item) => [item.group_id, item])
    )
    for (const item of onboardingGroups.value)
      if (!groups.has(item.id))
        groups.set(item.id, { ...item, group_id: item.id, platform: props.workspace })
    return [...groups.values()]
  })
  const windowId = ref(''),
    action = ref(''),
    target = ref(''),
    text = ref(''),
    minutes = ref(1)
  const reading = ref(false),
    submitting = ref(false),
    confirming = ref(false),
    loadingPreparation = ref(false),
    error = ref('')
  const snapshot = ref<NativeModerationSnapshot | null>(null),
    result = ref<Row | null>(null),
    pending = ref<Row | null>(null)
  const invitations = ref<Row | null>(null)
  const messageCandidates = ref<Row[]>([]),
    messageEvent = ref('')
  const membership = ref<Row | null>(null)
  const observedUsed = ref<Record<string, number>>({})
  const usedCount = (window: Row) => Math.max(window.used, observedUsed.value[window.id] || 0)
  const receiver = ref<Row | null>(null),
    history = ref<Row[]>([]),
    historyLoaded = ref(false),
    preparationGroup = ref(''),
    roleObservation = ref<Row | null>(null),
    preparationError = ref(''),
    effect = ref('')
  const kinds = [
    'mute_member',
    'unmute_member',
    'mute_group',
    'unmute_group',
    'kick_member',
    'kick_member_ban',
    'remove_group_ban',
    'publish_notice',
    'grant_admin',
    'revoke_admin',
    'member_nickname',
    'group_invite',
    'friend_delete',
    'friend_approve',
    'group_accept',
    'group_leave',
    'group_info',
    'send_text',
    'recall_message'
  ]
  const labels: Record<string, string> = {
    mute_member: '成员禁言',
    unmute_member: '成员解禁',
    mute_group: '全群禁言',
    unmute_group: '解除全群禁言',
    kick_member: '移出成员',
    kick_member_ban: '移出并永久禁止申请进群',
    remove_group_ban: '解除原生进群限制',
    publish_notice: '发布公告',
    grant_admin: '设置群管理员',
    revoke_admin: '撤销群管理员',
    member_nickname: '修改群内昵称',
    group_invite: '拉入群组',
    friend_delete: '删除好友',
    friend_approve: '同意指定好友申请',
    group_accept: '接受本人入群邀请',
    group_leave: '退出群组',
    group_info: '修改群名称',
    send_text: '发送群文本',
    recall_message: '撤回消息',
    send_private_text: '私信文本（待验收）',
    send_private_image: '私信图片（待验收）',
    recall: '撤回（待接入）'
  }
  const actionName = (kind: string) => labels[kind] || kind
  const roleName = (role = '') =>
    ({ owner: '群主', admin: '管理员', member: '普通成员' })[role] || '未核验'
  const date = (value: string) =>
    value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
  const groupName = (id: string) => testGroups.value.find((g) => g.group_id === id)?.name || id
  const currentAccount = computed(() => props.accounts.find((a) => a.id === accountId.value))
  const proofKinds = ['private', 'group'] as const
  type ProofKind = (typeof proofKinds)[number]
  type ControlledProofDraft = { key: string; version: number; text: string; window: Row | null }
  type ControlledFriendDraft = {
    challengeID: string
    account: string
    target: string
    isolated: boolean
    challenge: Row | null
    window: Row | null
    prepareKey: string
    sendKey: string
    proofs: Record<string, ControlledProofDraft>
    error: string
  }
  const newFriendDraft = (): ControlledFriendDraft => ({
    challengeID: '',
    account: '',
    target: '',
    isolated: false,
    challenge: null,
    window: null,
    prepareKey: '',
    sendKey: '',
    proofs: {},
    error: ''
  })
  const friendOpen = ref(false),
    friendGateOpen = ref(false),
    friendBusy = ref(false),
    friendGroup = ref('')
  const friendGateAllowed = computed(
    () =>
      session.user?.role === 'superadmin' &&
      session.console === 'admin' &&
      session.workspace === props.workspace &&
      currentAccount.value?.platform === props.workspace &&
      currentAccount.value?.purpose === 'management'
  )
  const friend = ref<ControlledFriendDraft>(newFriendDraft())
  const friendSubmitDiagnostic = computed(() => {
    const op = friend.value.challenge?.friend_outbound
    if (!['failed', 'unknown'].includes(op?.status)) return null
    const value: unknown = op?.submit_diagnostic
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null
    const item = value as Record<string, unknown>
    const fields = ['version', 'platform', 'stage', 'category', 'provider_code', 'observed_at']
    if (
      Object.keys(item).length !== fields.length ||
      Object.keys(item).some((key) => !fields.includes(key)) ||
      item.version !== 1 ||
      (item.platform !== 'wsl' && item.platform !== 'lajiaoliao') ||
      item.platform !== props.workspace ||
      item.stage !== 'friend_apply' ||
      ![
        'upstream_rejected',
        'rate_limited',
        'session_expired',
        'transport_unavailable',
        'response_invalid'
      ].includes(item.category as string) ||
      typeof item.provider_code !== 'number' ||
      !Number.isInteger(item.provider_code) ||
      item.provider_code < 1 ||
      item.provider_code > 9999 ||
      typeof item.observed_at !== 'string' ||
      item.observed_at.length > 64 ||
      !/^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])[Tt]([01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:[Zz]|[+-]([01]\d|2[0-3]):[0-5]\d)$/.test(
        item.observed_at
      ) ||
      !Number.isFinite(Date.parse(item.observed_at))
    )
      return null
    const observedDay = item.observed_at.slice(0, 10)
    if (new Date(`${observedDay}T00:00:00Z`).toISOString().slice(0, 10) !== observedDay) return null
    return { providerCode: item.provider_code, observedAt: item.observed_at }
  })
  const friendHistoricalSubmitFailure = computed(() => {
    const op = friend.value.challenge?.friend_outbound
    return (
      op?.status === 'failed' &&
      !friendSubmitDiagnostic.value &&
      [
        'native_business_rejected',
        'native_http_rejected',
        'native_rate_limited',
        'native_reauth_required',
        'native_transport_unknown',
        'native_response_invalid'
      ].includes(op.code)
    )
  })
  const friendDrafts = new Map<string, ControlledFriendDraft>()
  const friendScope = computed(
    () => `${session.user?.id}:${props.workspace}:${accountId.value}:${friendGroup.value}`
  )
  let friendGeneration = 0
  const friendWindow = computed(
    () => props.windows.find((w) => w.id === friend.value.window?.id) || friend.value.window
  )
  const isControlledFriendWindow = (w: Row) =>
    w.actions?.includes('onboarding_friend_search') ||
    w.actions?.includes('onboarding_friend_request')
  const validFriendID = (id: string) =>
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)
  const scopedWindows = computed(() =>
    props.windows.filter((w) => w.account_ids.includes(accountId.value))
  )
  const grant = computed(() => scopedWindows.value.find((w) => w.id === windowId.value))
  const deleteGrantValid = computed(() => {
    const w = grant.value
    return (
      session.user?.role === 'superadmin' &&
      w?.admission_mode === 'native_debug' &&
      w.actions?.length === 1 &&
      w.actions[0] === 'friend_delete' &&
      w.budget === 1 &&
      w.account_ids?.length === 1 &&
      w.target_ids?.length === 1
    )
  })
  const approveGrantValid = computed(() => {
    const w = grant.value
    return (
      session.user?.role === 'superadmin' &&
      w?.admission_mode === 'native_debug' &&
      w.actions?.length === 1 &&
      w.actions[0] === 'friend_approve' &&
      w.budget === 1 &&
      w.account_ids?.length === 1 &&
      w.target_ids?.length === 1 &&
      !!w.friend_application_id
    )
  })
  const now = ref(Date.now())
  const timer = setInterval(() => (now.value = Date.now()), 1000)
  const validGrant = computed(
    () =>
      grant.value &&
      !grant.value.cancelled &&
      new Date(grant.value.expires_at).getTime() > now.value + 3000
  )
  const busy = computed(
    () =>
      reading.value ||
      submitting.value ||
      confirming.value ||
      loadingPreparation.value ||
      friendBusy.value
  )
  const unresolved = (status: string) => ['preparing', 'submitting', 'unknown'].includes(status)
  const historyPending = computed(() => history.value.some((op) => unresolved(op.status)))
  const receiverText = computed(() => {
    if (!receiver.value) return '尚未检查'
    if (!receiver.value.enabled) return '未启用'
    if (!receiver.value.observed) return '等待连接或状态已陈旧'
    return receiver.value.snapshot?.state === 'connected' ? '已连接' : '连接尚未就绪'
  })
  const freshSnapshot = computed(() => {
    const sampled = new Date(snapshot.value?.sampled_at || '').getTime()
    return !!snapshot.value && sampled <= now.value && sampled > now.value - 30_000
  })
  const freshMembership = computed(() => {
    const sampled = new Date(membership.value?.sampled_at || '').getTime()
    return !!membership.value && sampled <= now.value && sampled > now.value - 30_000
  })
  const membershipKinds = [
    'group_invite',
    'group_leave',
    'group_accept',
    'friend_delete',
    'friend_approve'
  ]
  const pendingFriendApplication = computed(() => {
    const applications = membership.value?.applications
    if (!Array.isArray(applications)) return
    if (
      props.workspace === 'lajiaoliao' &&
      applications.filter(
        (row: Row) =>
          row.user?.id === grant.value?.target_ids?.[0] &&
          row.direction === 'FROM_HIM' &&
          row.state === 'FRIENDLOG_STATE_PROCESSING'
      ).length !== 1
    )
      return
    return applications.find(
      (row: Row) =>
        row.id === grant.value?.friend_application_id &&
        row.user?.id === grant.value?.target_ids?.[0] &&
        row.user?.id !== membership.value?.self_id &&
        row.direction === 'FROM_HIM' &&
        row.state === 'FRIENDLOG_STATE_PROCESSING'
    )
  })
  const completeFriends = computed(() => {
    const view = membership.value
    if (view?.friends_known !== true || !Array.isArray(view.friends)) return false
    const ids = view.friends.map((friend: Row) => friend?.id)
    return (
      ids.every((id: unknown) => typeof id === 'string' && !!id && id === id.trim()) &&
      new Set(ids).size === ids.length
    )
  })
  const selfInvitations = computed(() =>
    (membership.value?.invitations || []).filter(
      (item: Row) =>
        item.applicant?.id === membership.value?.self_id &&
        item.group_id === membership.value?.group_id &&
        item.type === 'INVITATION' &&
        item.state === 'MEMBER_STATE_INVITED'
    )
  )
  const membershipAction = computed(() => membershipKinds.includes(action.value))
  const hasMembership = computed(() =>
    grant.value?.actions.some((a: string) => membershipKinds.includes(a))
  )
  const hasModeration = computed(() =>
    grant.value?.actions.some((a: string) => kinds.includes(a) && !membershipKinds.includes(a))
  )
  function eligibleTarget(id: string) {
    if (action.value === 'friend_approve')
      return approveGrantValid.value && pendingFriendApplication.value?.user.id === id
    if (action.value === 'friend_delete')
      return (
        completeFriends.value &&
        !!membership.value?.self_id &&
        id !== membership.value.self_id &&
        membership.value.friends.some((friend: Row) => friend.id === id)
      )
    if (action.value === 'remove_group_ban')
      return (
        !!snapshot.value?.bans_known &&
        id !== snapshot.value?.self_id &&
        snapshot.value?.group_bans?.some((row) => row.userID === id)
      )
    if (action.value === 'kick_member_ban' && !snapshot.value?.bans_known) return false
    if (action.value === 'group_invite') {
      const view = membership.value
      return (
        !!view?.friends_known &&
        !!view?.members_known &&
        id !== view.self_id &&
        !view.member_ids.includes(id) &&
        view.friends.some((f: Row) => f.id === id)
      )
    }
    const member = snapshot.value?.members?.find((m) => m.id === id)
    if (action.value === 'member_nickname' && id === snapshot.value?.self_id)
      return !!member && ['member', 'admin', 'owner'].includes(member.role)
    if (action.value === 'grant_admin' || action.value === 'revoke_admin')
      return (
        !!member &&
        id !== snapshot.value?.self_id &&
        snapshot.value?.self_role === 'owner' &&
        member.role === (action.value === 'grant_admin' ? 'member' : 'admin')
      )
    return (
      !!member &&
      id !== snapshot.value?.self_id &&
      (member.role === 'member' ||
        (member.role === 'admin' && snapshot.value?.self_role === 'owner'))
    )
  }
  const availableActions = computed(() =>
    (grant.value?.actions || []).filter(
      (a: string) =>
        (!grant.value?.actions.includes('friend_delete') || deleteGrantValid.value) &&
        (!grant.value?.actions.includes('friend_approve') || approveGrantValid.value) &&
        kinds.includes(a) &&
        (props.workspace === 'lajiaoliao' || !['send_text', 'recall_message'].includes(a))
    )
  )
  const requiresTarget = computed(() =>
    [
      'mute_member',
      'unmute_member',
      'kick_member',
      'kick_member_ban',
      'remove_group_ban',
      'group_invite',
      'friend_delete',
      'friend_approve',
      'grant_admin',
      'revoke_admin',
      'member_nickname'
    ].includes(action.value)
  )
  const actionEvidence = computed(() => {
    if (action.value === 'send_text') {
      const view = snapshot.value
      return (
        freshSnapshot.value &&
        view?.in_group === true &&
        ['member', 'admin', 'owner'].includes(view.self_role || '') &&
        grant.value?.target_ids.includes(view.self_id)
      )
    }
    if (!membershipAction.value)
      return (
        freshSnapshot.value &&
        (action.value === 'member_nickname' && target.value === snapshot.value?.self_id
          ? ['member', 'admin', 'owner'].includes(snapshot.value?.self_role || '')
          : ['grant_admin', 'revoke_admin'].includes(action.value)
            ? snapshot.value?.self_role === 'owner'
            : ['admin', 'owner'].includes(snapshot.value?.self_role || ''))
      )
    const view = membership.value
    if (action.value === 'friend_delete' || action.value === 'friend_approve') {
      const group = testGroups.value.find((g) => g.group_id === grant.value?.group_id)
      return (
        (action.value === 'friend_delete'
          ? deleteGrantValid.value && completeFriends.value
          : approveGrantValid.value && !!pendingFriendApplication.value) &&
        freshMembership.value &&
        view?.directory_known === true &&
        view.members_known === true &&
        view.in_group === true &&
        ['admin', 'owner'].includes(view.self_role) &&
        !!view.self_id &&
        Array.isArray(view.member_ids) &&
        view.member_ids.includes(view.self_id) &&
        !!group?.native_id &&
        view.group_id === group.native_id &&
        (!view.platform || view.platform === props.workspace) &&
        view.account_epoch === currentAccount.value?.epoch &&
        view.account_version === currentAccount.value?.version
      )
    }
    if (action.value === 'group_accept')
      return (
        freshMembership.value &&
        view?.directory_known &&
        !view.in_group &&
        view.invitations_known &&
        grant.value?.target_ids.includes(view.self_id) &&
        selfInvitations.value.some((item: Row) => item.id === text.value)
      )
    if (!freshMembership.value || !view?.directory_known || !view.members_known || !view.in_group)
      return false
    if (action.value === 'group_invite') return ['admin', 'owner'].includes(view.self_role)
    const group = testGroups.value.find((g) => g.group_id === grant.value?.group_id)
    return (
      ['member', 'admin'].includes(view.self_role) &&
      grant.value?.target_ids.includes(view.self_id) &&
      group &&
      group.management_enabled === false &&
      group.ai_enabled === false
    )
  })
  const canRun = computed(
    () =>
      validGrant.value &&
      session.user?.role === 'superadmin' &&
      grant.value &&
      usedCount(grant.value) < grant.value.budget &&
      currentAccount.value?.testing_enabled &&
      currentAccount.value?.connection_enabled &&
      !busy.value &&
      !pending.value &&
      !historyPending.value &&
      historyLoaded.value &&
      actionEvidence.value &&
      availableActions.value.includes(action.value) &&
      (action.value !== 'mute_member' ||
        (Number.isInteger(minutes.value) && minutes.value >= 1 && minutes.value <= 5)) &&
      (!requiresTarget.value || eligibleTarget(target.value)) &&
      (action.value !== 'member_nickname' ||
        (Array.from(text.value).length <= 15 && text.value === text.value.trim())) &&
      (!['publish_notice', 'send_text'].includes(action.value) || text.value.trim()) &&
      (action.value !== 'send_text' || Array.from(text.value).length <= 2000) &&
      (action.value !== 'group_info' ||
        (!!text.value && Array.from(text.value).length <= 20 && !/\s/.test(text.value))) &&
      (action.value !== 'recall_message' ||
        messageCandidates.value.some(
          (item) =>
            item.event_id === messageEvent.value && grant.value?.target_ids.includes(item.sender)
        ))
  )
  const resultText = computed(
    () =>
      (result.value?.result_code === 'desired_state_observed'
        ? '只读核对已观察到期望状态，未重复提交'
        : '') ||
      {
        succeeded: '上游已确认操作',
        failed: '本次操作失败',
        unknown: '结果未知，禁止重复提交',
        preparing: '正在准备提交',
        submitting: '提交中'
      }[String(result.value?.status)] ||
      '请求状态待查询，请勿重复提交'
  )
  const cacheKey = () => `wsl:test-tool:${session.user?.id}:${props.workspace}:${accountId.value}`
  let generation = 0
  type ConfirmationKind = 'action' | 'friend' | 'account'
  let activeConfirmation: { kind: ConfirmationKind; cancel: () => void } | null = null
  function confirmAction(
    kind: ConfirmationKind,
    message: string,
    title: string,
    options: ElMessageBoxOptions
  ) {
    return new Promise<void>((resolve, reject) => {
      let settled = false
      const finish = (accepted: boolean) => {
        if (settled) return
        settled = true
        if (activeConfirmation === confirmation) activeConfirmation = null
        if (accepted) resolve()
        else reject('cancel')
      }
      const confirmation = {
        kind,
        cancel: () => {
          if (settled) return
          // Element Plus close() hides the box without settling its Promise.
          finish(false)
          ElMessageBox.close()
        }
      }
      activeConfirmation = confirmation
      try {
        void ElMessageBox.confirm(message, title, options).then(
          () => finish(true),
          () => finish(false)
        )
      } catch {
        finish(false)
      }
    })
  }
  function savePending() {
    try {
      if (pending.value) sessionStorage.setItem(cacheKey(), JSON.stringify(pending.value))
      else sessionStorage.removeItem(cacheKey())
    } catch {
      // Server history also retains submitted operations if browser storage is unavailable.
    }
  }
  function restorePending() {
    try {
      const value = JSON.parse(sessionStorage.getItem(cacheKey()) || 'null')
      pending.value =
        value?.account === accountId.value &&
        typeof value.created === 'number' &&
        (typeof value.key === 'string' || typeof value.id === 'string')
          ? value
          : null
    } catch {
      pending.value = null
    }
  }
  function reset() {
    activeConfirmation?.cancel()
    generation++
    windowId.value = ''
    action.value = ''
    target.value = ''
    text.value = ''
    snapshot.value = null
    membership.value = null
    invitations.value = null
    messageCandidates.value = []
    messageEvent.value = ''
    result.value = null
    receiver.value = null
    history.value = []
    historyLoaded.value = false
    preparationGroup.value = ''
    roleObservation.value = null
    preparationError.value = ''
    effect.value = ''
    error.value = ''
    reading.value = false
    submitting.value = false
    confirming.value = false
    loadingPreparation.value = false
    restorePending()
    void loadPreparation()
  }
  function selectWindow(id: string) {
    if (busy.value) return
    const selected = scopedWindows.value.find((w) => w.id === id)
    if (selected && isControlledFriendWindow(selected)) {
      openControlledFriend(selected)
      return
    }
    generation++
    windowId.value = id
    preparationGroup.value = grant.value?.group_id || ''
    snapshot.value = null
    membership.value = null
    invitations.value = null
    messageCandidates.value = []
    messageEvent.value = ''
    target.value = ''
    text.value = ''
    action.value = availableActions.value[0] || ''
    error.value = ''
    effect.value = ''
    if (!pending.value) result.value = null
  }
  const operationState = (status: string) =>
    ({
      succeeded: '已确认',
      failed: '失败',
      unknown: '结果未知',
      preparing: '准备中',
      submitting: '提交中'
    })[status] || status
  const memberName = (id: string) => {
    if (action.value === 'friend_approve') {
      const user = membership.value?.applications?.find(
        (row: Row) => row.id === grant.value?.friend_application_id && row.user?.id === id
      )?.user
      return `${user?.name || id} · ${id}`
    }
    const member =
      action.value === 'remove_group_ban'
        ? snapshot.value?.group_bans?.find((m) => m.userID === id)
        : ['group_invite', 'friend_delete'].includes(action.value)
          ? membership.value?.friends?.find((m: Row) => m.id === id)
          : snapshot.value?.members?.find((m) => m.id === id)
    return `${member?.nickname || member?.name || id} · ${id}`
  }
  const grantState = (w: Row) =>
    w.cancelled
      ? '已取消'
      : new Date(w.expires_at).getTime() <= now.value
        ? '已过期'
        : usedCount(w) >= w.budget
          ? '次数已用完'
          : '有效'
  const friendState = (status: string) =>
    ({
      prepared: '目标已确认',
      submitting: '提交中',
      submitted: '申请已提交',
      friends: '已观察到好友关系',
      failed: '本次尝试失败',
      unknown: '结果未知'
    })[status] || status
  const controlledFriendError = (code: string) =>
    ({
      onboarding_controlled_challenge_required: '需要本人创建的超管接入记录',
      onboarding_controlled_target_changed: '目标与授权范围不一致',
      group_binding_outbound_controlled_only: '此操作仅限独立受控授权',
      group_binding_friend_flow_retired: '此记录不允许执行出向操作',
      native_friend_request_acknowledged: '原生已确认收到申请',
      native_friend_request_rejected: '原生已拒绝本次申请',
      fresh_test_account_role_required: '账号当前群角色未通过核验'
    })[code] || errorText(code)
  function controlledFriendClaimant(c: Row) {
    if (c.robot?.id === accountId.value) return null
    try {
      const scope = controlledOnboardingScope(c.controlled_scope)
      return controlledOnboardingActive(scope, now.value + 4000) &&
        scope.actor_id === session.user?.id &&
        scope.platform === props.workspace &&
        scope.claimant.id === accountId.value &&
        scope.group.id === friendGroup.value &&
        scope.robot.id === c.robot?.id &&
        scope.robot.native_id === c.robot?.native_id
        ? scope
        : null
    } catch {
      return null
    }
  }
  const friendApplicant = computed(
    () => !!friend.value.challenge && friend.value.challenge.robot?.id !== accountId.value
  )
  const friendClaimantScope = computed(() =>
    friend.value.challenge ? controlledFriendClaimant(friend.value.challenge) : null
  )
  const friendRecordActive = computed(() => {
    const c = friend.value.challenge
    return (
      session.user?.role === 'superadmin' &&
      c &&
      c.id === friend.value.challengeID &&
      c.platform === props.workspace &&
      c.scope?.actor_id === session.user?.id &&
      (c.robot?.id === accountId.value || friendClaimantScope.value) &&
      c.mode === 'operator_demo' &&
      new Date(c.expires_at).getTime() > now.value + 4000
    )
  })
  const friendChallengeActive = computed(() => {
    const c = friend.value.challenge
    return (
      friendRecordActive.value &&
      (!friendApplicant.value ||
        friend.value.target === friendClaimantScope.value?.robot.native_id) &&
      c!.friend_flow === 'incoming_contact' &&
      c!.state === 'waiting' &&
      !c!.identity_proven
    )
  })
  const friendWindowActive = computed(() => {
    const w = friendWindow.value,
      c = friend.value.challenge
    return (
      friendChallengeActive.value &&
      w &&
      !w.cancelled &&
      usedCount(w) < w.budget &&
      w.group_id === friendGroup.value &&
      w.account_ids?.includes(accountId.value) &&
      w.admission_mode === 'native_debug' &&
      new Date(w.expires_at).getTime() > now.value + 4000 &&
      w.target_ids?.[0] === `onboarding_challenge:${c!.id}:${c!.version}` &&
      w.target_ids?.[1] === `onboarding_account:${friend.value.account}` &&
      w.target_ids?.[2] === `onboarding_target:${friend.value.target}` &&
      currentAccount.value?.testing_enabled &&
      currentAccount.value?.connection_enabled
    )
  })
  const canCreateFriendWindow = computed(
    () =>
      friendChallengeActive.value &&
      !friend.value.window &&
      !friend.value.challenge?.friend_outbound &&
      !friend.value.sendKey &&
      validFriendID(friendGroup.value) &&
      friend.value.isolated &&
      /^[A-Za-z0-9_-]{3,64}$/.test(friend.value.account) &&
      !!friend.value.target.trim() &&
      friend.value.target === friend.value.target.trim() &&
      currentAccount.value?.testing_enabled &&
      currentAccount.value?.connection_enabled
  )
  const canPrepareControlledFriend = computed(
    () =>
      friendWindowActive.value && !friend.value.sendKey && !friend.value.challenge?.friend_outbound
  )
  const canSendControlledFriend = computed(() => {
    const op = friend.value.challenge?.friend_outbound
    return (
      friendWindowActive.value &&
      !friend.value.sendKey &&
      op?.status === 'prepared' &&
      op.target?.native_id === friend.value.target &&
      op.target?.add_account === friend.value.account
    )
  })
  const proofName = (kind: ProofKind) => (kind === 'private' ? '私聊' : '群')
  const controlledProof = (kind: ProofKind) =>
    friend.value.proofs[`${friend.value.challengeID}:${kind}`]
  function setControlledFriendRecord(id: string) {
    if (id === friend.value.challengeID) return
    saveControlledFriendPending()
    if (activeConfirmation?.kind === 'friend') activeConfirmation.cancel()
    friendGeneration++
    friendBusy.value = false
    const proofs = friend.value.proofs
    friend.value = newFriendDraft()
    friend.value.proofs = proofs
    friend.value.challengeID = id
    if (validFriendID(id)) restoreControlledFriendPending()
    saveControlledFriendPending()
  }
  const controlledProofText = (kind: ProofKind) => controlledProof(kind)?.text || ''
  function setControlledProofText(kind: ProofKind, text: string) {
    const c = friend.value.challenge
    if (friendBusy.value || !c || controlledProof(kind)?.key) return
    const key = `${c.id}:${kind}`
    friend.value.proofs[key] ||= { key: '', version: c.version, text: '', window: null }
    friend.value.proofs[key].text = text
  }
  const controlledProofState = (kind: ProofKind) => {
    const window = controlledProof(kind)!.window!
    return window.challenge_version !== friend.value.challenge?.version
      ? '阶段已变化'
      : Date.parse(window.expires_at) <= now.value
        ? '已过期'
        : window.remaining < 1
          ? '次数已用完'
          : '已准备'
  }
  const canCreateControlledProof = (kind: ProofKind) => {
    const c = friend.value.challenge,
      proof = controlledProof(kind),
      text = controlledProofText(kind)
    return (
      friendRecordActive.value &&
      friendClaimantScope.value &&
      currentAccount.value?.testing_enabled &&
      currentAccount.value?.connection_enabled &&
      (kind === 'private' ? !c!.identity_proven : c!.identity_proven) &&
      (kind === 'private' ? /^群接入 [A-Za-z0-9_-]{43}$/ : /^群绑定 [A-Za-z0-9_-]{43}$/).test(
        text
      ) &&
      !proof?.window &&
      (!proof || (proof.version === c!.version && proof.text === text))
    )
  }
  function saveControlledFriendPending() {
    try {
      const key = `wsl:controlled-friend:${friendScope.value}`,
        value = JSON.stringify({
          challengeID: friend.value.challengeID,
          windowID: friendWindow.value?.id,
          window: friendWindow.value,
          prepareKey: friend.value.prepareKey,
          sendKey: friend.value.sendKey,
          ...(Object.keys(friend.value.proofs).length ? { proofs: friend.value.proofs } : {})
        })
      if (validFriendID(friend.value.challengeID))
        sessionStorage.setItem(`${key}:${friend.value.challengeID}`, value)
      sessionStorage.setItem(key, value)
    } catch {
      // The immutable server operation remains available by challenge ID.
    }
  }
  function restoreControlledFriendPending() {
    try {
      const key = `wsl:controlled-friend:${friendScope.value}`
      let value = JSON.parse(sessionStorage.getItem(key) || 'null')
      if (friend.value.challengeID && friend.value.challengeID !== value?.challengeID)
        value = JSON.parse(sessionStorage.getItem(`${key}:${friend.value.challengeID}`) || 'null')
      if (!value || !validFriendID(value.challengeID)) return
      if (friend.value.challengeID && friend.value.challengeID !== value.challengeID) return
      friend.value.challengeID = value.challengeID
      friend.value.prepareKey = typeof value.prepareKey === 'string' ? value.prepareKey : ''
      friend.value.sendKey = typeof value.sendKey === 'string' ? value.sendKey : ''
      if (value.proofs && typeof value.proofs === 'object' && !Array.isArray(value.proofs))
        for (const [key, entry] of Object.entries(value.proofs)) {
          const proof = entry as ControlledProofDraft,
            [challenge, kind] = key.split(':')
          if (
            validFriendID(challenge) &&
            proofKinds.includes(kind as ProofKind) &&
            proof &&
            typeof proof.key === 'string' &&
            Number.isSafeInteger(proof.version) &&
            proof.version > 0 &&
            typeof proof.text === 'string' &&
            (!proof.window || validFriendID(proof.window.id))
          )
            friend.value.proofs[key] = proof
        }
      const matches = (w: Row) =>
        w &&
        w.id === value.windowID &&
        w.group_id === friendGroup.value &&
        w.account_ids?.includes(accountId.value) &&
        w.target_ids?.[0]?.split(':')[1] === value.challengeID &&
        isControlledFriendWindow(w)
      const w = props.windows.find(matches) || (matches(value.window) ? value.window : null)
      if (w) hydrateControlledFriendWindow(w)
    } catch {
      friend.value.error = '本地原操作记录不可读，请读取原接入记录。'
    }
  }
  function hydrateControlledFriendWindow(w: Row) {
    friend.value.window = w
    friend.value.challengeID = w.target_ids?.[0]?.split(':')[1] || ''
    friend.value.account = w.target_ids?.[1]?.replace(/^onboarding_account:/, '') || ''
    friend.value.target = w.target_ids?.[2]?.replace(/^onboarding_target:/, '') || ''
  }
  function openControlledFriend(w?: Row) {
    if (busy.value || session.user?.role !== 'superadmin') return
    friendGroup.value = w?.group_id || friendGroup.value || preparationGroup.value || ''
    if (w && friend.value.window?.id !== w.id) {
      setControlledFriendRecord(w.target_ids?.[0]?.split(':')[1] || '')
      hydrateControlledFriendWindow(w)
      saveControlledFriendPending()
    }
    restoreControlledFriendPending()
    friendOpen.value = true
  }
  async function controlledFriendRequest(path: string, method = 'GET', body?: Row, key?: string) {
    const version = friendGeneration,
      scope = friendScope.value,
      workspace = props.workspace
    friendBusy.value = true
    friend.value.error = ''
    try {
      const data = await api<Row>(path, method, body, undefined, workspace, key)
      if (version !== friendGeneration || scope !== friendScope.value) return
      return data
    } catch (e) {
      if (version === friendGeneration && scope === friendScope.value)
        friend.value.error =
          controlledFriendError((e as { code?: string }).code || '') || (e as Error).message
    } finally {
      if (version === friendGeneration && scope === friendScope.value) friendBusy.value = false
    }
  }
  function acceptControlledFriend(c: Row) {
    const claimant = controlledFriendClaimant(c)
    if (
      c.id !== friend.value.challengeID ||
      c.platform !== props.workspace ||
      c.scope?.actor_id !== session.user?.id ||
      (c.robot?.id !== accountId.value && !claimant) ||
      c.mode !== 'operator_demo'
    ) {
      friend.value.challenge = null
      friend.value.error = '接入记录与当前超管、平台、账号或受控群范围不一致'
      return
    }
    if (claimant) {
      friend.value.target = claimant.robot.native_id
      if (!friend.value.window && !friend.value.account) friend.value.account = robotAddAccount(c)
      for (const kind of proofKinds) {
        const proof = controlledProof(kind)
        if (proof && !proof.key) proof.version = c.version
      }
    }
    friend.value.challenge = c
  }
  async function readControlledFriend() {
    if (busy.value || !validFriendID(friend.value.challengeID)) return
    const data = await controlledFriendRequest(
      `/group-binding/challenges/${friend.value.challengeID}`
    )
    if (data) acceptControlledFriend(data)
  }
  async function createControlledFriendWindow() {
    if (busy.value || !canCreateFriendWindow.value) return
    const draft = friend.value,
      group = friendGroup.value
    const data = await controlledFriendRequest('/test-tools/onboarding-friend/windows', 'POST', {
      challenge_id: draft.challengeID,
      version: draft.challenge!.version,
      group_id: group,
      account: draft.account,
      target_id: draft.target,
      seconds: 300,
      budget: 1,
      confirm_isolated: true,
      ...(friendClaimantScope.value ? { as_claimant: true } : {})
    })
    if (!data) return
    draft.window = {
      ...data,
      group_id: group,
      account_ids: [accountId.value],
      used: 0,
      budget: 1,
      actions: ['onboarding_friend_search', 'onboarding_friend_request'],
      target_ids: [
        `onboarding_challenge:${draft.challengeID}:${draft.challenge!.version}`,
        `onboarding_account:${draft.account}`,
        `onboarding_target:${draft.target}`
      ]
    }
    saveControlledFriendPending()
    emit('updated')
  }
  async function createControlledProof(kind: ProofKind) {
    if (busy.value || !canCreateControlledProof(kind)) return
    const draft = friend.value,
      c = draft.challenge!,
      scope = friendClaimantScope.value!,
      key = `${c.id}:${kind}`
    const proof = draft.proofs[key]
    proof.key ||= crypto.randomUUID()
    saveControlledFriendPending()
    const data = await controlledFriendRequest(
      `/test-tools/onboarding-friend/challenges/${c.id}/proof-windows`,
      'POST',
      { version: proof.version, kind, text: proof.text, seconds: 300 },
      proof.key
    )
    if (!data) return
    if (
      !validFriendID(data.id) ||
      data.account_id !== scope.claimant.id ||
      data.account_id !== accountId.value ||
      data.proof_kind !== kind ||
      data.challenge_version !== proof.version ||
      data.group_id !== scope.group.id ||
      data.native_group !== scope.group.native_id ||
      data.account_epoch !== currentAccount.value?.epoch ||
      !Number.isSafeInteger(data.binding_epoch) ||
      data.binding_epoch < 1 ||
      data.admission_mode !== 'native_debug' ||
      !Number.isFinite(Date.parse(data.expires_at)) ||
      !Number.isSafeInteger(data.remaining) ||
      data.remaining < 0 ||
      data.remaining > 1
    ) {
      draft.error = '证明窗口与当前申请账号、群或阶段不一致，请核对原记录。'
      return
    }
    proof.window = data
    saveControlledFriendPending()
    emit('updated')
  }
  function openControlledProofMessages(kind: ProofKind) {
    const window = controlledProof(kind)?.window
    if (
      busy.value ||
      !window ||
      session.user?.role !== 'superadmin' ||
      session.workspace !== props.workspace ||
      window.account_id !== accountId.value ||
      window.group_id !== friendGroup.value
    )
      return
    saveControlledFriendPending()
    session.selectAccount(window.account_id)
    friendOpen.value = false
    location.hash = 'messages'
  }
  async function prepareControlledFriend() {
    if (busy.value || !canPrepareControlledFriend.value) return
    friend.value.prepareKey ||= crypto.randomUUID()
    saveControlledFriendPending()
    const data = await controlledFriendRequest(
      `/test-tools/onboarding-friend/challenges/${friend.value.challengeID}/friend-target`,
      'POST',
      {
        test_window_id: friendWindow.value!.id,
        version: friend.value.challenge!.version,
        account: friend.value.account
      },
      friend.value.prepareKey
    )
    if (data) acceptControlledFriend(data)
  }
  async function sendControlledFriend() {
    if (busy.value || !canSendControlledFriend.value) return
    const version = friendGeneration,
      draft = friend.value,
      window = friendWindow.value!
    friendBusy.value = true
    try {
      await confirmAction(
        'friend',
        `${currentAccount.value?.name} · ${groupName(friendGroup.value)}：向 ${draft.account}（${draft.target}）发送一次好友申请，消耗一次授权次数。`,
        '确认受控好友申请',
        { confirmButtonText: '确认发送', cancelButtonText: '取消', type: 'warning' }
      )
    } catch {
      return
    } finally {
      if (version === friendGeneration) friendBusy.value = false
    }
    if (version !== friendGeneration || !canSendControlledFriend.value) return
    draft.sendKey = crypto.randomUUID()
    saveControlledFriendPending()
    const data = await controlledFriendRequest(
      `/test-tools/onboarding-friend/challenges/${draft.challengeID}/friend-send`,
      'POST',
      { test_window_id: window.id, version: draft.challenge!.version },
      draft.sendKey
    )
    if (version !== friendGeneration) return
    if (data) acceptControlledFriend(data)
    else draft.error ||= '请求结果待核对，请读取原记录。'
    observedUsed.value[window.id] = Math.max(usedCount(window), 1)
    emit('updated')
  }
  async function cancelControlledFriendWindow() {
    if (busy.value || !friendWindow.value || friendWindow.value.cancelled) return
    const window = friendWindow.value
    const data = await controlledFriendRequest(`/test-windows/${window.id}/cancel`, 'POST')
    if (!data) return
    window.cancelled = true
    emit('updated')
  }
  async function reconcileControlledFriend() {
    const c = friend.value.challenge,
      op = c?.friend_outbound
    if (busy.value || !op?.can_reconcile) return
    const data = await controlledFriendRequest(
      `/group-binding/challenges/${c!.id}/outbound-attempts/${op.id}/reconcile`,
      'POST',
      { version: c!.version },
      crypto.randomUUID()
    )
    if (data) acceptControlledFriend(data)
  }
  async function call<T = Row>(name: string, args: Row, key?: string) {
    return api<T>(
      '/test-tools/call',
      'POST',
      { name, arguments: args },
      undefined,
      props.workspace,
      key
    )
  }
  async function loadPreparation() {
    if (!supportedWorkspace.value || !accountId.value) return
    const version = generation,
      account = accountId.value,
      workspace = props.workspace
    loadingPreparation.value = true
    preparationError.value = ''
    const responses = await Promise.allSettled([
      api<Row>(`/accounts/${account}/receiver`, 'GET', undefined, undefined, workspace),
      api<Row[]>(
        `/test-tools/operations?account_id=${encodeURIComponent(account)}`,
        'GET',
        undefined,
        undefined,
        workspace
      )
    ])
    if (version !== generation) return
    const [connection, operations] = responses
    if (connection.status === 'fulfilled') receiver.value = connection.value
    else receiver.value = null
    historyLoaded.value = operations.status === 'fulfilled'
    if (operations.status === 'fulfilled') history.value = operations.value
    const failure = responses.find((response) => response.status === 'rejected')
    if (failure?.status === 'rejected') preparationError.value = failure.reason.message
    loadingPreparation.value = false
  }
  async function enableReceiver() {
    if (busy.value || !currentAccount.value || !receiver.value) return
    const version = generation,
      account = accountId.value,
      revision = receiver.value.revision
    submitting.value = true
    error.value = ''
    try {
      await api(
        `/accounts/${account}/receiver`,
        'PUT',
        { enabled: true, revision },
        undefined,
        props.workspace
      )
      if (version === generation) await loadPreparation()
    } catch (e) {
      if (version === generation) error.value = (e as Error).message
    } finally {
      if (version === generation) submitting.value = false
    }
  }
  async function observeRole() {
    if (busy.value || !preparationGroup.value) return
    const group = testGroups.value.find((g) => g.group_id === preparationGroup.value)
    if (!group?.native_id) return
    const version = generation
    reading.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        `/accounts/${accountId.value}/native-role`,
        'POST',
        { group_id: group.native_id },
        undefined,
        props.workspace
      )
      if (version === generation) roleObservation.value = data.observation
    } catch (e) {
      if (version === generation) error.value = (e as Error).message
    } finally {
      if (version === generation) reading.value = false
    }
  }
  async function readGroup() {
    const version = generation
    error.value = ''
    reading.value = true
    try {
      const data = await call<{ snapshot: NativeModerationSnapshot; scope?: Row }>(
        props.workspace === 'lajiaoliao' &&
          ['kick_member_ban', 'remove_group_ban'].includes(action.value)
          ? 'read_group_bans'
          : 'read_group',
        {
          account_id: accountId.value,
          test_window_id: windowId.value
        }
      )
      if (version === generation) {
        snapshot.value = data.snapshot
        recordRemaining(data.scope)
      }
    } catch (e) {
      if (version === generation) error.value = String((e as Error).message)
    } finally {
      if (version === generation) reading.value = false
    }
  }
  async function readMembership() {
    const version = generation
    error.value = ''
    reading.value = true
    try {
      const data = await call('read_membership', {
        account_id: accountId.value,
        test_window_id: windowId.value
      })
      if (version === generation) {
        membership.value = data.snapshot
        recordRemaining(data.scope)
      }
    } catch (e) {
      if (version === generation) error.value = (e as Error).message
    } finally {
      if (version === generation) reading.value = false
    }
  }
  function recordRemaining(scope?: Row) {
    if (grant.value && Number.isInteger(scope?.remaining) && scope!.remaining >= 0)
      observedUsed.value[grant.value.id] = Math.max(
        usedCount(grant.value),
        grant.value.budget - scope!.remaining
      )
  }
  async function readInvitations() {
    const version = generation
    error.value = ''
    reading.value = true
    try {
      const data = await call('read_invitations', {
        account_id: accountId.value,
        test_window_id: windowId.value
      })
      if (version === generation) invitations.value = data.snapshot
    } catch (e) {
      if (version === generation) error.value = (e as Error).message
    } finally {
      if (version === generation) reading.value = false
    }
  }
  async function run() {
    if (!canRun.value) return
    const version = generation,
      account = accountId.value,
      grantId = windowId.value,
      used = usedCount(grant.value!)
    const args: Row = { account_id: account, test_window_id: grantId, action: action.value }
    if (requiresTarget.value) args.target_id = target.value
    if (action.value === 'mute_member') args.minutes = minutes.value
    if (['publish_notice', 'member_nickname', 'send_text', 'group_info'].includes(action.value))
      args.text = text.value
    if (action.value === 'recall_message') args.event_id = messageEvent.value
    if (action.value === 'group_accept') {
      args.text = text.value
      args.target_id = membership.value?.self_id
    }
    if (action.value === 'friend_approve') args.text = grant.value?.friend_application_id
    confirming.value = true
    try {
      await confirmAction(
        'action',
        `${currentAccount.value?.native_nickname || currentAccount.value?.name} · ${groupName(grant.value?.group_id)}：${actionName(args.action)}${args.target_id ? `，目标 ${memberName(args.target_id)}` : ''}${args.minutes ? `，${args.minutes} 分钟` : ''}${args.text ? `，${args.action === 'friend_approve' ? '申请编号' : args.action === 'group_accept' ? '邀请记录' : '正文'}：${args.text}` : ''}。${args.action === 'friend_delete' ? '删除好友关系，不退群、不拉黑。' : args.action === 'friend_approve' ? '仅同意此申请，不认领群组、不启用业务。' : ''}消耗一次授权次数。`,
        args.action === 'friend_delete'
          ? '确认删除好友'
          : args.action === 'friend_approve'
            ? '确认同意指定申请'
            : '确认原生验证',
        {
          confirmButtonText: '确认提交',
          cancelButtonText: '取消',
          type: 'warning',
          ...(['friend_delete', 'friend_approve'].includes(args.action)
            ? { customClass: 'friend-delete-confirm' }
            : {})
        }
      )
    } catch {
      return
    } finally {
      if (version === generation) confirming.value = false
    }
    if (version !== generation || !canRun.value) return
    error.value = ''
    effect.value = ''
    submitting.value = true
    const key = crypto.randomUUID()
    pending.value = { key, account, window: grantId, created: Date.now() }
    savePending()
    try {
      const message = ['send_text', 'recall_message'].includes(args.action)
      const data = message
        ? {
            operation: await api<Row>(
              args.action === 'send_text' ? '/test-tools/text' : '/test-tools/recall',
              'POST',
              {
                account_id: account,
                test_window_id: grantId,
                ...(args.action === 'send_text' ? { text: args.text } : { event_id: args.event_id })
              },
              undefined,
              props.workspace,
              key
            )
          }
        : await call(
            membershipKinds.includes(args.action) ? 'run_social' : 'run_moderation',
            args,
            key
          )
      if (version === generation) {
        observedUsed.value[grantId] = Math.max(observedUsed.value[grantId] || 0, used + 1)
        accept(data.operation, true)
        if (data.operation.status === 'succeeded') {
          if (message) effect.value = '原生已确认；可在群消息中核对回显或撤回通知。'
          else await verifyEffect(args, version)
        }
      }
    } catch (e) {
      if (version === generation)
        error.value = `${(e as Error).message}；请查询原操作，勿另建请求。`
    } finally {
      if (version === generation) {
        await loadPreparation()
        if (version === generation) {
          submitting.value = false
          emit('updated')
        }
      }
    }
  }
  async function readMessageCandidates() {
    if (busy.value || !validGrant.value) return
    const version = generation
    reading.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        `/test-tools/messages?account_id=${encodeURIComponent(accountId.value)}&test_window_id=${encodeURIComponent(windowId.value)}`,
        'GET',
        undefined,
        undefined,
        props.workspace
      )
      if (version === generation) {
        messageCandidates.value = data.items
        if (!data.items.some((item: Row) => item.event_id === messageEvent.value))
          messageEvent.value = ''
      }
    } catch (e) {
      if (version === generation) error.value = (e as Error).message
    } finally {
      if (version === generation) reading.value = false
    }
  }
  async function verifyEffect(args: Row, version: number) {
    effect.value = '操作已确认，正在只读核对实际效果。'
    try {
      const social = membershipKinds.includes(args.action)
      const bans = ['kick_member_ban', 'remove_group_ban'].includes(args.action)
      const data = await call(
        social ? 'read_membership' : bans ? 'read_group_bans' : 'read_group',
        {
          account_id: args.account_id,
          test_window_id: args.test_window_id
        }
      )
      if (version !== generation) return
      const view = data.snapshot
      if (social) membership.value = view
      else snapshot.value = view
      recordRemaining(data.scope)
      let matches: boolean | undefined
      switch (args.action) {
        case 'group_info':
          matches = view.group_name === args.text
          break
        case 'grant_admin':
        case 'revoke_admin':
          matches = view.members.some(
            (member: Row) =>
              member.id === args.target_id &&
              member.role === (args.action === 'grant_admin' ? 'admin' : 'member')
          )
          break
        case 'group_leave':
          if (view.directory_known) matches = !view.in_group
          break
        case 'group_accept':
          if (view.directory_known && view.members_known)
            matches = view.in_group && view.member_ids.includes(view.self_id)
          break
        case 'group_invite':
          if (view.directory_known && view.members_known)
            matches = view.in_group && view.member_ids.includes(args.target_id)
          if (matches === false) {
            effect.value = '邀请已确认；尚未观察到目标加入群组，不会自动重发。'
            return
          }
          break
        case 'friend_delete':
          if (completeFriends.value)
            matches = !view.friends.some((friend: Row) => friend.id === args.target_id)
          break
        case 'friend_approve':
          matches = view.applications?.some(
            (row: Row) =>
              row.id === args.text &&
              row.user?.id === args.target_id &&
              row.direction === 'FROM_HIM' &&
              row.state === 'FRIENDLOG_STATE_AGREE'
          )
          if (matches && view.friends_known)
            matches = view.friends.some((friend: Row) => friend.id === args.target_id)
          break
        case 'mute_member':
        case 'unmute_member':
          if (view.mutes_known)
            matches = view.muted_ids.includes(args.target_id) === (args.action === 'mute_member')
          break
        case 'mute_group':
        case 'unmute_group':
          if (typeof view.policy?.muted === 'boolean')
            matches = view.policy.muted === (args.action === 'mute_group')
          break
        case 'kick_member':
          matches = !view.members.some((member: Row) => member.id === args.target_id)
          break
        case 'kick_member_ban':
          if (view.bans_known)
            matches =
              !view.members.some((member: Row) => member.id === args.target_id) &&
              view.group_bans.some((ban: Row) => ban.userID === args.target_id)
          break
        case 'remove_group_ban':
          if (view.bans_known)
            matches = !view.group_bans.some((ban: Row) => ban.userID === args.target_id)
          break
      }
      effect.value =
        matches === true
          ? `只读核对：已观察到期望状态${args.action === 'mute_member' ? '；未验证禁言自动到期时长' : ''}。`
          : matches === false
            ? '只读核对：当前状态与期望不一致，请核查；不会自动重发。'
            : '只读核对：当前证据无法证明实际效果，请在官方客户端核查。'
    } catch (e) {
      if (version === generation)
        effect.value = `操作已确认；效果读取未完成：${(e as Error).message}。不会自动重发。`
    }
  }
  function accept(op: Row, resolvePending = false) {
    result.value = op
    if (resolvePending || pending.value?.id === op.id) {
      if (!unresolved(op.status)) pending.value = null
      else if (pending.value) pending.value.id = op.id
    } else if (!pending.value && unresolved(op.status)) {
      pending.value = { id: op.id, account: accountId.value, created: Date.now() }
    }
    history.value = history.value.map((item) => (item.id === op.id ? { ...item, ...op } : item))
    savePending()
  }
  async function inspect(op?: Row) {
    const version = generation
    error.value = ''
    effect.value = ''
    reading.value = true
    try {
      const data = await call('inspect_operation', {
        account_id: accountId.value,
        ...(op
          ? { operation_id: op.id }
          : pending.value?.key
            ? { key: pending.value.key }
            : { operation_id: pending.value?.id || result.value?.id })
      })
      if (version === generation) accept(data.operation, !op && !!pending.value)
    } catch (e) {
      if (version === generation) {
        error.value = (e as Error).message
        // A request can still be preparing its transaction when the first lookup
        // returns 404. Keep the key beyond the bounded HTTP/submission deadlines.
        if (
          (e as { code?: string }).code === 'operation_not_found' &&
          !op &&
          pending.value?.created &&
          Date.now() - pending.value.created > 90_000
        ) {
          pending.value = null
          savePending()
        }
      }
    } finally {
      if (version === generation) {
        await loadPreparation()
        if (version === generation) reading.value = false
      }
    }
  }
  async function reconcile() {
    const version = generation
    error.value = ''
    reading.value = true
    try {
      const data = await call('reconcile_operation', {
        account_id: accountId.value,
        operation_id: result.value?.id
      })
      if (version === generation) accept(data.operation)
    } catch (e) {
      if (version === generation) error.value = (e as Error).message
    } finally {
      if (version === generation) {
        await loadPreparation()
        if (version === generation) reading.value = false
      }
    }
  }
  async function toggleTesting(value: string | number | boolean) {
    await changeAccountFlag('testing_enabled', !!value, '受控测试')
  }
  async function toggleAdmission(value: string | number | boolean) {
    await changeAccountFlag('connection_enabled', !!value, '动作准入')
  }
  async function changeAccountFlag(field: string, value: boolean, label: string) {
    if (busy.value) return
    const version = generation,
      account = currentAccount.value
    if (!account) return
    const accountVersion = account.version
    confirming.value = true
    try {
      await confirmAction(
        'account',
        `${value ? '开启' : '关闭'} ${account.name} 的${label}，已有授权窗口将因账号版本变化失效。`,
        label,
        { confirmButtonText: '确认', cancelButtonText: '取消' }
      )
    } catch {
      return
    } finally {
      if (version === generation) confirming.value = false
    }
    if (version !== generation) return
    submitting.value = true
    error.value = ''
    try {
      await api(
        `/accounts/${account.id}`,
        'PATCH',
        { version: accountVersion, [field]: value },
        undefined,
        props.workspace
      )
      if (version === generation) {
        snapshot.value = null
        membership.value = null
        roleObservation.value = null
        emit('updated')
        await loadPreparation()
      }
    } catch (e) {
      if (version === generation) error.value = (e as Error).message
    } finally {
      if (version === generation) submitting.value = false
    }
  }
  watch(
    () => props.account,
    (value) => (accountId.value = value || '')
  )
  watch(() => `${props.workspace}:${session.user?.id}:${accountId.value}`, reset, {
    immediate: true
  })
  watch(
    friendScope,
    (scope, previous) => {
      friendGateOpen.value = false
      if (activeConfirmation?.kind === 'friend') activeConfirmation.cancel()
      if (previous) friendDrafts.set(previous, friend.value)
      friendGeneration++
      friendBusy.value = false
      friend.value = friendDrafts.get(scope) || newFriendDraft()
      restoreControlledFriendPending()
    },
    { flush: 'sync' }
  )
  watch(
    () => `${props.workspace}:${session.user?.id}:${accountId.value}`,
    () => {
      friendOpen.value = false
    },
    { flush: 'sync' }
  )
  watch(preparationGroup, () => (roleObservation.value = null))
  watch(
    () => `${currentAccount.value?.epoch}:${currentAccount.value?.version}`,
    () => {
      snapshot.value = null
      membership.value = null
      roleObservation.value = null
    }
  )
  watch(
    () =>
      `${session.user?.id}:${session.user?.role}:${props.workspace}:${accountId.value}:${windowId.value}:${currentAccount.value?.epoch}:${currentAccount.value?.version}:${currentAccount.value?.testing_enabled}:${currentAccount.value?.connection_enabled}`,
    () => {
      activeConfirmation?.cancel()
    },
    { flush: 'sync' }
  )
  watch(
    [validGrant, actionEvidence],
    ([valid, evidence]) => {
      if (activeConfirmation?.kind === 'action' && (!valid || !evidence))
        activeConfirmation.cancel()
    },
    { flush: 'sync' }
  )
  watch(
    [friendOpen, canSendControlledFriend],
    ([open, canSend]) => {
      if (!open) friendGateOpen.value = false
      if (activeConfirmation?.kind === 'friend' && (!open || !canSend)) activeConfirmation.cancel()
    },
    { flush: 'sync' }
  )
  onUnmounted(() => {
    activeConfirmation?.cancel()
    generation++
    friendGeneration++
    clearInterval(timer)
  })
</script>

<style scoped>
  .test-tools {
    min-width: 0;
  }
  .tools-toolbar,
  .tools-account {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .tools-toolbar {
    justify-content: space-between;
  }
  .tools-toolbar h3,
  .tools-toolbar h4 {
    margin: 0;
    font-size: 16px;
  }
  .tools-account {
    margin: 20px 0;
  }
  .tools-account .el-select {
    width: 240px;
    max-width: 100%;
  }
  .tools-windows {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
    gap: 8px;
  }
  .tools-grant {
    display: grid;
    gap: 6px;
    text-align: left;
    padding: 12px;
    border: 1px solid var(--el-border-color);
    border-radius: 6px;
    background: var(--el-bg-color);
    color: inherit;
    cursor: pointer;
    overflow-wrap: anywhere;
  }
  .tools-grant.selected {
    border-color: var(--el-color-primary);
  }
  .tools-grant small,
  .tools-state small {
    color: var(--el-text-color-secondary);
  }
  .tools-scope,
  .tools-result,
  .tools-history,
  .tools-preparation {
    border-top: 1px solid var(--el-border-color);
    margin-top: 20px;
    padding-top: 16px;
  }
  .tools-scope p,
  .tools-preparation p,
  .tools-history p,
  .tools-result p {
    font-size: 13px;
    line-height: 1.7;
    overflow-wrap: anywhere;
  }
  .tools-state {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 20px;
    padding: 12px 0;
    font-size: 13px;
  }
  .tools-fields {
    display: grid;
    gap: 12px;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr));
    margin-top: 16px;
  }
  .tools-fields .el-select {
    width: 100%;
  }
  .test-tools .el-button {
    gap: 6px;
  }
  .test-tools .el-button svg {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
  }
  .tools-operation {
    font-family: monospace;
  }
  .tools-history-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    padding: 12px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .tools-history-row > div {
    min-width: 0;
    max-width: 100%;
  }
  .tools-history-row p {
    margin: 4px 0;
  }
  .tools-history-row small {
    display: block;
    overflow-wrap: anywhere;
  }
  :global(.friend-delete-confirm) {
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 32px);
    overflow-y: auto;
  }
  :global(.friend-delete-confirm .el-message-box__message) {
    overflow-wrap: anywhere;
    word-break: break-word;
  }
  :global(.friend-delete-options) {
    max-width: calc(100vw - 24px);
  }
  :global(.friend-delete-options .el-select-dropdown__item) {
    height: auto;
    min-height: 34px;
    padding-top: 8px;
    padding-bottom: 8px;
    white-space: normal;
    overflow-wrap: anywhere;
    line-height: 1.5;
  }
  .friend-record,
  .friend-actions {
    display: flex;
    gap: 12px;
    align-items: center;
  }
  .friend-record {
    width: 100%;
  }
  .friend-record .el-input {
    flex: 1;
    min-width: 0;
  }
  .friend-actions {
    justify-content: flex-end;
    flex-wrap: wrap;
  }
  .friend-fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }
  .friend-scope {
    font-size: 13px;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .friend-window {
    border-top: 1px solid var(--el-border-color);
    margin: 16px 0;
    padding-top: 8px;
  }
  :global(.controlled-friend-dialog) {
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 32px);
    display: flex;
    flex-direction: column;
    margin: 16px auto;
  }
  :global(.controlled-friend-dialog .el-dialog__body) {
    overflow-y: auto;
    min-height: 0;
  }
  :global(.controlled-friend-dialog .el-dialog__footer) {
    flex-shrink: 0;
  }
  @media (max-width: 560px) {
    .friend-fields {
      grid-template-columns: minmax(0, 1fr);
      gap: 0;
    }
    .friend-record {
      flex-wrap: wrap;
    }
    .friend-record .el-input {
      flex-basis: 100%;
    }
  }
</style>
