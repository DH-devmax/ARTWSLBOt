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
  const removedScopes = new Set<string>()

  async function restore() {
    try {
      const result = await api<{ user: User; permissions: string[] }>('/me')
      setUser(result.user, result.permissions)
    } finally {
      ready.value = true
    }
  }
  function setUser(value: User, grants: string[] = []) {
    if (user.value?.id !== value.id) removedScopes.clear()
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
    if (scope.split(':').some((part) => removedScopes.has(part))) return
    drafts.value[scope] = JSON.parse(JSON.stringify(value))
    if (user.value)
      sessionStorage.setItem(`wsl:drafts:${user.value.id}`, JSON.stringify(drafts.value))
  }
  function removeAccountData(id: string, scope: 'wsl' | 'maibot', groups: string[] = []) {
    if (!user.value) return
    for (const key of [id, ...groups]) removedScopes.add(key)
    for (const key of Object.keys(drafts.value)) {
      if (key.split(':').some((part) => removedScopes.has(part))) delete drafts.value[key]
    }
    sessionStorage.setItem(`wsl:drafts:${user.value.id}`, JSON.stringify(drafts.value))
    const selectionKey = `wsl:account:${user.value.id}:${scope}`
    if (sessionStorage.getItem(selectionKey) === id) sessionStorage.removeItem(selectionKey)
    if (workspace.value === scope && selectedAccount.value === id) selectedAccount.value = ''
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
    removedScopes.clear()
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
    removeAccountData,
    logout,
    clear
  }
})
