import type { Row } from './api'

export function accountLabel(account?: Row): string {
  return account?.native_nickname?.trim() || account?.name || '未绑定机器人'
}

export function accountOptionLabel(account?: Row): string {
  const label = accountLabel(account)
  return account?.name && account.name !== label ? `${label} · ${account.name}` : label
}
