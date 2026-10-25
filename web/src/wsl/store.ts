import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type User } from './api'

export const useSession = defineStore('wsl-session', () => {
  const user = ref<User | null>(null)
  const permissions = ref<string[]>([])
  const workspace = ref<'wsl' | 'maibot'>('wsl')
  const selectedAccount = ref('')
  const drafts = ref<Record<string, unknown>>({})
  const ready = ref(false)

  async function restore() {
    try {
      const result = await api<{ user: User; permissions: string[] }>('/me')
      setUser(result.user, result.permissions)
    } finally {
      ready.value = true
    }
  }
  function setUser(value: User, grants: string[] = []) {
    user.value = value
    permissions.value = grants
    workspace.value =
      value.role === 'superadmin' &&
      grants.includes('workspaces:maibot') &&
      sessionStorage.getItem(`wsl:workspace:${value.id}`) === 'maibot'
        ? 'maibot'
        : 'wsl'
    selectedAccount.value =
      sessionStorage.getItem(`wsl:account:${value.id}:${workspace.value}`) || ''
    try {
      drafts.value = JSON.parse(sessionStorage.getItem(`wsl:drafts:${value.id}`) || '{}')
    } catch {
      drafts.value = {}
    }
  }
  function selectAccount(id: string) {
    selectedAccount.value = id
    if (user.value) sessionStorage.setItem(`wsl:account:${user.value.id}:${workspace.value}`, id)
  }
  function selectWorkspace(value: 'wsl' | 'maibot') {
    if (value === 'maibot' && !permissions.value.includes('workspaces:maibot')) return
    workspace.value = value
    if (user.value) {
      sessionStorage.setItem(`wsl:workspace:${user.value.id}`, value)
      selectedAccount.value = sessionStorage.getItem(`wsl:account:${user.value.id}:${value}`) || ''
    }
  }
  function saveDraft(scope: string, value: unknown) {
    drafts.value[scope] = JSON.parse(JSON.stringify(value))
    if (user.value)
      sessionStorage.setItem(`wsl:drafts:${user.value.id}`, JSON.stringify(drafts.value))
  }
  async function logout() {
    await api('/auth/logout', 'POST')
    clear()
  }
  function clear() {
    if (user.value) {
      sessionStorage.removeItem(`wsl:drafts:${user.value.id}`)
      for (const scope of ['wsl', 'maibot'])
        sessionStorage.removeItem(`wsl:account:${user.value.id}:${scope}`)
      sessionStorage.removeItem(`wsl:workspace:${user.value.id}`)
    }
    user.value = null
    permissions.value = []
    selectedAccount.value = ''
    workspace.value = 'wsl'
    drafts.value = {}
  }
  return {
    user,
    permissions,
    selectedAccount,
    workspace,
    drafts,
    ready,
    restore,
    setUser,
    selectAccount,
    selectWorkspace,
    saveDraft,
    logout,
    clear
  }
})
