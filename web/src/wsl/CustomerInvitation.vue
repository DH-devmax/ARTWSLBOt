<template>
  <main class="customer-activation">
    <header class="activation-brand">
      <img src="/wsl-logo.png" alt="" />
      <strong>群组管理平台</strong>
      <ElButton text :icon="Close" aria-label="关闭邀请" @click="$emit('close')" />
    </header>
    <section class="activation-content" aria-labelledby="activation-title">
      <span class="activation-icon"
        ><ElIcon><Message /></ElIcon
      ></span>
      <h1 id="activation-title">{{ completed ? '邀请已接受' : '开启你的群组工作空间' }}</h1>
      <p class="activation-description">{{
        completed ? '使用本人账号登录，继续接入群组。' : '客户负责人邀请'
      }}</p>
      <ElSkeleton v-if="loading" :rows="4" animated />
      <ElAlert
        v-else-if="!token && !completed"
        title="请从完整的邀请链接打开此页面"
        type="warning"
        :closable="false"
      />
      <ElAlert
        v-else-if="preview && preview.state !== 'pending' && !completed"
        :title="
          preview.state === 'accepted'
            ? '此邀请已接受，请登录本人账号'
            : '此邀请已过期或撤销，请联系平台重新邀请'
        "
        type="warning"
        :closable="false"
      />
      <template v-else-if="completed">
        <ElButton
          type="primary"
          :icon="ArrowRight"
          class="activation-submit"
          @click="$emit('complete')"
          >进入用户中心</ElButton
        >
      </template>
      <template v-else-if="user && preview?.mode === 'new'">
        <ElAlert
          title="此链接用于创建新客户账号，请退出当前网站账号后继续"
          type="info"
          :closable="false"
        />
        <ElButton class="activation-submit" :icon="User" @click="$emit('logout')"
          >退出当前账号</ElButton
        >
      </template>
      <template v-else-if="user && preview">
        <dl class="activation-account"
          ><dt>当前账号</dt><dd>{{ user.username }}</dd></dl
        >
        <ElAlert v-if="error" :title="error" type="error" :closable="false" />
        <ElButton
          type="primary"
          :loading="saving"
          :disabled="uncertain"
          class="activation-submit"
          @click="activate"
          >接受邀请</ElButton
        >
      </template>
      <ElForm v-else-if="preview" label-position="top" @submit.prevent="activate">
        <template v-if="existing">
          <p class="activation-description">登录本人网站账号后接受邀请。</p>
          <ElButton type="primary" :icon="User" class="activation-submit" @click="$emit('login')"
            >前往登录</ElButton
          >
        </template>
        <template v-else>
          <ElFormItem label="网站用户名">
            <ElInput
              v-model="username"
              autocomplete="username"
              :disabled="saving || uncertain"
              maxlength="80"
            />
          </ElFormItem>
          <ElFormItem label="设置网站密码">
            <ElInput
              v-model="password"
              type="password"
              autocomplete="new-password"
              show-password
              :disabled="saving || uncertain"
              maxlength="128"
            />
          </ElFormItem>
          <ElFormItem label="确认密码">
            <ElInput
              v-model="confirmation"
              type="password"
              autocomplete="new-password"
              show-password
              :disabled="saving || uncertain"
              maxlength="128"
            />
          </ElFormItem>
          <ElAlert v-if="error" :title="error" type="error" :closable="false" />
          <ElButton
            native-type="submit"
            type="primary"
            :loading="saving"
            :disabled="!token || !username.trim() || !password || !confirmation || uncertain"
            class="activation-submit"
            >接受邀请并设置密码</ElButton
          >
        </template>
      </ElForm>
      <ElAlert v-if="!preview && error" :title="error" type="error" :closable="false" />
      <p v-if="preview?.state === 'pending'" class="activation-expiry"
        >邀请有效至 {{ customerDate(preview.expires_at) }}</p
      >
      <ElAlert
        v-if="uncertain"
        title="激活结果尚未确认，请先尝试登录原账号，不要重复设置。"
        type="warning"
        :closable="false"
      />
    </section>
    <footer>旺商聊 · 辣椒聊</footer>
  </main>
</template>
<script setup lang="ts">
  import { onBeforeUnmount, ref, watch } from 'vue'
  import { ElAlert, ElButton, ElForm, ElFormItem, ElIcon, ElInput, ElSkeleton } from 'element-plus'
  import { ArrowRight, Close, Message, User } from '@element-plus/icons-vue'
  import { api, ApiError, type Row, type User as WebsiteUser } from './api'
  import { customerDate } from './customer'
  const props = defineProps<{ token: string; user: WebsiteUser | null }>()
  defineEmits<{ close: []; login: []; logout: []; complete: [] }>()
  const username = ref(''),
    password = ref(''),
    confirmation = ref(''),
    error = ref('')
  const existing = ref(false),
    saving = ref(false),
    completed = ref(false),
    uncertain = ref(false)
  const preview = ref<Row>(),
    loading = ref(false)
  let generation = 0
  async function activate() {
    if (
      saving.value ||
      uncertain.value ||
      !props.token ||
      preview.value?.state !== 'pending' ||
      (preview.value.mode === 'new' && props.user) ||
      (preview.value.mode === 'existing' && !props.user)
    )
      return
    if (!props.user && (password.value.length < 12 || password.value !== confirmation.value)) {
      error.value = '请设置至少12位密码，并确保两次输入一致'
      return
    }
    const request = ++generation,
      actor = props.user?.id,
      token = props.token
    saving.value = true
    error.value = ''
    try {
      const result = await api<Row>(
        props.user ? '/customer-invitations/accept' : '/customer-invitations/activate',
        'POST',
        {
          token,
          ...(!props.user ? { username: username.value.trim(), password: password.value } : {})
        }
      )
      if (request !== generation || actor !== props.user?.id || token !== props.token) return
      password.value = ''
      confirmation.value = ''
      if (result?.requires_login) {
        existing.value = true
        return
      }
      completed.value = true
    } catch (e) {
      if (request !== generation || actor !== props.user?.id || token !== props.token) return
      if (
        e instanceof ApiError &&
        [
          'customer_invitation_login_required',
          'customer_invitation_website_login_required'
        ].includes(e.code)
      )
        existing.value = true
      else {
        error.value = (e as Error).message
        uncertain.value = !(e instanceof ApiError)
      }
    } finally {
      if (request === generation) saving.value = false
    }
  }
  watch(
    () => props.user?.id,
    () => {
      generation++
      saving.value = false
      loading.value = false
      error.value = ''
      password.value = ''
      confirmation.value = ''
      if (!preview.value && props.token) void loadPreview(props.token)
    }
  )
  async function loadPreview(token: string) {
    const request = ++generation
    preview.value = undefined
    if (!token) return
    loading.value = true
    error.value = ''
    try {
      const value = await api<Row>('/customer-invitations/preview', 'POST', { token })
      if (request !== generation || token !== props.token) return
      preview.value = value
      existing.value = value.mode === 'existing'
    } catch (e) {
      if (request === generation) error.value = (e as Error).message
    } finally {
      if (request === generation) loading.value = false
    }
  }
  watch(
    () => props.token,
    (token) => {
      completed.value = uncertain.value = false
      username.value = password.value = confirmation.value = ''
      void loadPreview(token)
    },
    { immediate: true }
  )
  onBeforeUnmount(() => {
    generation++
    password.value = ''
    confirmation.value = ''
  })
</script>
<style scoped>
  .customer-activation {
    min-height: 100dvh;
    padding: 0 32px;
    display: flex;
    flex-direction: column;
    background: var(--fw-canvas, #f5f6f7);
  }
  .activation-brand {
    min-height: 88px;
    display: flex;
    align-items: center;
    gap: 12px;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .activation-brand img {
    width: 30px;
    height: 30px;
  }
  .activation-brand > .el-button {
    margin-left: auto;
  }
  .activation-content {
    width: 100%;
    max-width: 400px;
    margin: auto;
    padding: 48px 0;
  }
  .activation-icon {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 8px;
    background: var(--el-fill-color-light);
    color: var(--el-color-primary);
    margin-bottom: 24px;
  }
  h1 {
    font-size: 24px;
    font-weight: 600;
    margin: 0 0 12px;
    overflow-wrap: anywhere;
  }
  .activation-description {
    color: var(--el-text-color-secondary);
    margin: 0 0 28px;
  }
  .activation-expiry {
    color: var(--el-text-color-secondary);
    font-size: 12px;
    margin-top: 20px;
  }
  .activation-mode {
    display: flex;
    margin-bottom: 28px;
  }
  .activation-submit {
    width: 100%;
    margin: 20px 0 0;
  }
  .activation-account {
    padding: 20px 0;
    border-block: 1px solid var(--el-border-color-light);
  }
  .activation-account dt {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .activation-account dd {
    margin: 8px 0 0;
    overflow-wrap: anywhere;
  }
  .el-alert {
    margin-top: 16px;
  }
  footer {
    padding: 24px 0;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    border-top: 1px solid var(--el-border-color-light);
  }
  @media (max-width: 640px) {
    .customer-activation {
      padding-inline: 20px;
    }
    .activation-brand {
      min-height: 72px;
    }
    .activation-content {
      padding-block: 32px;
    }
    h1 {
      font-size: 22px;
    }
  }
</style>
