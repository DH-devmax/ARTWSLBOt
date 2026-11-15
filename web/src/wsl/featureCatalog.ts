export interface NativeCapability {
  id: string
  name: string
  category: string
  kind: 'read' | 'action' | 'session'
  support: 'confirmed' | 'review'
  integration: 'implemented' | 'partial' | 'pending' | 'accepted' | 'deferred'
  role: string
  summary: string
  evidence: string
}

export interface ControlledAcceptance {
  verified_at: string
  scope: string
  result: string
  remaining: string
  evidence: string
}

export interface NativeDetail extends NativeCapability {
  parent_id: string
  acceptance?: ControlledAcceptance
}

export const integrationLabel = (status: string) =>
  ({
    implemented: '已实现 · 待完整验收',
    partial: '部分接入',
    pending: '待接入',
    deferred: '已暂缓',
    accepted: '已验收'
  })[status] || '待核验'

export const kindLabel = (kind: string) =>
  ({ read: '读取', action: '操作', session: '认证' })[kind] || '无'
