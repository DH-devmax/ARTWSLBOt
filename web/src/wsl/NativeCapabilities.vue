<template>
  <section v-if="visible" class="native-catalog" data-testid="native-capabilities">
    <div v-if="audience === 'foundation'" class="native-overview">
      <div class="native-progress">
        <h2>原生底座 · 独立验收项进度</h2>
        <ArtRingChart
          :data="integrationCounts"
          :center-text="String(checklist.length)"
          height="130px"
        />
        <div class="native-chart-legend">
          <span v-for="item in integrationCounts" :key="item.name">
            <i :style="{ backgroundColor: item.itemStyle.color }"></i>
            {{ item.name }} <b>{{ item.value }}</b>
          </span>
        </div>
      </div>
      <div class="native-summary">
        <span
          >原生能力组 <strong>{{ scopedItems.length }}</strong></span
        >
        <span
          >已核对支持
          <strong>{{ scopedItems.filter((item) => item.support === 'confirmed').length }}</strong></span
        >
        <span
          >本平台待核实
          <strong>{{ scopedItems.filter((item) => item.support === 'review').length }}</strong></span
        >
        <span
          >独立验收项
          <strong data-testid="native-checklist-count">{{ checklist.length }}</strong></span
        >
        <span data-testid="native-controlled-count"
          >受控能力组已核验 <strong>{{ controlledActions }}</strong></span
        >
        <span data-testid="native-usable-count"
          >指定场景可用 <strong>{{ controlledChecks }}</strong></span
        >
        <span data-testid="native-production-count"
          >完整验收通过 <strong>{{ acceptedActions }}</strong></span
        >
        <p>受控记录仅覆盖指定场景。完整验收按独立子项统计；业务模块的自动执行另行验收。</p>
      </div>
    </div>
    <div class="native-toolbar">
      <ElInput
        v-model="search"
        aria-label="搜索原生功能"
        placeholder="搜索原生功能"
        clearable
        :prefix-icon="Search"
      />
      <ElSelect v-model="category" aria-label="原生功能分类" placeholder="全部分类">
        <ElOption label="全部分类" value="" />
        <ElOption v-for="name in categories" :key="name" :label="name" :value="name" />
      </ElSelect>
      <ElSelect v-model="status" aria-label="原生接入状态" placeholder="全部接入状态">
        <ElOption label="全部接入状态" value="" />
        <ElOption label="已实现 · 待完整验收" value="implemented" />
        <ElOption label="部分接入" value="partial" />
        <ElOption label="待接入" value="pending" />
        <ElOption label="已验收" value="accepted" />
        <ElOption label="受控场景已核验" value="controlled" />
      </ElSelect>
    </div>
    <div class="native-table-wrap">
      <ElTable :data="filtered" row-key="id" empty-text="无匹配原生功能">
        <ElTableColumn label="原生功能" min-width="225">
          <template #default="{ row }">
            <strong>{{ row.name }}</strong>
            <div class="native-muted"
              >{{ audience === 'manual' ? '人工操作' : '原生底座' }} · {{ row.category }} · {{ kindLabel(row.kind) }}</div
            >
            <div v-if="parentOf(row.id)" class="native-muted">{{ parentOf(row.id)?.name }}</div>
          </template>
        </ElTableColumn>
        <ElTableColumn label="平台支持" width="130">
          <template #default="{ row }">
            <span :class="['support-state', row.support]">
              <span class="support-dot" />
              {{ row.support === 'confirmed' ? '已核对支持' : '待核验' }}
            </span>
          </template>
        </ElTableColumn>
        <ElTableColumn label="本项目接入" min-width="185">
          <template #default="{ row }">
            <ElTag :type="row.integration === 'accepted' || acceptanceOf(row) ? 'success' : 'info'">
              {{ availabilityLabel(row) }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="验收记录" min-width="190">
          <template #default="{ row }">
            <ElTag v-if="acceptanceOf(row)" type="success">指定场景已核验</ElTag>
            <template v-else-if="demonstrations?.[row.id]">
              <ElTag type="success">移动端样本已核验</ElTag>
              <div class="native-muted">本站执行待验收</div>
            </template>
            <span v-else class="native-muted">尚无通过记录</span>
          </template>
        </ElTableColumn>
        <ElTableColumn v-if="audience === 'foundation'" label="关联业务模块" min-width="240">
          <template #default="{ row }">
            <span>{{ usersOf(row.id).join('、') || '无' }}</span>
          </template>
        </ElTableColumn>
        <ElTableColumn label="详情" width="80">
          <template #default="{ row }">
            <ElButton
              link
              type="primary"
              :aria-label="`查看${row.name}`"
              @click="selected = row.id"
            >
              查看
            </ElButton>
          </template>
        </ElTableColumn>
      </ElTable>
    </div>
  </section>
  <ElDialog
    :model-value="!!detail"
    :title="detail?.name || '原生功能详情'"
    class="platform-config-dialog"
    align-center
    width="620px"
    @close="selected = ''"
  >
    <template v-if="detail">
      <p class="native-description">{{ detail.summary }}</p>
      <ElDescriptions :column="1" border>
        <ElDescriptionsItem label="功能归属"
          >{{ usage?.[detail.id] === 'manual' ? '人工操作' : '原生底座' }} · {{ kindLabel(detail.kind) }}</ElDescriptionsItem
        >
        <ElDescriptionsItem v-if="parentOf(detail.id)" label="所属能力组">
          <ElButton link type="primary" @click="selected = parentOf(detail.id)!.id">
            {{ parentOf(detail.id)?.name }}
          </ElButton>
        </ElDescriptionsItem>
        <ElDescriptionsItem label="原生权限">{{ detail.role }}</ElDescriptionsItem>
        <ElDescriptionsItem label="支持依据">{{ detail.evidence }}</ElDescriptionsItem>
        <ElDescriptionsItem label="本项目接入">
          {{ integrationLabel(detail.integration) }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="依赖模块">
          {{ usersOf(detail.id).join('、') || '无' }}
        </ElDescriptionsItem>
      </ElDescriptions>
      <section v-if="children.length" class="native-components" aria-label="独立验收子项">
        <h3>独立验收子项</h3>
        <div v-for="child in children" :key="child.id" class="native-component">
          <ElButton
            link
            type="primary"
            :aria-label="`查看${child.name}`"
            @click="selected = child.id"
          >
            {{ child.name }}
          </ElButton>
          <ElTag :type="acceptanceOf(child) ? 'success' : 'info'">{{
            availabilityLabel(child)
          }}</ElTag>
          <span class="native-muted">{{
            acceptanceOf(child) ? '指定场景已核验' : demonstrations?.[child.id] ? '移动端样本已核验 · 本站执行待验收' : '尚无通过记录'
          }}</span>
        </div>
      </section>
      <ElDescriptions v-if="controlledDetail" :column="1" border class="native-acceptance">
        <ElDescriptionsItem label="受控结果">{{ controlledDetail.result }}</ElDescriptionsItem>
        <ElDescriptionsItem label="核验时间">{{
          acceptanceTime(controlledDetail.verified_at)
        }}</ElDescriptionsItem>
        <ElDescriptionsItem label="测试范围">{{ controlledDetail.scope }}</ElDescriptionsItem>
        <ElDescriptionsItem label="尚需验收">{{ controlledDetail.remaining }}</ElDescriptionsItem>
        <ElDescriptionsItem label="报告依据">{{ controlledDetail.evidence }}</ElDescriptionsItem>
      </ElDescriptions>
      <ElDescriptions v-if="detail && demonstrations?.[detail.id]" :column="1" border class="native-acceptance">
        <ElDescriptionsItem label="移动端样本">{{ demonstrations[detail.id].result }}</ElDescriptionsItem>
        <ElDescriptionsItem label="样本时间">{{ acceptanceTime(demonstrations[detail.id].verified_at) }}</ElDescriptionsItem>
        <ElDescriptionsItem label="样本范围">{{ demonstrations[detail.id].scope }}</ElDescriptionsItem>
        <ElDescriptionsItem label="本站待验收">{{ demonstrations[detail.id].remaining }}</ElDescriptionsItem>
        <ElDescriptionsItem label="报告依据">{{ demonstrations[detail.id].evidence }}</ElDescriptionsItem>
      </ElDescriptions>
      <NativeSocial
        v-if="
          account &&
          session.user?.role === 'superadmin' &&
          ['wsl', 'lajiaoliao'].includes(session.workspace) &&
          detail.support === 'confirmed' &&
          ['partial', 'implemented', 'accepted'].includes(detail.integration) &&
          ['approve_friend', 'accept_friend', 'reject_friend'].includes(detail.id)
        "
        :account="account"
        :groups="groups"
        mode="applications"
        :scope="scope"
        :key="`${scope}:${detail.id}`"
      />
      <p v-else-if="['approve_friend', 'accept_friend', 'reject_friend'].includes(detail.id)" class="native-muted"
        >请先在顶部选择本平台账号；此操作仅向超管开放。</p
      >
      <ElButton
        v-if="
          session.user?.role === 'superadmin' &&
          ['wsl', 'lajiaoliao'].includes(session.workspace) &&
          detail.support === 'confirmed' &&
          ['partial', 'implemented', 'accepted'].includes(detail.integration) &&
          [
            'mute_member',
            'unmute_member',
            'mute_group',
            'kick_member',
            'publish_notice',
            'read_invitations',
            'invite_group',
            'leave_group',
            'grant_admin',
            'revoke_admin'
          ].includes(detail.id)
        "
        type="primary"
        class="native-tool-link"
        @click="openTools"
        >进入受控验证 Tools</ElButton
      >
    </template>
    <template #footer><ElButton @click="selected = ''">关闭详情</ElButton></template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, ref, watch } from 'vue'
  import { Search } from '@element-plus/icons-vue'
  import { type Row } from './api'
  import {
    integrationLabel,
    kindLabel,
    type NativeCapability,
    type ControlledAcceptance,
    type NativeDetail
  } from './featureCatalog'
  import { useSession } from './store'
  import NativeSocial from './NativeSocial.vue'
  import ArtRingChart from '../components/core/charts/art-ring-chart/index.vue'

  const props = defineProps<{
    items: NativeCapability[]
    details?: NativeDetail[]
    acceptance?: Record<string, ControlledAcceptance>
    demonstrations?: Record<string, ControlledAcceptance>
    modules: Row[]
    visible: boolean
    scope: string
    account: string
    groups: Row[]
    usage?: Record<string, string>
    audience: 'foundation' | 'manual'
  }>()
  const selected = defineModel<string>('selected', { default: '' })
  const emit = defineEmits(['tools'])
  function openTools() {
    selected.value = ''
    emit('tools')
  }
  const session = useSession()
  const search = ref(''),
    category = ref(''),
    status = ref('')
  const scopedItems = computed(() =>
    props.items.filter((item) => props.usage?.[item.id] === props.audience)
  )
  const categories = computed(() => [...new Set(scopedItems.value.map((item) => item.category))])
  const details = computed(() => props.details || [])
  const checklist = computed(() => {
    const parents = new Set(details.value.map((item) => item.parent_id))
    return scopedItems.value
      .flatMap((item) =>
        parents.has(item.id) ? details.value.filter((child) => child.parent_id === item.id) : [item]
      )
      .filter((item) => item.integration !== 'deferred' && props.usage?.[item.id] === props.audience)
  })
  function parentOf(id: string) {
    const child = details.value.find((item) => item.id === id)
    return child && props.items.find((item) => item.id === child.parent_id)
  }
  function acceptanceOf(item: NativeCapability) {
    const child = details.value.find((entry) => entry.id === item.id)
    return child ? child.acceptance : props.acceptance?.[item.id]
  }
  const controlledActions = computed(
    () => scopedItems.value.filter((item) => props.acceptance?.[item.id]).length
  )
  const controlledChecks = computed(
    () =>
      checklist.value.filter((item) => item.integration !== 'accepted' && acceptanceOf(item)).length
  )
  function availabilityLabel(item: NativeCapability) {
    return item.integration !== 'accepted' && acceptanceOf(item)
      ? '指定场景可用'
      : integrationLabel(item.integration)
  }
  const acceptedActions = computed(
    () => checklist.value.filter((item) => item.integration === 'accepted').length
  )
  const integrationCounts = computed(() =>
    ['accepted', 'controlled', 'implemented', 'partial', 'pending']
      .map((value, index) => ({
        name: value === 'controlled' ? '指定场景可用' : integrationLabel(value),
        value: checklist.value.filter((item) =>
          value === 'controlled'
            ? item.integration !== 'accepted' && !!acceptanceOf(item)
            : item.integration === value && (value === 'accepted' || !acceptanceOf(item))
        ).length,
        itemStyle: { color: ['#256340', '#4c9475', '#6c8aa6', '#bea668', '#c9d0cc'][index] }
      }))
      .filter((item) => item.value > 0)
  )
  const filtered = computed(() =>
    checklist.value.filter(
      (item) =>
        (!category.value || item.category === category.value) &&
        (!status.value ||
          (status.value === 'controlled'
            ? !!acceptanceOf(item)
            : item.integration === status.value)) &&
        `${item.name} ${parentOf(item.id)?.name || ''} ${item.summary} ${usersOf(item.id).join(' ')}`.includes(
          search.value.trim()
        )
    )
  )
  const detail = computed(() =>
    [...props.items, ...details.value].find((item) => item.id === selected.value)
  )
  const children = computed(() =>
    details.value.filter((item) => item.parent_id === selected.value && props.usage?.[item.id] !== 'excluded')
  )
  const controlledDetail = computed(() => detail.value && acceptanceOf(detail.value))
  function acceptanceTime(value: string) {
    return (
      new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) +
      ' · Asia/Shanghai'
    )
  }
  function usersOf(id: string): string[] {
    if (props.usage?.[id] !== 'foundation') return []
    const dependency = parentOf(id)?.id || id
    return props.modules
      .filter((item) => (item.native_dependencies || []).includes(dependency))
      .map((item) => item.name)
  }
  watch(
    () => `${props.scope}:${props.audience}`,
    () => {
      const stored = session.drafts[`${props.scope}:${props.audience}:native-filters`] as Row | undefined
      search.value = stored?.search || ''
      category.value = stored?.category || ''
      status.value = stored?.status || ''
      selected.value = ''
    },
    { immediate: true }
  )
  watch([search, category, status], () =>
    session.saveDraft(`${props.scope}:${props.audience}:native-filters`, {
      search: search.value,
      category: category.value,
      status: status.value
    })
  )
</script>

<style scoped>
  .native-catalog {
    min-width: 0;
  }
  .native-components {
    margin-top: 20px;
  }
  .native-components h3 {
    font-size: 14px;
    margin: 0 0 8px;
  }
  .native-component {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .native-overview {
    display: grid;
    grid-template-columns: minmax(0, 250px) minmax(0, 1fr);
    gap: 36px;
    border-bottom: 1px solid var(--ui-line);
    margin-bottom: 20px;
  }
  .native-progress h2 {
    font-size: 12px;
    font-weight: 500;
    color: var(--el-text-color-secondary);
    margin: 0;
  }
  .native-chart-legend {
    display: grid;
    gap: 6px;
    margin-bottom: 4px;
    font-size: 11px;
    color: var(--el-text-color-secondary);
  }
  .native-chart-legend > span {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .native-chart-legend i {
    width: 6px;
    height: 6px;
    border-radius: 2px;
    flex-shrink: 0;
  }
  .native-chart-legend b {
    margin-left: auto;
    font-weight: 500;
    color: var(--el-text-color-primary);
  }
  .native-overview .native-summary {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-content: center;
    gap: 24px 36px;
  }
  .native-summary p {
    grid-column: 1 / -1;
    font-size: 12px;
    color: #858d9b;
    line-height: 1.7;
  }
  @media (max-width: 700px) {
    .native-overview {
      grid-template-columns: minmax(0, 1fr);
      gap: 8px;
    }
    .native-progress {
      max-width: 420px;
    }
  }
  .native-tool-link {
    margin-top: 16px;
  }
  .native-acceptance {
    margin-top: 16px;
    overflow-wrap: anywhere;
  }
  .native-summary,
  .native-toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px;
    margin-bottom: 20px;
  }
  .native-summary {
    color: #687386;
    font-size: 13px;
  }
  .native-summary strong {
    display: block;
    color: var(--el-text-color-primary);
    font-size: 26px;
    font-weight: 600;
    line-height: 1.4;
    margin-top: 6px;
    font-variant-numeric: tabular-nums;
  }
  .native-toolbar .el-input {
    width: 260px;
  }
  .native-toolbar .el-select {
    width: 200px;
  }
  .native-table-wrap {
    width: 100%;
    overflow-x: auto;
  }
  .native-table-wrap .el-table {
    min-width: 860px;
  }
  .native-muted {
    color: #687386;
    font-size: 12px;
    margin-top: 5px;
  }
  .support-state {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .support-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #b88629;
  }
  .confirmed .support-dot {
    background: #169b7d;
  }
  .native-description {
    margin: 0 0 20px;
    color: #505b6c;
    line-height: 1.7;
  }
  @media (max-width: 600px) {
    .native-summary {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
    }
    .native-toolbar .el-input,
    .native-toolbar .el-select {
      width: 100%;
    }
  }
</style>
