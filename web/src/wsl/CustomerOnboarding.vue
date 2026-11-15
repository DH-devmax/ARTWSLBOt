<template>
  <ElDialog
    :model-value="true"
    :title="initialGroup ? '重新核验群归属' : operator ? '接入群组' : '接入我的群组'"
    width="620px"
    class="customer-scope-dialog onboarding-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
    @close="close"
  >
    <ol class="onboarding-steps" aria-label="群接入步骤">
      <li
        v-for="(item, index) in setupSteps"
        :key="item.title"
        :class="{ 'is-current': index === step, 'is-done': item.done }"
        :aria-current="index === step ? 'step' : undefined"
      >
        <span>{{ String(index + 1).padStart(2, '0') }}</span>
        <strong>{{ item.title }}</strong>
        <small v-if="item.skipped">已跳过</small>
      </li>
    </ol>
    <dl v-if="initialGroup" class="onboarding-details"
      ><dt>群组</dt><dd>{{ initialGroup.name }}</dd
      ><dt>平台</dt><dd>{{ platformLabel(initialGroup.platform) }}</dd></dl
    >
    <ElAlert
      v-if="controlledScope"
      class="onboarding-controlled-notice"
      title="受控接入测试"
      type="warning"
      :closable="false"
    >
      <dl class="onboarding-details">
        <dt>机器人</dt
        ><dd>{{ controlledScope.robot.name }} · {{ controlledScope.robot.native_id }}</dd>
        <dt>申请人</dt
        ><dd>{{ controlledScope.claimant.name }} · {{ controlledScope.claimant.native_id }}</dd>
        <dt>隔离群</dt
        ><dd>{{ controlledScope.group.name }} · {{ controlledScope.group.native_id }}</dd>
        <dt>有效至</dt><dd>{{ customerDate(controlledScope.expires_at) }}</dd>
      </dl>
      <p>仅限此申请人在此群完成本次测试，不作为普通客户身份或群归属验收。</p>
      <p v-if="controlledCaptchaExempt">本次受控接入免滑块，好友处理仍需本人确认。</p>
    </ElAlert>
    <ElAlert
      v-if="controlledExpired"
      class="onboarding-controlled-expired"
      :title="
        controlledScope?.state === 'unknown'
          ? '受控范围已到期，原操作结果仍待核对'
          : '受控接入范围已过期，继续测试需创建新范围'
      "
      type="error"
      show-icon
      :closable="false"
      role="alert"
    >
      <template #icon><WarningFilled /></template>
      <p v-if="controlledScope?.state === 'unknown' || pendingOperation"
        >请刷新原记录核对结果，不要重复提交或创建替代范围。</p
      >
      <p v-else>原范围不能续期或恢复。请在测试工具中创建新范围，再重新开始核验。</p>
    </ElAlert>
    <ElAlert
      v-else-if="controlledScope && controlledUnavailable"
      :title="
        controlledScope.blocked_reason
          ? customerReason(controlledScope.blocked_reason)
          : '此受控范围当前不可提交，请刷新原记录核对。'
      "
      type="warning"
      :closable="false"
    />
    <p v-if="!controlledScope && !complete && !terminal" class="onboarding-hint"
      >群主和管理员指官方 App
      中的群内角色，与平台管理号池身份不同。请使用未接入号池的本人群主或管理员账号发送核验内容。</p
    >
    <ElAlert v-if="error" class="onboarding-error" :title="error" type="error" :closable="false" />
    <ElAlert
      v-if="customerError && !challenge"
      :title="customerError"
      type="error"
      :closable="false"
    />
    <ElSkeleton v-if="loading && !challenge" :rows="5" animated />
    <ElForm v-else-if="!challenge" label-position="top" @submit.prevent="create">
      <ElFormItem v-if="operator && canOnboardForCustomer" label="接入归属">
        <ElRadioGroup
          v-model="targetMode"
          aria-label="接入归属"
          :disabled="saving || uncertain || Boolean(initialGroup)"
        >
          <ElRadio value="mine">我的工作空间</ElRadio>
          <ElRadio value="customer" :disabled="Boolean(controlledID)">代客户接入</ElRadio>
        </ElRadioGroup>
      </ElFormItem>
      <p v-if="targetMode === 'mine'" class="onboarding-hint">
        所属工作空间：{{ context?.workspace?.name || workspace?.name || '我的工作空间' }}
      </p>
      <p v-if="controlledID" class="onboarding-hint"
        >受控接入仅限本人工作空间；如需代客户接入，请先明确选择普通接入。</p
      >
      <ElFormItem v-if="operator && targetMode === 'customer'" label="所属客户">
        <ElSelect
          v-model="selectedCustomer"
          :disabled="saving || loading || uncertain || Boolean(initialGroup)"
          filterable
          remote
          :remote-method="searchCustomers"
          :loading="customersLoading"
          placeholder="选择群组所属客户"
          aria-label="群组所属客户"
          @change="customerChanged"
        >
          <ElOption
            v-for="item in customerOptions"
            :key="item.tenant_id"
            :label="item.name"
            :value="item.tenant_id"
          />
          <template #footer>
            <ElButton
              v-if="customerNext >= 0"
              text
              :loading="customersLoading"
              @click="loadCustomers(true)"
              >加载更多客户</ElButton
            >
          </template>
        </ElSelect>
        <p class="onboarding-hint">群组将绑定到明确选择的客户，自动业务保持关闭。</p>
      </ElFormItem>
      <ElFormItem v-if="!initialGroup" label="平台">
        <ElRadioGroup
          v-model="platform"
          class="onboarding-platforms"
          aria-label="选择接入平台"
          :disabled="saving"
        >
          <ElRadioButton v-for="item in allowedPlatforms" :key="item.value" :value="item.value"
            ><img :src="item.image" alt="" />{{ item.label }}</ElRadioButton
          >
        </ElRadioGroup>
      </ElFormItem>
      <ElFormItem v-if="canUseControlled" label="接入范围">
        <ElSelect
          v-model="controlledID"
          aria-label="接入范围"
          :disabled="saving || loading || uncertain || controlledLoading"
          :loading="controlledLoading"
        >
          <ElOption value="" label="普通接入（本人非号池账号）" />
          <ElOption
            v-for="item in controlledOptions"
            :key="item.id"
            :value="item.id"
            :label="`${item.robot.name} / ${item.claimant.name} / ${item.group.name} · ${controlledOnboardingState(item, clock)}`"
            :disabled="!controlledOnboardingActive(item, clock)"
          />
          <ElOption
            v-if="controlledID && !controlledOptions.some((item) => item.id === controlledID)"
            :value="controlledID"
            label="已选范围不可用，请重新选择"
            disabled
          />
        </ElSelect>
        <div class="onboarding-controlled-actions">
          <ElButton
            text
            :icon="Refresh"
            :disabled="saving || loading || controlledLoading"
            @click="loadControlledScopes"
          >
            刷新可用范围
          </ElButton>
          <ElButton
            v-if="controlledExpired && controlledScope?.state !== 'unknown'"
            text
            :icon="Setting"
            :disabled="saving || loading"
            @click="openControlledTools"
          >
            创建新接入范围
          </ElButton>
        </div>
        <ElAlert v-if="controlledError" :title="controlledError" type="warning" :closable="false" />
        <p v-if="controlledID && !controlledScope" class="onboarding-hint">
          选中的范围尚未通过服务端核对。请选择有效范围，或明确选择普通接入。
        </p>
      </ElFormItem>
      <p class="onboarding-hint">群主核验后可直接绑定。管理员申请交平台审核。</p>
    </ElForm>
    <template v-else>
      <dl class="onboarding-details">
        <dt>工作空间</dt
        ><dd>{{
          challenge.workspace?.name ||
          challenge.customer?.name ||
          context?.workspace?.name ||
          workspace?.name ||
          '当前工作空间'
        }}</dd>
      </dl>
      <div class="onboarding-state"
        ><ElTag :type="expired ? 'danger' : complete ? 'success' : 'info'">{{ stateLabel }}</ElTag
        ><span>{{ platformLabel(challenge.platform) }}</span
        ><span>有效至 {{ customerDate(challenge.expires_at) }}</span></div
      >
      <ElAlert
        v-if="expired && !controlledExpired"
        class="onboarding-expired"
        title="已过期，请重新开始核验"
        type="error"
        show-icon
        :closable="false"
        role="alert"
      >
        <template #icon><WarningFilled /></template>
        <p>本次核验内容已失效，点击「重新开始核验」获取新内容。</p>
        <p v-if="pendingOperation">已有操作结果待核对，请先核对原记录。</p>
      </ElAlert>
      <section class="onboarding-section">
        <h3>本次接入机器人</h3>
        <div class="onboarding-robot"
          ><img :src="customerPlatforms.find((p) => p.value === platform)?.image" alt="" /><div
            ><strong>{{ robotLabel(challenge) }}</strong
            ><p>{{ platformLabel(challenge.platform) }}</p></div
          ></div
        >
        <dl class="onboarding-details">
          <dt>加好友账号</dt>
          <dd v-if="addAccount" class="onboarding-public-account">
            <span>{{ addAccount }}</span>
            <ElButton
              text
              :icon="CopyDocument"
              aria-label="复制机器人加好友账号"
              @click="copy(addAccount, '机器人加好友账号')"
            />
          </dd>
          <dd v-else>暂未提供可添加账号，请联系平台管理员；已有好友可继续私聊核验。</dd>
        </dl>
      </section>
      <section
        v-if="!terminal && !groupProven"
        class="onboarding-section"
        aria-label="好友接入核验"
      >
        <h3>添加机器人与私聊核验</h3>
        <p v-if="friendVerified" class="onboarding-proof">
          <ElIcon><CircleCheck /></ElIcon>好友身份已核验，可邀请机器人入群
        </p>
        <template v-else>
          <ElRadioGroup
            v-if="incomingFlow"
            v-model="friendPath"
            class="onboarding-friend-paths"
            aria-label="选择好友核验方式"
            :disabled="saving || loading || terminal"
            @change="saveProgressDraft"
          >
            <ElRadioButton value="incoming">我添加机器人</ElRadioButton>
            <ElRadioButton value="private">已是好友，私聊核验</ElRadioButton>
          </ElRadioGroup>
          <template v-if="incomingFlow && friendPath === 'incoming'">
            <p class="onboarding-hint">
              请在{{ platformLabel(challenge.platform) }} App
              中添加上方机器人，再填写本人当前完整昵称，核对刚刚发出的好友请求。无需填写指定申请附言。
              超过10分钟的旧申请默认单条拒绝：点击「核对近期好友请求」后，若选中过期申请，
              {{
                controlledCaptchaExempt
                  ? '确认后仅拒绝这一条。'
                  : '将直接进行安全验证，通过后仅拒绝这一条。'
              }}之后请重新开始核验，并在有效时间内重新发申请。
            </p>
            <ElForm
              v-if="!friendContact"
              label-position="top"
              class="onboarding-friend-target"
              @submit.prevent="prepareFriendCandidate"
            >
              <ElFormItem label="本人完整昵称">
                <ElInput
                  v-model="friendNickname"
                  :maxlength="64"
                  autocomplete="off"
                  :disabled="saving || loading || uncertain || friendSubmitted || terminal"
                  placeholder="与刚刚发送请求时的昵称完全一致"
                  aria-label="本人完整昵称"
                />
              </ElFormItem>
              <p class="onboarding-hint">
                仅核对服务器限定的本次接入时间范围内、昵称完全一致的一条待处理请求。
                昵称和申请时间只用于选择联系请求，不能证明本人身份。
              </p>
              <ElButton
                :icon="Search"
                :loading="saving"
                :disabled="!canPrepareFriendCandidate"
                @click="prepareFriendCandidate"
                >核对近期好友请求</ElButton
              >
            </ElForm>
            <template v-if="friendContact">
              <h3>本次选中的好友请求</h3>
              <dl class="onboarding-details onboarding-friend-target">
                <dt>申请人昵称</dt><dd>{{ friendContact.name }}</dd> <dt>申请时间</dt
                ><dd>{{ customerDate(friendContact.requested_at) }}</dd> <dt>平台</dt
                ><dd>{{ platformLabel(challenge.platform) }}</dd> <dt>执行机器人</dt
                ><dd>{{ robotLabel(challenge) }}</dd>
              </dl>
              <ElTag
                :type="
                  friendContact.status === 'succeeded' &&
                  (friendContact.action === 'approve' || expiredRejectionConfirmed)
                    ? 'success'
                    : 'info'
                "
              >
                {{ contactLabel(friendContact.status) }}
              </ElTag>
              <p
                v-if="friendContact.result_code && friendContact.action === 'approve'"
                class="onboarding-hint"
              >
                {{ customerReason(friendContact.result_code) }}
              </p>
              <template v-if="friendContact.status === 'prepared' && !friendSubmitted">
                <template v-if="friendContact.action === 'reject_expired'">
                  <p class="onboarding-hint onboarding-consent">
                    这是一条超过10分钟的旧申请，尚未拒绝，也不会同意。
                    {{
                      controlledCaptchaExempt
                        ? '确认后仅拒绝这条旧申请。'
                        : '完成安全验证后仅拒绝这条旧申请；取消验证会保留本条记录，可点击「处理过期申请」继续。'
                    }}
                  </p>
                  <ElButton
                    type="warning"
                    :icon="Delete"
                    :loading="saving"
                    :disabled="!canRejectExpiredFriend"
                    @click="rejectExpiredFriend"
                    >处理过期申请</ElButton
                  >
                </template>
                <template v-else>
                  <p class="onboarding-hint onboarding-consent">
                    核对昵称和申请时间后，点击「接受此好友请求」确认{{
                      controlledCaptchaExempt ? '。' : '，并完成安全验证。'
                    }}
                    接受申请后仍需独立私聊核验本人身份。
                  </p>
                  <ElButton
                    type="primary"
                    :icon="Checked"
                    :loading="saving"
                    :disabled="!canApproveFriend"
                    @click="friendApproveOpen = true"
                    >接受此好友请求</ElButton
                  >
                </template>
                <p
                  v-if="
                    friendContact.action === 'reject_expired'
                      ? !friendContact.can_reject
                      : !friendContact.can_approve
                  "
                  class="onboarding-hint"
                >
                  {{
                    friendContact.blocked_reason
                      ? customerReason(friendContact.blocked_reason)
                      : '当前尚不满足好友审批条件，请刷新本次接入进度核对。'
                  }}
                </p>
              </template>
              <p v-else-if="friendContact.action === 'reject_expired'" class="onboarding-hint">
                {{ expiredRejectionMessage }}
              </p>
              <p v-else-if="friendContact.status === 'failed'" class="onboarding-hint">
                本次好友审批未完成，不会自动重试。如已是好友，可选择私聊核验。
              </p>
              <p v-else-if="friendContact.status === 'succeeded'" class="onboarding-hint">
                好友申请已接受，请继续下方独立私聊核验。接受申请不代表本人身份或群归属已核验。
              </p>
              <p v-else-if="contactUnresolved || friendSubmitted" class="onboarding-hint">
                好友审批结果待核对，请查询原操作，不要再次提交。
              </p>
            </template>
            <ElAlert
              v-if="friendSubmitted && !friendContact"
              title="好友审批提交结果待确认，请刷新原记录核对，不要重新提交。"
              type="info"
              :closable="false"
            />
          </template>
          <ElAlert
            v-if="!incomingFlow"
            title="此记录使用旧版接入方式，好友操作仅保留历史核对。已有好友可继续私聊核验，或取消后重新开始。"
            type="info"
            :closable="false"
          />
          <ElAlert
            v-if="incomingFlow && friendPath === 'private' && expiredContactSelected"
            class="onboarding-private-blocked"
            title="本次记录不能改走私聊核验"
            type="warning"
            :closable="false"
          >
            <p v-if="friendContact?.status === 'prepared' && !pendingOperation">
              本次已选中过期申请，尚未提交拒绝。若已在官方 App 确认是好友，
              请先取消本次接入，再点击「重新开始核验」选择私聊方式。
            </p>
            <p v-else>本次记录已关联过期申请处理。请先核对原操作，确认处理结果后再重新开始核验。</p>
          </ElAlert>
          <template v-if="showPrivateProof">
            <h3 class="onboarding-private-title">私聊核验本人身份</h3>
            <p class="onboarding-hint">
              请使用本人账号私聊本次机器人，发送以下完整内容，再刷新进度。
              此内容独立于好友请求；接受好友申请、昵称和申请时间均不能证明本人身份。
            </p>
            <div v-if="privateChallenge" class="onboarding-code">
              <code>{{ privateChallenge }}</code>
              <ElButton
                text
                :icon="CopyDocument"
                aria-label="复制身份核验内容"
                @click="copy(privateChallenge)"
              />
            </div>
            <ElAlert
              v-else
              title="身份核验内容仅在首次签发时显示，请使用此前保留的完整内容，或取消本次接入后重新开始。"
              type="info"
              :closable="false"
            />
            <p class="onboarding-hint">等待本次私聊身份核验通过后，再邀请机器人入群。</p>
          </template>
        </template>
      </section>
      <section
        v-if="!complete && !terminal && !groupProven && friendVerified"
        class="onboarding-section"
      >
        <h3>邀请机器人入群</h3>
        <p class="onboarding-hint"
          >添加好友后，在官方 App 将本次机器人邀请到{{ initialGroup ? '本群' : '目标群' }}。</p
        >
        <p
          v-if="privateChallenge && friendVerified && !challenge.identity_proven"
          class="onboarding-hint"
        >
          若邀请仍需身份核验，请私聊机器人发送本次身份核验内容。
        </p>
        <div
          v-if="privateChallenge && friendVerified && !challenge.identity_proven"
          class="onboarding-code"
        >
          <code>{{ privateChallenge }}</code>
          <ElButton
            text
            :icon="CopyDocument"
            aria-label="复制身份核验内容"
            @click="copy(privateChallenge)"
          />
        </div>
      </section>
      <section
        v-if="!complete && !terminal && !groupProven"
        class="onboarding-section"
        aria-label="群内归属核验"
      >
        <h3>证明群归属</h3>
        <p class="onboarding-hint"
          >机器人已在群内时，可直接由本人群主或管理员在群内发送以下完整内容，再刷新进度。
          尚未入群时，请先完成添加好友和邀请入群。</p
        >
        <div v-if="groupChallenge" class="onboarding-code"
          ><code>{{ groupChallenge }}</code
          ><ElButton
            text
            :icon="CopyDocument"
            aria-label="复制群归属核验内容"
            @click="copy(groupChallenge)"
        /></div>
        <ElAlert
          v-else
          title="核验内容仅在创建时显示。请使用此前保留的内容，或取消本次接入后重新核验。"
          type="info"
          :closable="false"
        />
      </section>
      <section v-if="groupProven && !terminal" class="onboarding-section">
        <p class="onboarding-proof"
          ><ElIcon><CircleCheck /></ElIcon>机器人已在群内，群归属证明已核验</p
        >
        <p class="onboarding-hint"
          >{{ !friendSubmitted ? '已跳过添加好友与邀请步骤，' : '' }}请核对群组后确认绑定。</p
        >
      </section>
      <section
        v-if="challenge.can_accept_invitation && friendVerified && !terminal"
        class="onboarding-section"
      >
        <h3>核对本人发出的群邀请</h3>
        <ElForm label-position="top" @submit.prevent="prepareInvitation">
          <ElFormItem label="目标群原生标识">
            <ElInput
              v-model="nativeGroupID"
              maxlength="200"
              autocomplete="off"
              :disabled="saving || terminal || !!initialGroup || !!controlledID"
            />
          </ElFormItem>
          <ElFormItem label="邀请编号（可选）">
            <ElInput
              v-model="invitationID"
              maxlength="300"
              autocomplete="off"
              :disabled="saving || terminal"
            />
          </ElFormItem>
          <ElButton
            :icon="Search"
            :loading="saving"
            :disabled="!nativeGroupID.trim() || uncertain || loading || terminal"
            @click="prepareInvitation"
            >核对群邀请</ElButton
          >
        </ElForm>
        <template v-if="invitationPreview">
          <dl class="onboarding-details"
            ><dt>群名</dt><dd>{{ invitationPreview.group_name || '未提供群名' }}</dd
            ><dt>群标识</dt><dd>{{ invitationPreview.native_group_id }}</dd
            ><dt>有效至</dt><dd>{{ customerDate(invitationPreview.expires_at) }}</dd></dl
          >
          <ElButton
            :icon="Checked"
            :disabled="saving || uncertain || loading || invitationExpired"
            @click="acceptOpen = true"
            >接受此群邀请</ElButton
          >
        </template>
      </section>
      <section
        v-if="friendContact && (terminal || friendVerified || friendPath === 'private')"
        class="onboarding-section"
        aria-label="来向好友审批结果"
      >
        <h3>来向好友审批结果</h3>
        <dl class="onboarding-details onboarding-friend-target">
          <dt>申请人昵称</dt><dd>{{ friendContact.name }}</dd> <dt>申请时间</dt
          ><dd>{{ customerDate(friendContact.requested_at) }}</dd>
        </dl>
        <ElTag
          :type="
            friendContact.status === 'succeeded' &&
            (friendContact.action === 'approve' || expiredRejectionConfirmed)
              ? 'success'
              : 'info'
          "
        >
          {{ contactLabel(friendContact.status) }}
        </ElTag>
        <p v-if="friendContact.action === 'reject_expired'" class="onboarding-hint">
          {{ expiredRejectionMessage }}
        </p>
        <template v-else>
          <p v-if="friendContact.result_code" class="onboarding-hint">
            {{ customerReason(friendContact.result_code) }}
          </p>
          <p class="onboarding-hint">好友审批只建立联系关系，本人身份须由独立私聊消息核验。</p>
        </template>
      </section>
      <section v-if="canReconcileContact" class="onboarding-section">
        <ElButton :icon="Refresh" :disabled="saving || loading" @click="reconcileFriend('contact')">
          核对原好友审批
        </ElButton>
      </section>
      <section v-if="outboundAttempt" class="onboarding-section" aria-label="好友申请结果">
        <h3>旧版发出好友申请记录</h3>
        <ElTag :type="outboundAttempt.status === 'friends' ? 'success' : 'info'">
          {{ outboundAttemptLabel(outboundAttempt.status) }}
        </ElTag>
        <p class="onboarding-hint"
          >历史记录只读，已准备的旧申请也不能发送。已有好友可直接私聊核验。</p
        >
        <p v-if="outboundAttempt.code" class="onboarding-hint">
          {{ customerReason(outboundAttempt.code) }}
        </p>
        <ElButton
          v-if="canReconcileOutbound"
          :icon="Refresh"
          :disabled="saving || loading"
          @click="reconcileOutboundFriend"
          >核对原好友申请</ElButton
        >
      </section>
      <section v-if="challenge.friend_attempt" class="onboarding-section">
        <h3>旧版好友操作记录</h3>
        <ElTag :type="challenge.friend_attempt.status === 'succeeded' ? 'success' : 'info'">
          {{ friendAttemptLabel(challenge.friend_attempt.status) }}
        </ElTag>
        <p class="onboarding-hint">仅保留历史结果与原操作核对，不会重新审批好友。</p>
        <p class="onboarding-hint">{{ customerReason(challenge.friend_attempt.result_code) }}</p>
        <ElButton
          v-if="challenge.friend_attempt.status === 'unknown'"
          :icon="Refresh"
          :disabled="saving || loading"
          @click="reconcileFriend('legacy')"
          >核对原好友操作</ElButton
        >
      </section>
      <section v-if="challenge.accept_attempt" class="onboarding-section">
        <h3>接受邀请结果</h3>
        <ElTag :type="challenge.accept_attempt.status === 'succeeded' ? 'success' : 'info'">{{
          attemptLabel(challenge.accept_attempt.status)
        }}</ElTag>
        <p class="onboarding-hint">{{ customerReason(challenge.accept_attempt.result_code) }}</p>
        <ElButton
          v-if="challenge.accept_attempt.status === 'unknown'"
          :icon="Refresh"
          :disabled="saving || loading"
          @click="reconcile"
          >核对原邀请操作</ElButton
        >
      </section>
      <section v-if="challenge.group" class="onboarding-section">
        <h3>{{ complete ? '已绑定群组' : '核验到的群组' }}</h3>
        <dl class="onboarding-details"
          ><dt>群名</dt><dd>{{ challenge.group.name }}</dd
          ><dt>平台</dt><dd>{{ platformLabel(challenge.platform) }}</dd
          ><dt>群标识</dt><dd>{{ challenge.group.native_id }}</dd
          ><dt>机器人</dt><dd>{{ robotLabel(challenge) }}</dd></dl
        >
        <p v-if="challenge.proof_role" class="onboarding-hint"
          >本人原生角色：{{
            challenge.proof_role === 'owner'
              ? '群主'
              : challenge.proof_role === 'admin'
                ? '管理员'
                : '待核验'
          }}</p
        >
        <ElAlert
          v-if="complete"
          title="群组已绑定，业务运行保持关闭。需要启用时，请进入「运行与权限」明确开启。"
          type="info"
          :closable="false"
        />
      </section>
      <ElAlert
        v-if="challenge.blocked_reason"
        :title="customerReason(challenge.blocked_reason)"
        type="warning"
        :closable="false"
      />
      <ElAlert
        v-if="uncertain"
        title="提交结果尚未确认。请刷新本次记录核对，不要重新提交。"
        type="warning"
        :closable="false"
      />
    </template>
    <template #footer>
      <ElButton :disabled="saving" @click="close">{{ complete ? '完成' : '关闭' }}</ElButton>
      <ElButton
        v-if="complete"
        type="primary"
        :icon="Setting"
        :disabled="saving || loading"
        @click="openRuntime"
        >运行与权限</ElButton
      >
      <ElButton
        v-if="challenge?.can_cancel && !expired && !controlledUnavailable"
        :disabled="saving || pendingOperation"
        @click="cancelOpen = true"
        >取消本次接入</ElButton
      >
      <ElButton
        v-if="challenge"
        :icon="Refresh"
        :loading="loading"
        :disabled="saving"
        @click="refresh"
        >刷新进度</ElButton
      >
      <ElButton
        v-if="!challenge"
        type="primary"
        :loading="saving"
        :disabled="
          loading ||
          customersLoading ||
          uncertain ||
          !allowedPlatforms.length ||
          !targetReady ||
          (canUseControlled && controlledLoading) ||
          controlledUnavailable
        "
        @click="create"
        >开始核验</ElButton
      >
      <ElButton
        v-else-if="controlledExpired && controlledScope?.state !== 'unknown' && !pendingOperation"
        type="primary"
        :icon="Setting"
        :disabled="saving || loading"
        @click="openControlledTools"
        >创建新接入范围</ElButton
      >
      <ElButton
        v-else-if="expired || challenge.state === 'cancelled'"
        type="primary"
        :icon="Refresh"
        :disabled="saving || loading || pendingOperation || controlledUnavailable"
        @click="restart"
        >重新开始核验</ElButton
      >
      <ElButton
        v-else-if="challenge.can_confirm && !terminal"
        type="primary"
        :loading="saving"
        :disabled="uncertain || loading"
        @click="confirmOpen = true"
        >{{ challenge.proof_role === 'owner' ? '确认绑定' : '提交审核' }}</ElButton
      >
    </template>
  </ElDialog>
  <ElDialog
    v-model="friendApproveOpen"
    :title="friendContact?.action === 'reject_expired' ? '确认拒绝过期请求' : '确认接受好友请求'"
    width="440px"
    class="customer-scope-dialog onboarding-friend-confirm"
    append-to-body
    align-center
    :close-on-click-modal="false"
  >
    <dl class="onboarding-details">
      <dt>平台</dt><dd>{{ platformLabel(challenge?.platform) }}</dd> <dt>机器人</dt
      ><dd>{{ robotLabel(challenge) }}</dd> <dt>申请人昵称</dt><dd>{{ friendContact?.name }}</dd>
      <dt>申请时间</dt><dd>{{ customerDate(friendContact?.requested_at) }}</dd>
    </dl>
    <p v-if="friendContact?.action === 'reject_expired'" class="onboarding-hint">
      只拒绝上方这一条旧申请。之后须重新开始核验，并在有效时间内重新发送好友申请。
    </p>
    <p v-else class="onboarding-hint">
      请确认这是本人刚刚发出的请求。昵称和时间仅用于选择联系请求。
      {{
        controlledCaptchaExempt ? '确认后只接受这一条请求' : '完成安全验证后只接受这一条请求'
      }}，仍需独立私聊核验本人身份。
    </p>
    <template #footer>
      <ElButton :disabled="saving" @click="friendApproveOpen = false">取消</ElButton>
      <ElButton
        type="primary"
        :icon="Checked"
        :loading="saving"
        :disabled="
          friendContact?.action === 'reject_expired' ? !canRejectExpiredFriend : !canApproveFriend
        "
        @click="approveFriend"
        >{{
          friendContact?.action === 'reject_expired'
            ? '确认拒绝'
            : controlledCaptchaExempt
              ? '确认接受'
              : '确认并安全验证'
        }}</ElButton
      >
    </template>
  </ElDialog>
  <ElDialog
    v-model="confirmOpen"
    title="确认群归属"
    width="440px"
    class="customer-scope-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
  >
    <dl class="onboarding-details"
      ><template v-if="challenge?.customer"
        ><dt>所属客户</dt><dd>{{ challenge.customer.name }}</dd></template
      ><dt>群组</dt><dd>{{ challenge?.group?.name }}</dd
      ><dt>平台</dt><dd>{{ platformLabel(challenge?.platform) }}</dd
      ><dt>机器人</dt><dd>{{ robotLabel(challenge) }}</dd></dl
    >
    <dl v-if="controlledScope" class="onboarding-details">
      <dt>受控申请人</dt><dd>{{ controlledScope.claimant.name }}</dd> <dt>隔离群</dt
      ><dd>{{ controlledScope.group.name }}</dd> <dt>范围到期</dt
      ><dd>{{ customerDate(controlledScope.expires_at) }}</dd>
    </dl>
    <p class="onboarding-hint">{{
      challenge?.proof_role === 'owner'
        ? `此群将绑定到${challenge?.workspace?.name || challenge?.customer?.name || context?.workspace?.name || workspace?.name || '当前工作空间'}，业务运行保持关闭。`
        : '本次管理员申请将进入平台审核，业务运行保持关闭。'
    }}</p>
    <template #footer
      ><ElButton :disabled="saving" @click="confirmOpen = false">取消</ElButton
      ><ElButton
        type="primary"
        :loading="saving"
        :disabled="uncertain || terminal"
        @click="mutate('confirm')"
        >{{ challenge?.proof_role === 'owner' ? '确认绑定' : '提交审核' }}</ElButton
      ></template
    >
  </ElDialog>
  <ElDialog
    v-model="acceptOpen"
    title="确认接受群邀请"
    width="440px"
    class="customer-scope-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
  >
    <dl class="onboarding-details"
      ><dt>群名</dt><dd>{{ invitationPreview?.group_name || '未提供群名' }}</dd
      ><dt>群标识</dt><dd>{{ invitationPreview?.native_group_id }}</dd
      ><dt>机器人</dt><dd>{{ robotLabel(challenge) }}</dd
      ><dt>有效至</dt><dd>{{ customerDate(invitationPreview?.expires_at) }}</dd></dl
    >
    <template #footer
      ><ElButton :disabled="saving" @click="acceptOpen = false">取消</ElButton
      ><ElButton
        type="primary"
        :loading="saving"
        :disabled="uncertain || terminal || invitationExpired"
        @click="acceptInvitation"
        >确认接受邀请</ElButton
      ></template
    >
  </ElDialog>
  <ElDialog
    v-model="cancelOpen"
    title="取消本次接入"
    width="420px"
    class="customer-scope-dialog"
    append-to-body
    align-center
  >
    <p>取消后，本次核验内容将不再用于绑定群组。</p>
    <template #footer
      ><ElButton :disabled="saving" @click="cancelOpen = false">保留</ElButton
      ><ElButton
        type="danger"
        :loading="saving"
        :disabled="expired || controlledUnavailable"
        @click="mutate('cancel')"
        >确认取消</ElButton
      ></template
    >
  </ElDialog>
  <WebCaptcha ref="webCaptcha" />
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDialog,
    ElForm,
    ElFormItem,
    ElIcon,
    ElInput,
    ElMessage,
    ElRadio,
    ElRadioButton,
    ElRadioGroup,
    ElSelect,
    ElOption,
    ElSkeleton,
    ElTag
  } from 'element-plus'
  import {
    Checked,
    CircleCheck,
    CopyDocument,
    Delete,
    Refresh,
    Search,
    Setting,
    WarningFilled
  } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'
  import WebCaptcha from './WebCaptcha.vue'
  import {
    controlledOnboardingActive,
    controlledOnboardingExpired,
    controlledOnboardingScope,
    controlledOnboardingSameScope,
    controlledOnboardingSelectionKey,
    controlledOnboardingState,
    customerDate,
    customerPlatforms,
    customerReason,
    onboardingFriendContact,
    platformLabel,
    robotAddAccount,
    robotLabel,
    type CustomerPlatform,
    type ControlledOnboardingScope
  } from './customer'
  const props = defineProps<{
    platforms: string[]
    initialPlatform?: string
    initialGroup?: Row
    operatorDemo?: boolean
    workspace?: Row
  }>()
  const emit = defineEmits<{
    close: []
    bound: []
    manage: [group: Row]
    'controlled-tools': [platform: CustomerPlatform]
  }>()
  const session = useSession(),
    actor = session.user?.id,
    tenant = session.user?.tenant_id,
    websiteRole = session.user?.role,
    tenantRole = session.user?.tenant_role,
    permissions = JSON.stringify([...session.permissions].sort()),
    view = session.console,
    openedWorkspace = session.workspace,
    openedAccount = session.selectedAccount
  const webCaptcha = ref<InstanceType<typeof WebCaptcha>>()
  const operator = computed(
    () => props.operatorDemo === true && session.user?.role === 'superadmin'
  )
  const context = ref<Row>(),
    targetMode = ref('mine'),
    selectedCustomer = ref(String(props.initialGroup?.tenant_id || '')),
    customers = ref<Row[]>([]),
    customersLoading = ref(false),
    customerError = ref(''),
    customerNext = ref(-1),
    customerSearch = ref(''),
    selectedCustomerRow = ref<Row>()
  const canOnboardForCustomer = computed(
    () => operator.value && context.value?.can_onboard_for_customer === true
  )
  const targetTenant = computed(() =>
    operator.value
      ? targetMode.value === 'customer'
        ? selectedCustomer.value
        : String(context.value?.default_target_tenant_id || '')
      : String(tenant || '')
  )
  const targetReady = computed(
    () =>
      Boolean(targetTenant.value) &&
      (!operator.value ||
        (Boolean(context.value?.workspace?.id) &&
          (targetMode.value === 'mine' || canOnboardForCustomer.value)))
  )
  const customerOptions = computed(() => {
    const selected =
      selectedCustomerRow.value ||
      (props.initialGroup
        ? {
            tenant_id: props.initialGroup.tenant_id,
            name: props.initialGroup.customer_name || props.initialGroup.tenant_name || '所选客户'
          }
        : undefined)
    return selected && !customers.value.some((item) => item.tenant_id === selected.tenant_id)
      ? [selected, ...customers.value]
      : customers.value
  })
  let customerSequence = 0,
    customerController: AbortController | undefined,
    customerTimer: ReturnType<typeof setTimeout> | undefined
  const allowedPlatforms = computed(() =>
    customerPlatforms.filter((item) => props.platforms.includes(item.value))
  )
  const selectedPlatform = ref<CustomerPlatform>(
    props.initialPlatform === 'lajiaoliao'
      ? 'lajiaoliao'
      : allowedPlatforms.value[0]?.value || 'wsl'
  )
  const platform = computed<CustomerPlatform>({
    get: () => props.initialGroup?.platform || selectedPlatform.value,
    set: (value) => {
      if (!props.initialGroup) selectedPlatform.value = value
    }
  })
  const controlledID = ref('')
  const controlledScopes = ref<ControlledOnboardingScope[]>([])
  const controlledLoading = ref(false),
    controlledError = ref('')
  let controlledSequence = 0,
    controlledController: AbortController | undefined
  const canUseControlled = computed(
    () => operator.value && targetMode.value === 'mine' && !props.initialGroup
  )
  const challenge = ref<Row>(),
    error = ref(''),
    loading = ref(false),
    saving = ref(false),
    uncertain = ref(false)
  const groupChallenge = ref(''),
    privateChallenge = ref(''),
    friendSubmitted = ref(false),
    friendNickname = ref('')
  const friendPath = ref<'incoming' | 'private'>('incoming')
  const nativeGroupID = ref(''),
    invitationID = ref(''),
    invitationPreview = ref<Row>(),
    acceptOpen = ref(false)
  const clock = ref(Date.now())
  const controlledScope = computed<ControlledOnboardingScope | undefined>(() =>
    challenge.value
      ? challenge.value.controlled_scope
      : controlledScopes.value.find((item) => item.id === controlledID.value)
  )
  const controlledExpired = computed(() =>
    Boolean(
      controlledScope.value && controlledOnboardingExpired(controlledScope.value, clock.value)
    )
  )
  const controlledScopeMismatch = computed(() =>
    Boolean(
      (controlledID.value || challenge.value?.proof_kind === 'controlled_test') &&
        (!canUseControlled.value ||
          !controlledScope.value ||
          controlledScope.value.actor_id !== actor ||
          controlledScope.value.platform !== platform.value ||
          controlledScope.value.tenant_id !== targetTenant.value)
    )
  )
  const controlledUnavailable = computed(
    () =>
      controlledScopeMismatch.value ||
      Boolean(
        controlledScope.value && !controlledOnboardingActive(controlledScope.value, clock.value)
      )
  )
  const controlledCaptchaExempt = computed(
    () =>
      Boolean(controlledScope.value) &&
      !controlledUnavailable.value &&
      controlledScope.value?.human_verification_required === false
  )
  const controlledOptions = computed(() =>
    controlledScopes.value.filter(
      (item) =>
        (item.tenant_id === targetTenant.value && controlledOnboardingActive(item, clock.value)) ||
        item.id === controlledID.value
    )
  )
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  const invitationExpired = computed(
    () =>
      !invitationPreview.value ||
      new Date(invitationPreview.value.expires_at).getTime() <= clock.value
  )
  const friendApproveOpen = ref(false),
    confirmOpen = ref(false),
    cancelOpen = ref(false)
  let sequence = 0,
    alive = true,
    createKey = crypto.randomUUID(),
    confirmKey = crypto.randomUUID(),
    acceptKey = crypto.randomUUID(),
    friendCandidateKey = crypto.randomUUID(),
    friendApproveKey = crypto.randomUUID(),
    friendReconcileKey = crypto.randomUUID()
  const baseScope = computed(
    () =>
      `customer:${actor}:${tenant || 'operator'}:${platform.value}:${props.initialGroup?.group_id || 'new'}:${props.initialGroup?.version ?? 0}`
  )
  const scope = computed(
    () =>
      `${baseScope.value}:${targetTenant.value || targetMode.value}:onboarding${controlledID.value ? `:controlled:${controlledID.value}` : ''}`
  )
  const complete = computed(() => challenge.value?.state === 'bound')
  const expired = computed(
    () =>
      controlledExpired.value ||
      challenge.value?.state === 'expired' ||
      (['waiting', 'proven'].includes(challenge.value?.state) &&
        new Date(challenge.value?.expires_at).getTime() <= clock.value)
  )
  const terminal = computed(
    () =>
      expired.value ||
      controlledUnavailable.value ||
      ['bound', 'cancelled', 'review', 'rejected'].includes(challenge.value?.state)
  )
  const pendingOperation = computed(
    () =>
      uncertain.value ||
      (friendSubmitted.value &&
        (incomingFlow.value
          ? !friendContact.value || friendContact.value.status === 'prepared'
          : outboundAttempt.value?.status === 'prepared')) ||
      contactUnresolved.value ||
      outboundUnresolved.value ||
      [challenge.value?.friend_attempt, challenge.value?.accept_attempt].some((attempt) =>
        ['prepared', 'submitting', 'running', 'unknown'].includes(attempt?.status)
      )
  )
  const groupProven = computed(() => ['proven', 'bound', 'review'].includes(challenge.value?.state))
  const friendVerified = computed(
    () => groupProven.value || challenge.value?.identity_proven === true
  )
  const incomingFlow = computed(() => challenge.value?.friend_flow === 'incoming_contact')
  const addAccount = computed(() => robotAddAccount(challenge.value))
  const friendContact = computed(() => onboardingFriendContact(challenge.value?.friend_contact))
  const expiredRejectionConfirmed = computed(
    () =>
      friendContact.value?.action === 'reject_expired' &&
      friendContact.value.status === 'succeeded' &&
      [
        'native_friend_expired_rejection_acknowledged',
        'native_friend_expired_rejection_observed'
      ].includes(friendContact.value.result_code)
  )
  const expiredRejectionMessage = computed(() =>
    expiredRejectionConfirmed.value
      ? customerReason(friendContact.value?.result_code)
      : friendContact.value?.status === 'prepared' && !pendingOperation.value
        ? '已选中过期申请，尚未提交拒绝，也未同意此申请。'
        : friendContact.value?.status === 'failed'
          ? '过期申请未处理成功，未同意此申请。不会自动重试，请刷新原记录核对。'
          : '过期申请拒绝结果待核对，尚不能确认拒绝成功。请核对原操作，不要再次提交。'
  )
  const contactUnresolved = computed(
    () =>
      ['submitting', 'unknown'].includes(friendContact.value?.status || '') ||
      (friendContact.value?.action === 'reject_expired' &&
        friendContact.value.status === 'succeeded' &&
        !expiredRejectionConfirmed.value)
  )
  const outboundAttempt = computed<Row | undefined>(() => challenge.value?.friend_outbound)
  const outboundUnresolved = computed(() =>
    ['submitting', 'unknown'].includes(outboundAttempt.value?.status)
  )
  const canPrepareFriendCandidate = computed(
    () =>
      targetReady.value &&
      incomingFlow.value &&
      friendPath.value === 'incoming' &&
      !terminal.value &&
      !groupProven.value &&
      !friendVerified.value &&
      !friendSubmitted.value &&
      !friendContact.value &&
      !outboundUnresolved.value &&
      !challenge.value?.friend_attempt &&
      Boolean(friendNickname.value.trim()) &&
      !saving.value &&
      !loading.value &&
      !uncertain.value
  )
  const canApproveFriend = computed(
    () =>
      targetReady.value &&
      incomingFlow.value &&
      friendPath.value === 'incoming' &&
      !terminal.value &&
      !friendVerified.value &&
      !friendSubmitted.value &&
      !challenge.value?.friend_attempt &&
      !outboundUnresolved.value &&
      friendContact.value?.action === 'approve' &&
      friendContact.value?.status === 'prepared' &&
      friendContact.value?.can_approve === true &&
      friendContact.value.version === challenge.value?.version &&
      !saving.value &&
      !loading.value &&
      !uncertain.value
  )
  const canRejectExpiredFriend = computed(
    () =>
      targetReady.value &&
      incomingFlow.value &&
      friendPath.value === 'incoming' &&
      !terminal.value &&
      !friendVerified.value &&
      !friendSubmitted.value &&
      !challenge.value?.friend_attempt &&
      !outboundUnresolved.value &&
      friendContact.value?.action === 'reject_expired' &&
      friendContact.value.status === 'prepared' &&
      friendContact.value.can_reject === true &&
      friendContact.value.version === challenge.value?.version &&
      !saving.value &&
      !loading.value &&
      !uncertain.value
  )
  const canReconcileContact = computed(
    () =>
      !controlledScopeMismatch.value &&
      incomingFlow.value &&
      Boolean(friendContact.value?.id) &&
      friendContact.value?.can_reconcile === true &&
      (contactUnresolved.value ||
        (friendSubmitted.value && friendContact.value?.status === 'prepared'))
  )
  const canReconcileOutbound = computed(
    () =>
      !controlledScopeMismatch.value &&
      Boolean(outboundAttempt.value?.id) &&
      outboundAttempt.value?.can_reconcile === true &&
      (['submitting', 'submitted', 'unknown'].includes(outboundAttempt.value?.status) ||
        (friendSubmitted.value && outboundAttempt.value?.status === 'prepared'))
  )
  const expiredContactSelected = computed(() => friendContact.value?.action === 'reject_expired')
  const showPrivateProof = computed(
    () =>
      !expiredContactSelected.value &&
      (!incomingFlow.value ||
        friendPath.value === 'private' ||
        (friendContact.value?.action === 'approve' && friendContact.value.status === 'succeeded'))
  )
  const step = computed(() => {
    if (!challenge.value) return 0
    if (complete.value) return 5
    if (groupProven.value) return 4
    if (friendVerified.value) return 3
    if (showPrivateProof.value) return 2
    return 1
  })
  const setupSteps = computed(() =>
    ['分配机器人', '添加与核对', '私聊身份核验', '群归属证明', '确认绑定'].map((title, index) => {
      return {
        title,
        done: index < step.value,
        skipped: (groupProven.value || friendPath.value === 'private') && index === 1
      }
    })
  )
  const stateLabel = computed(() =>
    expired.value
      ? '已过期'
      : (
          {
            waiting: '等待核验',
            proven: '待本人确认',
            review: '待平台审核',
            bound: '绑定成功',
            cancelled: '已取消',
            expired: '已过期',
            rejected: '审核未通过'
          } as Record<string, string>
        )[challenge.value?.state || ''] || '接入核验中'
  )
  const contactLabel = (status: string) =>
    friendContact.value?.action === 'reject_expired'
      ? {
          prepared: '已选中过期申请，尚未拒绝',
          submitting: '过期申请拒绝提交中',
          succeeded: expiredRejectionConfirmed.value ? '拒绝已确认' : '过期申请拒绝结果待核对',
          failed: '过期申请未处理成功',
          unknown: '过期申请拒绝结果待核对'
        }[status] || '过期申请拒绝结果待核对'
      : {
          prepared: '已选中请求，尚未接受',
          submitting: '好友审批提交中',
          succeeded: '好友申请已接受',
          failed: '好友审批未完成',
          unknown: '好友审批结果待核对'
        }[status] || '好友审批结果待核对'
  const attemptLabel = (status: string) =>
    (
      ({
        prepared: '已准备',
        submitting: '已提交',
        succeeded: '已接受',
        failed: '未接受',
        unknown: '结果待核对',
        cancelled: '已取消'
      }) as Record<string, string>
    )[status] || '待核对'
  const friendAttemptLabel = (status: string) =>
    ({
      prepared: '好友核验已准备',
      submitting: '好友核验中',
      running: '好友核验中',
      succeeded: '好友申请已接受',
      failed: '好友核验未通过',
      unknown: '好友结果待核对',
      cancelled: '好友核验已取消'
    })[status] || '好友结果待核对'
  const outboundAttemptLabel = (status: string) =>
    ({
      prepared: '账号已核对，尚未发送',
      submitting: '好友申请提交中',
      submitted: '好友申请已提交',
      friends: '已是好友',
      failed: '好友申请未完成',
      unknown: '好友申请结果待核对'
    })[status] || '好友申请结果待核对'
  const sameSession = () =>
    alive &&
    actor === session.user?.id &&
    tenant === session.user?.tenant_id &&
    websiteRole === session.user?.role &&
    tenantRole === session.user?.tenant_role &&
    permissions === JSON.stringify([...session.permissions].sort()) &&
    view === session.console &&
    openedWorkspace === session.workspace &&
    openedAccount === session.selectedAccount
  const current = (request: number) => sameSession() && sequence === request && targetReady.value
  async function loadControlledScopes() {
    if (!sameSession() || !canUseControlled.value || saving.value || challenge.value) return
    const request = ++controlledSequence,
      selected = platform.value
    controlledController?.abort()
    controlledController = new AbortController()
    controlledLoading.value = true
    controlledError.value = ''
    try {
      const result = await api<Row>(
        `/test-tools/onboarding/scopes?platform=${selected}`,
        'GET',
        undefined,
        controlledController.signal,
        selected
      )
      if (
        !sameSession() ||
        request !== controlledSequence ||
        selected !== platform.value ||
        !canUseControlled.value
      )
        return
      if (!Array.isArray(result.items)) throw new Error('受控接入范围暂无法核对，请重新刷新')
      const next = result.items
        .map(controlledOnboardingScope)
        .filter(
          (item: ControlledOnboardingScope) => item.actor_id === actor && item.platform === selected
        )
      for (const item of next) {
        const previous = controlledScopes.value.find((value) => value.id === item.id)
        if (
          previous &&
          (!controlledOnboardingSameScope(previous, item) || item.version < previous.version)
        )
          throw new Error('原受控范围不能变更或续期，请查询原记录或创建新范围')
      }
      controlledScopes.value = next
      updateExpiry()
    } catch (e) {
      if (
        sameSession() &&
        request === controlledSequence &&
        selected === platform.value &&
        !(e instanceof Error && e.name === 'AbortError')
      )
        controlledError.value = (e as Error).message
    } finally {
      if (request === controlledSequence) controlledLoading.value = false
    }
  }
  function openControlledTools() {
    if (!sameSession() || saving.value || loading.value || session.user?.role !== 'superadmin')
      return
    const selected = platform.value
    sessionStorage.removeItem(controlledOnboardingSelectionKey(actor!, tenant, selected))
    close()
    emit('controlled-tools', selected)
  }
  async function loadCustomers(more = false) {
    if (!sameSession() || challenge.value || (more && customerNext.value < 0)) return
    const request = ++customerSequence,
      selected = platform.value
    customerController?.abort()
    customerController = new AbortController()
    customersLoading.value = true
    customerError.value = ''
    try {
      const query = new URLSearchParams({
        platform: selected,
        search: customerSearch.value,
        offset: String(more ? customerNext.value : 0),
        limit: '20'
      })
      const value = await api<Row>(
        `/group-binding/context?${query}`,
        'GET',
        undefined,
        customerController.signal,
        selected
      )
      if (!sameSession() || request !== customerSequence || platform.value !== selected) return
      if (
        operator.value &&
        (value.mode !== 'operator_demo' || !value.workspace?.id || !value.default_target_tenant_id)
      )
        throw new Error('接入权限已变化，请关闭后重新打开')
      const firstContext = !context.value
      context.value = value
      if (
        firstContext &&
        operator.value &&
        props.initialGroup?.tenant_id &&
        props.initialGroup.tenant_id !== value.default_target_tenant_id
      ) {
        if (!canOnboardForCustomer.value) throw new Error('当前不可代此客户重新核验')
        targetMode.value = 'customer'
        selectedCustomer.value = String(props.initialGroup.tenant_id)
      }
      customers.value = [
        ...new Map<string, Row>(
          [...(more ? customers.value : []), ...(value.customers || [])].map((item: Row) => [
            String(item.tenant_id || item.id),
            { ...item, tenant_id: item.tenant_id || item.id }
          ])
        ).values()
      ]
      customerNext.value = Number.isInteger(value.next_offset) ? value.next_offset : -1
    } catch (e) {
      if (request === customerSequence && sameSession() && !customerController.signal.aborted)
        customerError.value = (e as Error).message
    } finally {
      if (request === customerSequence) customersLoading.value = false
    }
  }
  function searchCustomers(value: string) {
    customerSearch.value = value.trim()
    clearTimeout(customerTimer)
    customerTimer = setTimeout(() => void loadCustomers(), 250)
  }
  function customerChanged() {
    selectedCustomerRow.value = customerOptions.value.find(
      (item) => item.tenant_id === selectedCustomer.value
    )
    createKey = crypto.randomUUID()
  }
  function saveProgressDraft() {
    if (sameSession() && challenge.value?.id && (!terminal.value || pendingOperation.value))
      session.saveDraft(scope.value, {
        id: challenge.value.id,
        friend_submitted: friendSubmitted.value,
        friend_path: friendPath.value,
        friend_nickname: friendNickname.value
      })
  }
  function applyProgress(value: Row) {
    if (value.platform !== platform.value || !value.id || !value.robot)
      throw new Error('接入范围已变化，请关闭后重新核对')
    if (props.initialGroup && value.group?.id !== props.initialGroup.group_id)
      throw new Error('核验到的群与所选群不一致，请关闭后重新核对')
    if (challenge.value?.id && value.id !== challenge.value.id)
      throw new Error('接入记录已变化，请关闭后重新核对')
    if (
      !Number.isSafeInteger(value.version) ||
      value.version < 1 ||
      (challenge.value && value.version < challenge.value.version) ||
      !Number.isFinite(Date.parse(value.expires_at))
    )
      throw new Error('接入版本或有效期已变化，请刷新原记录核对')
    const controlled =
      value.controlled_scope == null ? undefined : controlledOnboardingScope(value.controlled_scope)
    if (controlled || value.proof_kind === 'controlled_test' || controlledID.value) {
      if (
        !controlled ||
        !canUseControlled.value ||
        value.proof_kind !== 'controlled_test' ||
        controlled.id !== controlledID.value ||
        controlled.actor_id !== actor ||
        controlled.platform !== platform.value ||
        controlled.tenant_id !== targetTenant.value ||
        controlled.account_id !== value.robot.id ||
        (value.group &&
          (value.group.id !== controlled.group_id ||
            value.group.native_id !== controlled.group.native_id)) ||
        (controlledScope.value &&
          (!controlledOnboardingSameScope(controlled, controlledScope.value) ||
            controlled.version < controlledScope.value.version))
      )
        throw new Error('受控接入范围已变化，请刷新原记录核对')
    }
    const contact = onboardingFriendContact(value.friend_contact)
    if (
      friendContact.value &&
      (contact?.id !== friendContact.value.id || contact?.action !== friendContact.value.action)
    )
      throw new Error('好友请求记录已变化，请刷新原记录核对')
    if (operator.value) {
      if (
        value.mode !== 'operator_demo' ||
        !value.customer?.tenant_id ||
        value.customer.tenant_id !== targetTenant.value
      )
        throw new Error('群组所属客户已变化，请关闭后重新核对')
      if (targetMode.value === 'customer') selectedCustomerRow.value = value.customer
    }
    // Proof text and contact metadata never enter the persisted progress draft.
    const { group_challenge, private_challenge, ...progress } = value
    delete progress.friend_challenge
    if (contact) progress.friend_contact = contact
    if (controlled) progress.controlled_scope = controlled
    challenge.value = progress
    clock.value = Date.now()
    if (controlled) nativeGroupID.value = controlled.group.native_id
    else if (props.initialGroup) nativeGroupID.value = value.group.native_id
    if (!value.can_accept_invitation || invitationPreview.value?.version !== value.version)
      invitationPreview.value = undefined
    if (!groupChallenge.value && group_challenge) groupChallenge.value = group_challenge
    if (!privateChallenge.value && private_challenge) privateChallenge.value = private_challenge
    if (contact && contact.status !== 'prepared') friendSubmitted.value = true
    if (value.friend_flow !== 'incoming_contact') friendPath.value = 'private'
    if (terminal.value) {
      groupChallenge.value = privateChallenge.value = ''
      if (pendingOperation.value) saveProgressDraft()
      else if (value.state !== 'review') session.saveDraft(scope.value, {})
    } else saveProgressDraft()
  }
  async function fetchProgress(id: string) {
    const request = ++sequence,
      selected = platform.value
    loading.value = true
    error.value = ''
    try {
      const value = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}`,
        'GET',
        undefined,
        undefined,
        selected
      )
      if (!current(request) || platform.value !== selected) return
      applyProgress(value)
      uncertain.value = false
      if (complete.value) emit('bound')
    } catch (e) {
      if (current(request) && platform.value === selected) error.value = (e as Error).message
    } finally {
      if (current(request)) loading.value = false
    }
  }
  function refresh() {
    if (challenge.value?.id) void fetchProgress(challenge.value.id)
  }
  function updateExpiry() {
    clearTimeout(expiryTimer)
    clock.value = Date.now()
    const deadlines = [
      ...(['waiting', 'proven'].includes(challenge.value?.state)
        ? [challenge.value?.expires_at]
        : []),
      invitationPreview.value?.expires_at,
      controlledScope.value?.expires_at
    ]
      .map((value) => new Date(value).getTime())
      .filter((value) => Number.isFinite(value) && value > clock.value)
    if (deadlines.length)
      expiryTimer = setTimeout(
        updateExpiry,
        Math.min(Math.min(...deadlines) - clock.value, 2147483647)
      )
  }
  function restart() {
    if (
      !(expired.value || challenge.value?.state === 'cancelled') ||
      saving.value ||
      loading.value ||
      pendingOperation.value ||
      controlledUnavailable.value ||
      !current(sequence)
    )
      return
    sequence++
    session.saveDraft(scope.value, {})
    challenge.value = undefined
    groupChallenge.value = privateChallenge.value = friendNickname.value = error.value = ''
    invitationPreview.value = undefined
    friendApproveOpen.value = confirmOpen.value = cancelOpen.value = acceptOpen.value = false
    friendSubmitted.value = false
    friendPath.value = 'incoming'
    nativeGroupID.value = props.initialGroup?.native_id || ''
    invitationID.value = ''
    createKey = crypto.randomUUID()
    confirmKey = crypto.randomUUID()
    acceptKey = crypto.randomUUID()
    friendCandidateKey = crypto.randomUUID()
    friendApproveKey = crypto.randomUUID()
    friendReconcileKey = crypto.randomUUID()
    void create()
  }
  async function create() {
    updateExpiry()
    if (
      saving.value ||
      loading.value ||
      uncertain.value ||
      controlledUnavailable.value ||
      (canUseControlled.value && controlledLoading.value) ||
      !targetReady.value ||
      !allowedPlatforms.value.some((item) => item.value === platform.value) ||
      !current(sequence)
    )
      return
    const request = ++sequence,
      selected = platform.value,
      key = createKey,
      input = {
        platform: platform.value,
        ...(controlledID.value ? { controlled_scope_id: controlledID.value } : {}),
        ...(operator.value ? { target_tenant_id: targetTenant.value } : {}),
        ...(props.initialGroup
          ? {
              restore_group_id: props.initialGroup.group_id,
              binding_version: props.initialGroup.version
            }
          : {})
      }
    saving.value = true
    error.value = ''
    try {
      const exempt = controlledCaptchaExempt.value
      const ticket = exempt
        ? undefined
        : await webCaptcha.value?.open({
            purpose: 'group_binding_create',
            platform: selected
          })
      updateExpiry()
      if (
        (!exempt && !ticket) ||
        !current(request) ||
        selected !== platform.value ||
        controlledUnavailable.value
      )
        return
      const result = await api<Row>(
        '/group-binding/challenges',
        'POST',
        input,
        undefined,
        selected,
        key,
        ticket ?? undefined
      )
      if (!current(request) || selected !== platform.value) return
      applyProgress(result)
    } catch (e) {
      if (!current(request)) return
      error.value = (e as Error).message
      uncertain.value = !(e instanceof ApiError)
    } finally {
      if (current(request)) saving.value = false
    }
  }
  async function mutate(action: 'confirm' | 'cancel') {
    updateExpiry()
    if (
      !challenge.value?.id ||
      saving.value ||
      loading.value ||
      uncertain.value ||
      expired.value ||
      controlledUnavailable.value ||
      (action === 'cancel' && pendingOperation.value) ||
      (action === 'confirm' && terminal.value) ||
      !challenge.value[action === 'confirm' ? 'can_confirm' : 'can_cancel']
    )
      return
    const request = ++sequence,
      id = challenge.value.id,
      selected = platform.value
    saving.value = true
    error.value = ''
    try {
      const value = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/${action}`,
        'POST',
        { version: challenge.value.version },
        undefined,
        selected,
        action === 'confirm' ? confirmKey : undefined
      )
      if (!current(request) || id !== challenge.value?.id || selected !== platform.value) return
      applyProgress(value)
      confirmOpen.value = cancelOpen.value = false
      if (complete.value) emit('bound')
    } catch (e) {
      if (!current(request)) return
      error.value = (e as Error).message
      uncertain.value = !(e instanceof ApiError)
      confirmOpen.value = cancelOpen.value = false
    } finally {
      if (current(request)) saving.value = false
    }
  }
  function applyFriendResult(value: Row, operation: string, kind: 'contact' | 'legacy') {
    if (value.robot) {
      if (value[kind === 'contact' ? 'friend_contact' : 'friend_attempt']?.id !== operation)
        throw new Error('好友操作记录已变化，请刷新原记录核对')
      applyProgress(value)
      return
    }
    if (!value.status || value.id !== operation) throw new Error('好友操作结果待核对，请刷新原记录')
    // Reconciliation may return only the durable attempt, without contact metadata.
    if (kind === 'contact') {
      const contact = onboardingFriendContact({
        ...friendContact.value,
        status: value.status,
        result_code: value.result_code,
        can_approve: false,
        can_reject: false,
        can_reconcile: ['submitting', 'unknown'].includes(value.status)
      })
      challenge.value = { ...challenge.value, friend_contact: contact }
    } else challenge.value = { ...challenge.value, friend_attempt: value, can_verify_friend: false }
    saveProgressDraft()
  }
  async function prepareFriendCandidate() {
    updateExpiry()
    if (!canPrepareFriendCandidate.value || !challenge.value?.id || !current(sequence)) return
    const request = ++sequence,
      id = challenge.value.id,
      selected = platform.value,
      version = challenge.value.version,
      nickname = friendNickname.value.trim(),
      key = friendCandidateKey
    let rejectExpired = false
    saving.value = true
    error.value = ''
    try {
      const value = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/friend-candidate`,
        'POST',
        { version, nickname },
        undefined,
        selected,
        key
      )
      if (
        !current(request) ||
        id !== challenge.value?.id ||
        selected !== platform.value ||
        terminal.value ||
        Date.parse(challenge.value.expires_at) <= Date.now()
      )
        return
      if (
        value.friend_flow !== 'incoming_contact' ||
        value.friend_contact?.status !== 'prepared' ||
        value.friend_contact?.name !== nickname
      )
        throw new Error('好友请求选择结果待核对，请刷新本次接入记录')
      applyProgress(value)
      rejectExpired = friendContact.value?.action === 'reject_expired'
    } catch (e) {
      if (
        !current(request) ||
        id !== challenge.value?.id ||
        selected !== platform.value ||
        terminal.value ||
        Date.parse(challenge.value.expires_at) <= Date.now()
      )
        return
      error.value =
        e instanceof ApiError
          ? customerReason(e.code)
          : '好友请求选择结果待核对，请刷新本次接入记录'
      uncertain.value = !(e instanceof ApiError)
      if (!uncertain.value) friendCandidateKey = crypto.randomUUID()
    } finally {
      if (current(request)) saving.value = false
    }
    // A selected old request still needs an explicit confirmation when exempt.
    if (
      rejectExpired &&
      current(request) &&
      id === challenge.value?.id &&
      selected === platform.value
    )
      await rejectExpiredFriend()
  }
  async function approveFriend() {
    if (!friendApproveOpen.value) return
    await submitFriendContact(
      friendContact.value?.action === 'reject_expired' ? 'reject_expired' : 'approve'
    )
  }
  async function rejectExpiredFriend() {
    if (controlledCaptchaExempt.value) {
      friendApproveOpen.value = true
      return
    }
    await submitFriendContact('reject_expired')
  }
  async function submitFriendContact(action: 'approve' | 'reject_expired') {
    updateExpiry()
    if (
      !(action === 'approve' ? canApproveFriend.value : canRejectExpiredFriend.value) ||
      !challenge.value?.id ||
      !current(sequence)
    )
      return
    friendApproveOpen.value = false
    const request = ++sequence,
      id = challenge.value.id,
      selected = platform.value,
      version = challenge.value.version,
      contact = { ...friendContact.value! },
      operation = contact.id,
      key = friendApproveKey
    let submitted = false
    saving.value = true
    error.value = ''
    try {
      const exempt = controlledCaptchaExempt.value
      const ticket = exempt
        ? undefined
        : await webCaptcha.value?.open({
            purpose: 'group_binding_friend',
            platform: selected,
            target: id
          })
      updateExpiry()
      if (
        (!exempt && !ticket) ||
        controlledUnavailable.value ||
        terminal.value ||
        !current(request) ||
        id !== challenge.value?.id ||
        selected !== platform.value ||
        version !== challenge.value.version ||
        operation !== friendContact.value?.id ||
        contact.version !== friendContact.value?.version ||
        contact.name !== friendContact.value?.name ||
        contact.requested_at !== friendContact.value?.requested_at ||
        action !== friendContact.value?.action ||
        friendContact.value?.status !== 'prepared' ||
        !(action === 'approve'
          ? friendContact.value?.can_approve
          : friendContact.value?.can_reject) ||
        friendSubmitted.value ||
        friendPath.value !== 'incoming'
      )
        return
      friendSubmitted.value = true
      submitted = true
      saveProgressDraft()
      const value = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/${action === 'approve' ? 'friend-approve' : 'friend-reject'}`,
        'POST',
        { version, operation_id: operation },
        undefined,
        selected,
        key,
        ticket ?? undefined
      )
      if (!current(request) || id !== challenge.value?.id || selected !== platform.value) return
      if (
        value.friend_flow !== 'incoming_contact' ||
        value.friend_contact?.id !== operation ||
        onboardingFriendContact(value.friend_contact)?.action !== action ||
        !value.robot
      )
        throw new Error('好友审批结果待核对，请刷新原记录')
      applyProgress(value)
    } catch (e) {
      if (!current(request) || expired.value) return
      const unresolved =
        !(e instanceof ApiError) ||
        (!e.code.startsWith('captcha_') &&
          (e.status >= 500 || /unknown|unresolved|submission_limit/.test(e.code)))
      error.value = unresolved
        ? action === 'reject_expired'
          ? '过期申请拒绝结果待确认，请刷新原记录核对，不要重新提交。'
          : '好友审批提交结果待确认，请刷新原记录核对，不要重新提交。'
        : action === 'reject_expired' && submitted
          ? '过期申请未处理成功，未同意此申请。请刷新原记录核对，不会自动重试。'
          : customerReason(e instanceof ApiError ? e.code : '')
      uncertain.value = unresolved
      if (!unresolved && !(action === 'reject_expired' && submitted)) {
        friendSubmitted.value = false
        friendApproveKey = crypto.randomUUID()
        saveProgressDraft()
      }
    } finally {
      if (current(request)) saving.value = false
    }
  }
  async function reconcileOutboundFriend() {
    if (
      !canReconcileOutbound.value ||
      !challenge.value?.id ||
      saving.value ||
      loading.value ||
      controlledScopeMismatch.value ||
      !current(sequence)
    )
      return
    const request = ++sequence,
      id = challenge.value.id,
      operation = outboundAttempt.value!.id,
      selected = platform.value,
      version = challenge.value.version,
      key = friendReconcileKey
    saving.value = true
    error.value = ''
    try {
      const value = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/outbound-attempts/${encodeURIComponent(operation)}/reconcile`,
        'POST',
        { version },
        undefined,
        selected,
        key
      )
      if (!current(request) || id !== challenge.value?.id || selected !== platform.value) return
      if (value.friend_outbound?.id !== operation || !value.robot)
        throw new Error('好友操作记录已变化，请刷新原记录核对')
      applyProgress(value)
      friendReconcileKey = crypto.randomUUID()
      uncertain.value = false
    } catch (e) {
      if (current(request)) error.value = (e as Error).message
    } finally {
      if (current(request)) saving.value = false
    }
  }
  async function reconcileFriend(kind: 'contact' | 'legacy') {
    const attempt = kind === 'contact' ? friendContact.value : challenge.value?.friend_attempt
    if (
      !challenge.value?.id ||
      !attempt?.id ||
      (kind === 'contact' ? !canReconcileContact.value : attempt.status !== 'unknown') ||
      saving.value ||
      loading.value ||
      controlledScopeMismatch.value ||
      !current(sequence)
    )
      return
    const request = ++sequence,
      id = challenge.value.id,
      operation = attempt.id,
      selected = platform.value
    saving.value = true
    error.value = ''
    try {
      const value = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/friend-attempts/${encodeURIComponent(operation)}/reconcile`,
        'POST',
        {},
        undefined,
        selected
      )
      if (!current(request) || id !== challenge.value?.id || selected !== platform.value) return
      applyFriendResult(value, operation, kind)
      if (kind === 'contact' && friendContact.value?.action === 'reject_expired' && !value.robot) {
        const progress = await api<Row>(
          `/group-binding/challenges/${encodeURIComponent(id)}`,
          'GET',
          undefined,
          undefined,
          selected
        )
        if (!current(request) || id !== challenge.value?.id || selected !== platform.value) return
        applyProgress(progress)
      }
      uncertain.value = false
    } catch (e) {
      if (current(request)) error.value = (e as Error).message
    } finally {
      if (current(request)) saving.value = false
    }
  }
  async function reconcile() {
    if (
      !challenge.value?.accept_attempt?.id ||
      saving.value ||
      loading.value ||
      controlledScopeMismatch.value ||
      !current(sequence)
    )
      return
    const request = ++sequence,
      id = challenge.value.id,
      operation = challenge.value.accept_attempt.id,
      selected = platform.value
    saving.value = true
    error.value = ''
    try {
      const attempt = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/accept-attempts/${encodeURIComponent(operation)}/reconcile`,
        'POST',
        {},
        undefined,
        selected
      )
      if (!current(request) || id !== challenge.value?.id) return
      challenge.value = { ...challenge.value, accept_attempt: attempt }
    } catch (e) {
      if (current(request)) error.value = (e as Error).message
    } finally {
      if (current(request)) saving.value = false
    }
  }
  async function prepareInvitation() {
    updateExpiry()
    if (
      !challenge.value?.can_accept_invitation ||
      !friendVerified.value ||
      terminal.value ||
      saving.value ||
      loading.value ||
      uncertain.value ||
      !nativeGroupID.value.trim()
    )
      return
    const request = ++sequence,
      id = challenge.value.id,
      selected = platform.value
    saving.value = true
    error.value = ''
    invitationPreview.value = undefined
    try {
      const result = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/invitation-prepare`,
        'POST',
        {
          version: challenge.value.version,
          native_group_id: nativeGroupID.value.trim(),
          ...(invitationID.value.trim() ? { invitation_id: invitationID.value.trim() } : {})
        },
        undefined,
        selected
      )
      if (!current(request) || id !== challenge.value?.id || selected !== platform.value) return
      if (
        result.native_group_id !== nativeGroupID.value.trim() ||
        result.version !== challenge.value.version ||
        !result.invitation_id
      )
        throw new Error('邀请已变化，请重新核对')
      invitationPreview.value = result
      acceptKey = crypto.randomUUID()
    } catch (e) {
      if (current(request)) error.value = (e as Error).message
    } finally {
      if (current(request)) saving.value = false
    }
  }
  async function acceptInvitation() {
    updateExpiry()
    if (
      !challenge.value?.can_accept_invitation ||
      !friendVerified.value ||
      terminal.value ||
      !invitationPreview.value ||
      saving.value ||
      loading.value ||
      uncertain.value ||
      invitationExpired.value
    )
      return
    const request = ++sequence,
      id = challenge.value.id,
      selected = platform.value,
      preview = { ...invitationPreview.value }
    saving.value = true
    error.value = ''
    try {
      const attempt = await api<Row>(
        `/group-binding/challenges/${encodeURIComponent(id)}/invitation-accept`,
        'POST',
        {
          version: preview.version,
          native_group_id: preview.native_group_id,
          invitation_id: preview.invitation_id
        },
        undefined,
        selected,
        acceptKey
      )
      if (!current(request) || id !== challenge.value?.id || selected !== platform.value) return
      challenge.value = {
        ...challenge.value,
        can_accept_invitation: false,
        accept_attempt: attempt
      }
      invitationPreview.value = undefined
    } catch (e) {
      if (!current(request)) return
      error.value = (e as Error).message
      uncertain.value = !(e instanceof ApiError)
    } finally {
      if (current(request)) {
        saving.value = false
        acceptOpen.value = false
      }
    }
  }
  async function copy(value: string, label = '核验内容') {
    try {
      await navigator.clipboard.writeText(value)
      ElMessage.success(`${label}已复制`)
    } catch {
      error.value = `复制失败，请选中${label}后复制`
    }
  }
  function openRuntime() {
    const value = challenge.value
    if (!current(sequence) || !complete.value || !value?.group || saving.value || loading.value)
      return
    emit('manage', {
      group_id: value.group.id,
      name: value.group.name,
      native_id: value.group.native_id,
      platform: value.platform,
      tenant_id: value.customer?.tenant_id || targetTenant.value,
      primary_account_id: value.robot.id,
      robot_nickname: robotLabel(value),
      active: true,
      business_enabled: false
    })
  }
  function close() {
    if (!alive) return
    saveProgressDraft()
    alive = false
    sequence++
    clearTimeout(expiryTimer)
    webCaptcha.value?.cancel()
    groupChallenge.value = privateChallenge.value = ''
    emit('close')
  }
  watch(
    () => [
      challenge.value?.state,
      challenge.value?.expires_at,
      invitationPreview.value?.expires_at,
      controlledScope.value?.expires_at,
      controlledScope.value?.state
    ],
    updateExpiry,
    { immediate: true, flush: 'sync' }
  )
  watch(
    () => expired.value || controlledUnavailable.value,
    (value) => {
      if (!value) return
      error.value = ''
      groupChallenge.value = privateChallenge.value = ''
      invitationPreview.value = undefined
      friendApproveOpen.value = confirmOpen.value = cancelOpen.value = acceptOpen.value = false
      webCaptcha.value?.cancel()
    },
    { flush: 'sync' }
  )
  watch(canApproveFriend, (value) => {
    if (!value) friendApproveOpen.value = false
  })
  watch(friendPath, () => {
    friendApproveOpen.value = false
    saveProgressDraft()
  })
  onMounted(() => {
    document.addEventListener('visibilitychange', updateExpiry)
    window.addEventListener('focus', updateExpiry)
  })
  watch(
    baseScope,
    () => {
      context.value = undefined
      challenge.value = undefined
      targetMode.value = 'mine'
      selectedCustomer.value = String(props.initialGroup?.tenant_id || '')
      selectedCustomerRow.value = undefined
      customerSearch.value = ''
      customerError.value = ''
      customers.value = []
      customerNext.value = -1
      customerSequence++
      customerController?.abort()
      clearTimeout(customerTimer)
      void loadCustomers()
    },
    { immediate: true }
  )
  watch(
    () => `${baseScope.value}:${targetMode.value}`,
    () => {
      controlledSequence++
      controlledController?.abort()
      controlledScopes.value = []
      controlledLoading.value = false
      controlledError.value = ''
      controlledID.value = canUseControlled.value
        ? sessionStorage.getItem(
            controlledOnboardingSelectionKey(actor!, tenant, platform.value)
          ) || ''
        : ''
      void loadControlledScopes()
    },
    { immediate: true, flush: 'sync' }
  )
  watch(
    controlledID,
    (value) => {
      if (!sameSession() || !canUseControlled.value) return
      const key = controlledOnboardingSelectionKey(actor!, tenant, platform.value)
      if (value) sessionStorage.setItem(key, value)
      else sessionStorage.removeItem(key)
    },
    { flush: 'sync' }
  )
  watch(
    scope,
    () => {
      sequence++
      webCaptcha.value?.cancel()
      challenge.value = undefined
      groupChallenge.value = privateChallenge.value = friendNickname.value = error.value = ''
      loading.value = saving.value = uncertain.value = false
      friendSubmitted.value = false
      friendPath.value = 'incoming'
      createKey = crypto.randomUUID()
      confirmKey = crypto.randomUUID()
      acceptKey = crypto.randomUUID()
      friendCandidateKey = crypto.randomUUID()
      friendApproveKey = crypto.randomUUID()
      friendReconcileKey = crypto.randomUUID()
      invitationPreview.value = undefined
      friendApproveOpen.value = confirmOpen.value = cancelOpen.value = acceptOpen.value = false
      nativeGroupID.value = props.initialGroup?.native_id || ''
      invitationID.value = ''
      const draft = session.drafts[scope.value] as Row | undefined
      if (draft?.id && targetReady.value) {
        friendSubmitted.value = draft.friend_submitted === true
        friendPath.value = draft.friend_path === 'private' ? 'private' : 'incoming'
        friendNickname.value =
          typeof draft.friend_nickname === 'string' ? draft.friend_nickname.slice(0, 64) : ''
        void fetchProgress(draft.id)
      }
    },
    { immediate: true, flush: 'sync' }
  )
  watch([nativeGroupID, invitationID], () => {
    invitationPreview.value = undefined
    acceptOpen.value = false
  })
  watch(friendNickname, () => {
    if (!saving.value) friendCandidateKey = crypto.randomUUID()
    saveProgressDraft()
  })
  watch(
    () => [
      session.user?.id,
      session.user?.tenant_id,
      session.user?.role,
      session.user?.tenant_role,
      JSON.stringify([...session.permissions].sort()),
      session.console,
      session.workspace,
      session.selectedAccount
    ],
    () => {
      if (sameSession()) return
      sequence++
      groupChallenge.value = privateChallenge.value = friendNickname.value = ''
      friendApproveOpen.value = confirmOpen.value = cancelOpen.value = acceptOpen.value = false
      close()
    },
    { flush: 'sync' }
  )
  onBeforeUnmount(() => {
    webCaptcha.value?.cancel()
    alive = false
    sequence++
    customerSequence++
    customerController?.abort()
    controlledSequence++
    controlledController?.abort()
    clearTimeout(customerTimer)
    clearTimeout(expiryTimer)
    document.removeEventListener('visibilitychange', updateExpiry)
    window.removeEventListener('focus', updateExpiry)
    groupChallenge.value = privateChallenge.value = friendNickname.value = ''
  })
</script>
<style scoped>
  :global(.onboarding-dialog .el-select__input:focus-visible) {
    outline: none;
    box-shadow: none;
  }
  .onboarding-steps {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 8px;
    list-style: none;
    padding: 0 0 20px;
    margin: 0 0 20px;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .onboarding-steps li {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .onboarding-steps li > span {
    font-family: var(--ui-mono);
    font-variant-numeric: tabular-nums;
  }
  .onboarding-steps strong {
    font-size: 12px;
    font-weight: 500;
  }
  .onboarding-steps .is-current {
    color: var(--el-text-color-primary);
  }
  .onboarding-steps .is-current strong {
    font-weight: 600;
  }
  .onboarding-steps .is-done > span {
    color: var(--el-color-success);
  }
  .onboarding-steps small {
    font-size: 11px;
  }
  .onboarding-consent {
    color: var(--el-text-color-primary);
  }
  .onboarding-friend-paths {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: stretch;
    width: 100%;
    margin-bottom: 12px;
  }
  .onboarding-friend-target {
    margin-bottom: 16px;
  }
  .onboarding-friend-target.onboarding-details {
    grid-template-columns: 84px minmax(0, 1fr);
  }
  .onboarding-friend-target .el-form-item {
    margin-bottom: 8px;
  }
  .onboarding-friend-paths :deep(.el-radio-button__inner) {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 40px;
    height: 100%;
    width: 100%;
    padding: 6px 10px;
    white-space: normal;
    line-height: 1.5;
  }
  .onboarding-section .onboarding-private-title {
    margin-top: 8px;
  }
  .onboarding-section > .el-button,
  .onboarding-section > .el-tag {
    max-width: 100%;
    margin-bottom: 8px;
  }
  .onboarding-section > .el-button {
    height: auto;
    min-height: 36px;
    white-space: normal;
    line-height: 1.5;
  }
  .onboarding-section > .el-alert {
    margin-top: 12px;
  }
  .onboarding-platforms {
    margin-bottom: 0;
  }
  .onboarding-platforms :deep(.el-radio-button__inner) {
    white-space: normal;
  }
  .onboarding-dialog .el-radio-group {
    gap: 8px;
  }
  .onboarding-dialog .el-radio {
    margin-right: 8px;
  }
  .onboarding-steps + .onboarding-details {
    margin-bottom: 24px;
  }
  .onboarding-platforms {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .onboarding-platforms :deep(.el-radio-button__inner) {
    display: flex;
    align-items: center;
    gap: 8px;
    border-radius: 6px;
    border: 1px solid var(--el-border-color);
  }
  .onboarding-platforms img {
    width: 20px;
    height: 20px;
  }
  .onboarding-hint {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    line-height: 1.7;
  }
  .onboarding-state {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    align-items: center;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    padding: 12px 0;
  }
  .onboarding-expired p {
    margin: 4px 0 0;
    line-height: 1.6;
  }
  .onboarding-controlled-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 8px;
  }
  .onboarding-controlled-actions .el-button {
    margin: 0;
    max-width: 100%;
    white-space: normal;
    height: auto;
    min-height: 36px;
  }
  .onboarding-controlled-notice :deep(.el-alert__content) {
    width: 100%;
    min-width: 0;
  }
  .onboarding-controlled-notice :deep(.el-alert__description),
  .onboarding-controlled-expired :deep(.el-alert__description) {
    color: var(--el-text-color-regular);
  }
  .onboarding-controlled-expired p {
    margin: 4px 0 0;
    line-height: 1.6;
  }
  .onboarding-section {
    border-top: 1px solid var(--el-border-color-light);
    padding: 20px 0;
  }
  .onboarding-section h3 {
    font-size: 14px;
    font-weight: 600;
    margin: 0 0 14px;
  }
  .onboarding-robot {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .onboarding-robot img {
    width: 32px;
    height: 32px;
    flex-shrink: 0;
  }
  .onboarding-robot strong,
  .onboarding-robot p {
    overflow-wrap: anywhere;
  }
  .onboarding-robot p {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    margin: 6px 0 0;
  }
  .onboarding-public-account {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .onboarding-public-account > span {
    min-width: 0;
    overflow-wrap: anywhere;
    user-select: all;
  }
  .onboarding-public-account > .el-button {
    flex-shrink: 0;
  }
  .onboarding-code {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px;
    background: var(--el-fill-color-light);
    border: 1px solid var(--el-border-color-light);
    border-radius: 6px;
  }
  .onboarding-code code {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
    user-select: all;
  }
  .onboarding-code > .el-button {
    flex-shrink: 0;
  }
  .onboarding-proof {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--el-color-success);
    font-size: 13px;
  }
  .onboarding-details {
    display: grid;
    grid-template-columns: 68px minmax(0, 1fr);
    gap: 12px;
    margin-block: 12px;
  }
  .onboarding-details dt {
    color: var(--el-text-color-secondary);
  }
  .onboarding-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .el-alert {
    margin-bottom: 16px;
  }
  .onboarding-error :deep(.el-alert__content) {
    min-width: 0;
  }
  .onboarding-error :deep(.el-alert__title),
  .onboarding-error :deep(.el-alert__description) {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  @media (max-width: 420px) {
    .onboarding-steps {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px 8px;
    }
  }
</style>
<style>
  .customer-scope-dialog {
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 24px);
    display: flex;
    flex-direction: column;
    margin: 12px auto;
    border-radius: 8px;
  }
  .customer-scope-dialog > .el-dialog__body {
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .customer-scope-dialog > .el-dialog__header,
  .customer-scope-dialog > .el-dialog__footer {
    flex-shrink: 0;
  }
  .customer-scope-dialog > .el-dialog__footer {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
  }
  .customer-scope-dialog > .el-dialog__footer .el-button {
    margin: 0;
    max-width: 100%;
    white-space: normal;
    height: auto;
    min-height: 36px;
  }
  .customer-scope-dialog .el-select,
  .customer-scope-dialog .el-input {
    width: 100%;
  }
  .customer-scope-dialog p {
    overflow-wrap: anywhere;
  }
</style>
