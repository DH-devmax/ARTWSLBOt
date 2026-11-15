export type GroupAccess = {
  ownership_state?: 'active' | 'unverified' | 'paused'
  access_mode?: 'manage' | 'read_only'
  blocked_reason?: string
}

export function groupAccessReadOnly(access?: GroupAccess): boolean {
  if (access?.access_mode) return access.access_mode === 'read_only'
  return (
    access?.ownership_state === 'unverified' ||
    access?.ownership_state === 'paused'
  )
}

export function groupAccessLabel(access?: GroupAccess): string {
  if (access?.access_mode === 'manage' && access.ownership_state !== 'active')
    return '测试认领 · 可管理'
  if (access?.ownership_state === 'paused') return '需重新核验 · 只读'
  if (access?.ownership_state === 'unverified') return '未认领 · 只读'
  const label = access?.ownership_state === 'active' ? '已认领' : '认领状态待核对'
  return groupAccessReadOnly(access) ? `${label} · 只读` : label
}

export function groupAccessReason(access?: GroupAccess): string {
  const reasons: Record<string, string> = {
    group_ownership_proof_required: '尚未认领该群，请先完成群归属核验',
    group_ownership_reproof_required: '群归属需要重新核验，当前仅可查询',
    group_ownership_evidence_required: '群归属核验证据不足，当前仅可查询'
  }
  if (access?.blocked_reason) return reasons[access.blocked_reason] || access.blocked_reason
  if (!groupAccessReadOnly(access)) return ''
  if (access?.ownership_state === 'paused') return '群归属需要重新核验，当前仅可查询'
  if (groupAccessReadOnly(access)) return '当前群只读，仅可进行已授权的查询'
  return ''
}
