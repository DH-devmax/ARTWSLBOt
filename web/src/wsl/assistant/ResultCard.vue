<template>
  <section class="assistant-result" v-if="valid">
    <header
      ><strong>{{ toolLabels[String(card.type)] }}</strong
      ><time>{{ sampled }}</time></header
    >
    <ElTag v-if="card.truncated" type="warning">结果已截断，不可据此生成计划</ElTag>
    <ElButton
      v-if="card.type === 'propose_group_config_change' && planID"
      :icon="DocumentChecked"
      @click="$emit('plan', planID)"
      >查看配置差异</ElButton
    >
    <ElButton
      v-else-if="card.type === 'propose_group_action' && planID"
      :icon="DocumentChecked"
      @click="$emit('action', planID)"
      >查看动作与影响</ElButton
    >
    <ul
      v-else-if="card.type === 'find_group_members' || card.type === 'find_group_messages'"
      class="member-candidates"
    >
      <li v-for="member in rows" :key="String(member.id)">
        <span
          ><strong>{{ member.name }}</strong
          ><small>{{ member.id }} · {{ member.role }}</small></span
        >
        <ElButton
          :disabled="disabled || typeof member.token !== 'string'"
          @click="$emit('target', String(member.token))"
          >{{ card.type === 'find_group_messages' ? '选择消息' : '选择成员' }}</ElButton
        >
      </li>
    </ul>
    <ul v-else-if="card.type === 'get_group_capabilities'" class="capability-list">
      <li v-for="capability in capabilities" :key="String(capability.id)">
        <span>{{ capability.name }}</span>
        <ElTag :type="capability.production_admitted ? 'success' : 'info'" size="small">{{
          capability.production_admitted ? '可本人确认' : '尚未开放'
        }}</ElTag>
      </li>
    </ul>
    <GroupPicker
      v-else-if="card.type === 'list_authorized_groups' && selectableGroups.length"
      :groups="selectableGroups"
      :accounts="accounts || []"
      :disabled="disabled"
      @select="$emit('group', $event)"
    />
    <ElTable v-else-if="rows.length" :data="rows" size="small" max-height="260">
      <ElTableColumn
        v-for="column in columns"
        :key="column.key"
        :prop="column.key"
        :label="column.label"
        min-width="110"
      />
    </ElTable>
    <template v-else-if="card.type === 'get_group_config' && object(card.data)">
      <div class="config-version">版本 {{ card.data.version }}</div>
      <div v-for="field in configFields" :key="field.label">
        <strong>{{ field.label }}</strong
        ><ConfigValue :value="field.value" />
      </div>
    </template>
    <ElEmpty v-else description="无可交付数据" :image-size="40" />
    <small v-if="card.type === 'get_group_config'">配置快照 · 实际生效尚未验证</small>
    <small v-else-if="card.type === 'propose_group_action'">{{
      object(card.data) ? card.data.summary : '动作建议，尚未执行'
    }}</small>
    <small v-else-if="card.type === 'propose_group_config_change'">{{
      object(card.data) ? card.data.summary : '配置建议，尚未保存'
    }}</small>
    <small v-else>当前授权范围内的查询快照</small>
  </section>
</template>
<script setup lang="ts">
  import { computed } from 'vue'
  import { DocumentChecked } from '@element-plus/icons-vue'
  import { cardSchema, object, toolLabels } from './client'
  import type { Row } from '../api'
  import GroupPicker from './GroupPicker.vue'
  import ConfigValue from './ConfigValue.vue'
  const props = defineProps<{
    card: Record<string, unknown>
    availableGroups?: Row[]
    accounts?: Row[]
    disabled?: boolean
  }>()
  defineEmits<{
    plan: [id: string]
    group: [id: string]
    action: [id: string]
    target: [token: string]
  }>()
  const valid = computed(() => cardSchema(props.card))
  const sampled = computed(() => new Date(String(props.card.sampled_at)).toLocaleTimeString())
  const planID = computed(() =>
    object(props.card.data) && typeof props.card.data.id === 'string' ? props.card.data.id : ''
  )
  const rows = computed(() =>
    Array.isArray(props.card.data) ? props.card.data.filter(object).slice(0, 100) : []
  )
  const capabilities = computed(() =>
    object(props.card.data) && Array.isArray(props.card.data.actions)
      ? props.card.data.actions.filter(object)
      : []
  )
  const selectableGroups = computed(() =>
    (props.availableGroups || []).filter(
      (group) =>
        group.active &&
        rows.value.some(
          (row) => row.id === group.group_id && row.account_id === group.primary_account_id
        )
    )
  )
  const columns = computed(() => {
    switch (props.card.type) {
      case 'list_authorized_bots':
      case 'get_bot_status':
        return [
          { key: 'name', label: '机器人' },
          { key: 'runtime_state', label: '采样状态' },
          { key: 'authorized_groups', label: '授权群数' }
        ]
      case 'list_authorized_groups':
        return [
          { key: 'name', label: '群组' },
          { key: 'version', label: '配置版本' }
        ]
      case 'list_group_operations':
        return [
          { key: 'kind', label: '类型' },
          { key: 'status', label: '状态' },
          { key: 'created_at', label: '时间' }
        ]
      default:
        return []
    }
  })
  const configFields = computed(() => {
    if (!object(props.card.data) || !object(props.card.data.values)) return []
    const labels: Record<string, string> = {
      blocked_words: '拦截词',
      blacklist: '黑名单',
      exemptions: '豁免名单',
      keyword_replies: '关键词回复'
    }
    return Object.entries(props.card.data.values)
      .filter(([key]) => labels[key])
      .flatMap(([key, value]) =>
        object(value)
          ? Object.entries(value).map(([field, content]) => ({
              label: `${labels[key]} · ${{ words: '词语', members: '成员', rules: '回复规则', enabled: '功能开关', admin_only: '仅管理员触发' }[field] || field}`,
              value: content
            }))
          : []
      )
  })
</script>
<style scoped>
  .assistant-result {
    border: 1px solid var(--el-border-color);
    border-radius: 6px;
    padding: 12px;
    margin: 10px 0;
    overflow: hidden;
  }
  header {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
    font-size: 13px;
  }
  time,
  small {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .member-candidates,
  .capability-list {
    list-style: none;
    margin: 12px 0;
    padding: 0;
  }
  .member-candidates li,
  .capability-list li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding-block: 10px;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .member-candidates li > span {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 240px;
    overflow: auto;
    font: 12px/1.6 monospace;
    margin: 10px 0;
  }
  small {
    display: block;
    margin-top: 8px;
  }
</style>
