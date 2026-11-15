export type Workspace = 'user' | 'wsl' | 'lajiaoliao' | 'maibot'
export type Console = 'admin' | 'user'
export const consoleNames: Record<Console, string> = {
  admin: '管理后台',
  user: '用户中心'
}
export interface WorkspaceEntry {
  id: Workspace
  name: string
  purpose: string
  platform?: 'wsl' | 'lajiaoliao'
}
export const workspaceNames: Record<Workspace, string> = {
  user: '用户中心',
  wsl: '旺商聊机器人',
  lajiaoliao: '辣椒聊机器人',
  maibot: '热群机器人'
}
export const workspaceLogos = {
  wsl: '/wsl-logo.png',
  lajiaoliao: '/lajiaoliao-logo.png',
  maibot: '/maibot-logo.png'
}
