import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type User } from './api'
import { type Console, type Workspace, type WorkspaceEntry } from './workspaces'

export const useSession = defineStore('wsl-session', () => {
  const user = ref<User | null>(null)
  const permissions = ref<string[]>([])
  const console = ref<Console>('user')
  const workspace = ref<Workspace>('wsl')
  const panels = ref<WorkspaceEntry[]>([])
  const selectedAccount = ref('')
  const drafts = ref<Record<string, unknown>>({})
  const ready = ref(false)
  const removedScopes = new Set<string>()

  async function restore() {
    try {
      const result = await api<{ user: User; permissions: string[] }>('/me')
      setUser(result.user, result.permissions)
      await loadPanels()
    } finally {
      ready.value = true
    }
  }
  async function loadPanels(signal?: AbortSignal) {
    const owner = user.value?.id
    const result = await api<{ items: WorkspaceEntry[] }>('/workspaces', 'GET', undefined, signal)
    if (owner !== user.value?.id || signal?.aborted) return
    panels.value = result.items
    if (!panels.value.some((p) => p.id === workspace.value && p.id !== 'user')) {
      const first = panels.value.find((p) => p.id !== 'user' && p.id !== 'maibot')
      workspace.value = first?.id || 'wsl'
    }
  }
  function setUser(value: User, grants: string[] = []) {
    if (user.value?.id !== value.id) removedScopes.clear()
    user.value = value
    permissions.value = grants
    console.value =
      value.role === 'superadmin'
        ? sessionStorage.getItem(`wsl:console:${value.id}`) === 'user'
          ? 'user'
          : 'admin'
        : 'user'
    const remembered = sessionStorage.getItem(`wsl:workspace:${value.id}`) as Workspace
    workspace.value =
      console.value === 'admin' &&
      value.role === 'superadmin' &&
      grants.includes('workspaces:maibot') &&
      sessionStorage.getItem(`wsl:workspace:${value.id}`) === 'maibot'
        ? 'maibot'
        : ['wsl', 'lajiaoliao'].includes(remembered)
          ? remembered
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
  function rememberAccount(id: string, target: Workspace) {
    if (!user.value || !panels.value.some((panel) => panel.id === target)) return
    if (target === workspace.value) selectAccount(id)
    else sessionStorage.setItem(`wsl:account:${user.value.id}:${target}`, id)
  }
  function selectWorkspace(value: Workspace) {
    if (value === 'user') {
      selectConsole('user')
      return
    }
    if (
      value === 'maibot' &&
      (console.value !== 'admin' ||
        user.value?.role !== 'superadmin' ||
        !permissions.value.includes('workspaces:maibot'))
    )
      return
    if (!panels.value.some((p) => p.id === value)) return
    workspace.value = value
    if (user.value) {
      sessionStorage.setItem(`wsl:workspace:${user.value.id}`, value)
      selectedAccount.value = sessionStorage.getItem(`wsl:account:${user.value.id}:${value}`) || ''
    }
  }
  function selectConsole(value: Console) {
    if (!user.value || (value === 'admin' && user.value.role !== 'superadmin')) return
    if (value === 'user' && workspace.value === 'maibot') {
      const platform = panels.value.find((p) => p.id === 'wsl' || p.id === 'lajiaoliao')
      if (platform) selectWorkspace(platform.id)
    }
    console.value = value
    sessionStorage.setItem(`wsl:console:${user.value.id}`, value)
  }
  function saveDraft(scope: string, value: unknown) {
    if (scope.split(':').some((part) => removedScopes.has(part))) return
    drafts.value[scope] = JSON.parse(JSON.stringify(value))
    if (user.value)
      sessionStorage.setItem(`wsl:drafts:${user.value.id}`, JSON.stringify(drafts.value))
  }
  function removeAccountData(id: string, scope: Workspace, groups: string[] = []) {
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
      for (const scope of ['wsl', 'maibot', 'lajiaoliao', 'user'])
        sessionStorage.removeItem(`wsl:account:${user.value.id}:${scope}`)
      sessionStorage.removeItem(`wsl:workspace:${user.value.id}`)
      sessionStorage.removeItem(`wsl:console:${user.value.id}`)
    }
    user.value = null
    permissions.value = []
    selectedAccount.value = ''
    workspace.value = 'wsl'
    console.value = 'user'
    drafts.value = {}
    removedScopes.clear()
    panels.value = []
  }
  return {
    user,
    permissions,
    selectedAccount,
    workspace,
    console,
    panels,
    loadPanels,
    drafts,
    ready,
    restore,
    setUser,
    selectAccount,
    rememberAccount,
    selectWorkspace,
    selectConsole,
    saveDraft,
    removeAccountData,
    logout,
    clear
  }
})
