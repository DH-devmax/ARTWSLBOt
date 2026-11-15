<template>
  <section class="feature-rule-preview" aria-label="规则试算">
    <h3>规则试算</h3>
    <p class="preview-notice">仅试算，未执行；实际提交还要检查权限/角色/准入/冷却</p>
    <ElForm label-position="top" @submit.prevent>
      <ElFormItem label="试算文本">
        <ElInput
          v-model="text"
          aria-label="试算文本"
          type="textarea"
          :rows="4"
          :maxlength="16384"
        />
        <span class="preview-bytes" :class="{ 'is-invalid': bytes > 16384 }"
          >{{ bytes }} / 16384 bytes</span
        >
      </ElFormItem>
      <div class="preview-sender">
        <ElFormItem label="模拟角色">
          <ElSelect v-model="role" aria-label="模拟角色">
            <ElOption label="普通成员" value="member" />
            <ElOption label="管理员" value="admin" />
            <ElOption label="群主" value="owner" />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="模拟成员 ID（可选）">
          <ElInput v-model="sender" aria-label="模拟成员 ID（可选）" :maxlength="200" />
        </ElFormItem>
      </div>
      <ElAlert
        v-if="reason || inputError"
        :title="reason || inputError"
        type="warning"
        :closable="false"
      />
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <ElButton
        class="preview-submit"
        :icon="DataAnalysis"
        :loading="loading"
        :disabled="disabled || !!reason || !!inputError || !bytes"
        @click="emit('preview', { text, sender, role })"
        >试算规则</ElButton
      >
    </ElForm>
    <div v-if="result" class="preview-result" role="status" aria-label="规则试算结果">
      <ElTag type="info">仅试算 · 未执行</ElTag>
      <dl>
        <dt>命中模块</dt>
        <dd>{{
          result.decision.rule ? names[result.decision.rule] || result.decision.rule : '未命中'
        }}</dd>
        <dt>预期动作</dt>
        <dd>{{ actionName(result.decision.action) }}</dd>
        <template v-if="result.decision.target">
          <dt>模拟目标</dt><dd>{{ result.decision.target }}</dd>
        </template>
        <template v-if="result.decision.minutes">
          <dt>禁言时长</dt><dd>{{ result.decision.minutes }} 分钟</dd>
        </template>
        <dt>回复冷却</dt><dd>{{ result.cooldown_seconds }} 秒</dd> <dt>热群回复</dt
        ><dd>{{ result.decision.suppress_heat ? '抑制' : '未抑制' }}</dd> <dt>试算时间</dt
        ><dd>{{ result.sampled_at }}</dd>
      </dl>
      <template v-if="result.decision.text">
        <h4>回复正文</h4>
        <pre>{{ result.decision.text }}</pre>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, ref, watch } from 'vue'
  import { DataAnalysis } from '@element-plus/icons-vue'
  import {
    ElAlert,
    ElButton,
    ElForm,
    ElFormItem,
    ElInput,
    ElOption,
    ElSelect,
    ElTag
  } from 'element-plus'
  import type { Row } from './api'

  defineProps<{
    result: Row | null
    names: Record<string, string>
    loading: boolean
    disabled: boolean
    reason: string
    error: string
  }>()
  const emit = defineEmits<{
    preview: [input: { text: string; sender: string; role: string }]
    invalidate: []
  }>()
  const text = ref(''),
    sender = ref(''),
    role = ref('member')
  const size = (value: string) => new TextEncoder().encode(value).length
  const bytes = computed(() => size(text.value))
  const inputError = computed(() =>
    bytes.value > 16384
      ? '试算文本不能超过 16384 bytes'
      : size(sender.value) > 200
        ? '模拟成员 ID 不能超过 200 bytes'
        : ''
  )
  const actionName = (action: string) =>
    (
      ({
        recall_message: '撤回当前消息',
        mute_member: '禁言成员',
        send_text: '发送群文本'
      }) as Record<string, string>
    )[action] ||
    action ||
    '无动作'
  watch([text, sender, role], () => emit('invalidate'), { flush: 'sync' })
</script>

<style scoped>
  .feature-rule-preview {
    min-width: 0;
    margin-top: 24px;
    padding-top: 20px;
    border-top: 1px solid var(--ui-line, var(--el-border-color));
    --el-component-size: 36px;
    --el-border-radius-base: 8px;
  }
  h3,
  h4 {
    font-size: 14px;
    line-height: 1.5;
    margin: 0 0 10px;
  }
  .preview-notice,
  .preview-bytes {
    color: var(--ui-muted, var(--el-text-color-secondary));
    font-size: 12px;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .preview-notice {
    margin: 0 0 16px;
  }
  .preview-bytes {
    margin-top: 4px;
  }
  .is-invalid {
    color: var(--el-color-danger);
  }
  .preview-sender {
    display: grid;
    grid-template-columns: 150px minmax(0, 1fr);
    gap: 12px;
  }
  .preview-sender > * {
    min-width: 0;
  }
  .preview-submit {
    margin-top: 12px;
  }
  .feature-rule-preview :deep(.el-input__wrapper),
  .feature-rule-preview :deep(.el-select__wrapper) {
    min-height: 36px;
    border-radius: 8px;
  }
  .feature-rule-preview :deep(.el-textarea__inner) {
    border-radius: 8px;
  }
  .preview-result {
    min-width: 0;
    margin-top: 16px;
  }
  dl {
    display: grid;
    grid-template-columns: 76px minmax(0, 1fr);
    gap: 8px 12px;
    margin: 14px 0;
    font-size: 13px;
    line-height: 1.6;
  }
  dt {
    color: var(--ui-muted, var(--el-text-color-secondary));
  }
  dd {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }
  pre {
    margin: 0;
    font: inherit;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  @media (max-width: 420px) {
    .preview-sender {
      grid-template-columns: minmax(0, 1fr);
      gap: 0;
    }
  }
</style>
