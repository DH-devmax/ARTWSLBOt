<template>
  <div class="business-rule-editor" :data-rule-type="type">
    <ol v-if="rules.length" class="business-rules">
      <li v-for="(rule, index) in rules" :key="ruleKey(rule)" class="business-rule">
        <div class="rule-toolbar">
          <ElCheckbox
            :model-value="!rule.disabled"
            :disabled="disabled"
            :aria-label="`${noun} ${index + 1} 启用`"
            @update:model-value="update(index, { disabled: !$event })"
            >{{ noun }} {{ index + 1 }}</ElCheckbox
          >
          <div class="rule-actions">
            <template v-if="type === 'keyword_rules'">
              <ElTooltip content="上移规则">
                <ElButton
                  :icon="ArrowUp"
                  :aria-label="`上移规则 ${index + 1}`"
                  :disabled="disabled || index === 0"
                  @click="move(index, -1)"
                />
              </ElTooltip>
              <ElTooltip content="下移规则">
                <ElButton
                  :icon="ArrowDown"
                  :aria-label="`下移规则 ${index + 1}`"
                  :disabled="disabled || index === rules.length - 1"
                  @click="move(index, 1)"
                />
              </ElTooltip>
            </template>
            <ElTooltip :content="`删除${noun}`">
              <ElButton
                :icon="Delete"
                :aria-label="`删除${noun} ${index + 1}`"
                :disabled="disabled"
                @click="remove(index)"
              />
            </ElTooltip>
          </div>
        </div>
        <template v-if="type === 'keyword_rules'">
          <div class="keyword-condition">
            <ElSelect
              :model-value="rule.match || 'contains'"
              :aria-label="`匹配模式 ${index + 1}`"
              :disabled="disabled"
              @update:model-value="update(index, { match: $event })"
            >
              <ElOption label="包含" value="contains" />
              <ElOption label="完全匹配" value="exact" />
            </ElSelect>
            <ElInput
              :model-value="rule.keyword"
              :aria-label="`关键词 ${index + 1}`"
              placeholder="关键词"
              :maxlength="200"
              :disabled="disabled"
              @update:model-value="update(index, { keyword: $event })"
            />
          </div>
          <ElInput
            :model-value="rule.reply"
            :aria-label="`回复内容 ${index + 1}`"
            placeholder="回复内容"
            type="textarea"
            :rows="3"
            :maxlength="4000"
            :disabled="disabled"
            @update:model-value="update(index, { reply: $event })"
          />
        </template>
        <template v-else>
          <ElInput
            :model-value="rule.name"
            :aria-label="`组合规则名称 ${index + 1}`"
            placeholder="组合规则名称"
            :maxlength="120"
            :disabled="disabled"
            @update:model-value="update(index, { name: $event })"
          />
          <ElInput
            :model-value="(rule.words || []).join('\n')"
            :aria-label="`组合词（一行一个） ${index + 1}`"
            placeholder="组合词（一行一个，2 至 5 词）"
            type="textarea"
            :rows="3"
            :disabled="disabled"
            @update:model-value="update(index, { words: $event.split(/\r?\n/) })"
          />
        </template>
      </li>
    </ol>
    <p v-else class="rules-empty">暂无{{ noun }}</p>
    <div class="rules-footer">
      <ElButton :icon="Plus" :disabled="disabled || rules.length >= maximum" @click="add"
        >添加{{ noun }}</ElButton
      >
      <span>{{ rules.length }} / {{ maximum }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, toRaw } from 'vue'
  import { ArrowDown, ArrowUp, Delete, Plus } from '@element-plus/icons-vue'
  import { ElButton, ElCheckbox, ElInput, ElOption, ElSelect, ElTooltip } from 'element-plus'

  interface Rule {
    keyword?: string
    reply?: string
    match?: string
    disabled?: boolean
    name?: string
    words?: string[]
  }
  const props = defineProps<{
    modelValue: Rule[]
    type: 'keyword_rules' | 'blocked_rules'
    disabled: boolean
    limit?: number
  }>()
  const emit = defineEmits<{ 'update:modelValue': [value: Rule[]] }>()
  const rules = computed(() => (Array.isArray(props.modelValue) ? props.modelValue : []))
  const noun = computed(() => (props.type === 'keyword_rules' ? '规则' : '组合规则'))
  const maximum = computed(() =>
    Math.min(props.limit || Infinity, props.type === 'keyword_rules' ? 100 : 20)
  )
  const keys = new WeakMap<Rule, number>()
  let sequence = 0
  function ruleKey(rule: Rule) {
    const raw = toRaw(rule)
    if (!keys.has(raw)) keys.set(raw, ++sequence)
    return keys.get(raw)!
  }
  function update(index: number, patch: Partial<Rule>) {
    if (props.disabled) return
    const next = [...rules.value]
    next[index] = { ...next[index], ...patch }
    keys.set(toRaw(next[index]), ruleKey(rules.value[index]))
    emit('update:modelValue', next)
  }
  function add() {
    if (props.disabled || rules.value.length >= maximum.value) return
    emit('update:modelValue', [
      ...rules.value,
      props.type === 'keyword_rules'
        ? { keyword: '', reply: '', match: 'contains', disabled: false }
        : { name: '', words: ['', ''], disabled: false }
    ])
  }
  function remove(index: number) {
    if (!props.disabled)
      emit(
        'update:modelValue',
        rules.value.filter((_, position) => position !== index)
      )
  }
  function move(index: number, offset: number) {
    const destination = index + offset
    if (props.disabled || destination < 0 || destination >= rules.value.length) return
    const next = [...rules.value]
    ;[next[index], next[destination]] = [next[destination], next[index]]
    emit('update:modelValue', next)
  }
</script>

<style scoped>
  .business-rule-editor {
    width: 100%;
    min-width: 0;
    --el-component-size: 36px;
    --el-border-radius-base: 8px;
  }
  .business-rules {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .business-rule {
    display: grid;
    gap: 10px;
    min-width: 0;
    padding: 14px 0;
    border-top: 1px solid var(--ui-line, var(--el-border-color));
  }
  .rule-toolbar,
  .rule-actions,
  .rules-footer {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .rule-toolbar,
  .rules-footer {
    flex-wrap: wrap;
    justify-content: space-between;
  }
  .rule-toolbar :deep(.el-checkbox) {
    margin-right: 0;
  }
  .rule-actions .el-button {
    width: 36px;
    height: 36px;
    padding: 0;
    margin: 0;
    border-radius: 8px;
  }
  .keyword-condition {
    display: grid;
    grid-template-columns: 124px minmax(0, 1fr);
    gap: 10px;
    min-width: 0;
  }
  .business-rule-editor :deep(.el-input__wrapper),
  .business-rule-editor :deep(.el-select__wrapper) {
    min-height: 36px;
    border-radius: 8px;
  }
  .business-rule-editor :deep(.el-textarea__inner) {
    border-radius: 8px;
    overflow-wrap: anywhere;
  }
  .rules-footer {
    margin-top: 12px;
  }
  .rules-footer span,
  .rules-empty {
    color: var(--ui-muted, var(--el-text-color-secondary));
    font-size: 12px;
  }
  @media (max-width: 420px) {
    .keyword-condition {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
