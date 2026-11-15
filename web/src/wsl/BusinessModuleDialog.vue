<template>
  <ElDialog
    :model-value="open"
    :title="title"
    class="platform-config-dialog business-module-dialog"
    width="1040px"
    align-center
    append-to-body
    :close-on-click-modal="false"
    @update:model-value="!$event && emit('close')"
  >
    <div class="business-dialog-toolbar">
      <ElSelect
        :model-value="group"
        aria-label="业务记录群组"
        placeholder="全部可见群组"
        filterable
        clearable
        @update:model-value="emit('update:group', $event || '')"
      >
        <ElOption
          v-for="item in groups"
          :key="item.group_id"
          :label="item.name"
          :value="item.group_id"
        />
      </ElSelect>
      <ElInput
        v-model="search"
        aria-label="搜索业务记录"
        placeholder="搜索"
        clearable
        :prefix-icon="Search"
      />
      <ElButton v-if="createLabel" type="primary" :icon="Plus" @click="emit('create')">{{
        createLabel
      }}</ElButton>
    </div>
    <ElTable :data="visibleRows" v-loading="loading" row-key="id" empty-text="暂无记录">
      <ElTableColumn label="群组" min-width="150" show-overflow-tooltip>
        <template #default="{ row }">{{
          groups.find((item) => item.group_id === row.group_id)?.name || row.group_id
        }}</template>
      </ElTableColumn>
      <template v-if="kind === 'ledger'">
        <ElTableColumn prop="member_id" label="成员" min-width="150" />
        <ElTableColumn prop="kind" label="类型" width="120" />
        <ElTableColumn label="金额" width="130" align="right">
          <template #default="{ row }">{{ money(row.amount_cents) }}</template>
        </ElTableColumn>
        <ElTableColumn
          prop="external_key"
          label="业务流水号"
          min-width="230"
          show-overflow-tooltip
        />
        <ElTableColumn label="时间" min-width="160">
          <template #default="{ row }">{{ date(row.created_at) }}</template>
        </ElTableColumn>
      </template>
      <template v-else>
        <ElTableColumn prop="name" label="活动" min-width="180" />
        <ElTableColumn prop="participant_count" label="报名人数" width="100" />
        <ElTableColumn prop="winners" label="中奖名额" width="100" />
        <ElTableColumn label="状态" width="100">
          <template #default="{ row }">{{ statusLabel(row.state) }}</template>
        </ElTableColumn>
        <ElTableColumn label="截止时间" min-width="160">
          <template #default="{ row }">{{ date(row.closes_at) }}</template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="90">
          <template #default="{ row }">
            <ElButton
              v-if="row.state === 'open' && canDraw"
              link
              type="primary"
              @click="emit('draw', row)"
              >开奖</ElButton
            >
          </template>
        </ElTableColumn>
      </template>
    </ElTable>
    <template #footer><ElButton @click="emit('close')">返回业务模块</ElButton></template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, ref, watch } from 'vue'
  import { Plus, Search } from '@element-plus/icons-vue'
  import type { Row } from './api'

  const props = defineProps<{
    open: boolean
    kind: string
    title: string
    scope: string
    group: string
    groups: Row[]
    rows: Row[]
    createLabel: string
    canDraw: boolean
    loading: boolean
    date: (value: string) => string
    money: (value: string) => string
    statusLabel: (value: string) => string
  }>()
  const emit = defineEmits<{
    close: []
    create: []
    draw: [row: Row]
    'update:group': [group: string]
  }>()
  const search = ref('')
  const visibleRows = computed(() =>
    props.rows.filter(
      (row) =>
        !search.value || JSON.stringify(row).toLowerCase().includes(search.value.toLowerCase())
    )
  )
  watch(
    () => `${props.scope}:${props.kind}:${props.open}`,
    () => {
      search.value = ''
    }
  )
</script>

<style scoped>
  .business-dialog-toolbar {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 20px;
  }
  .business-dialog-toolbar .el-select {
    width: 240px;
    max-width: 100%;
  }
  .business-dialog-toolbar .el-input {
    width: 220px;
    max-width: 100%;
  }
  @media (max-width: 600px) {
    .business-dialog-toolbar .el-select,
    .business-dialog-toolbar .el-input {
      width: 100%;
    }
  }
</style>
