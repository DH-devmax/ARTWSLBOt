import type { Row } from './api'

export type SettingKind =
  | 'nickname'
  | 'member_nickname'
  | 'search'
  | 'friend_policy'
  | 'group_policy'
  | 'audio_video'
  | 'profile_gender'
  | 'profile_birth'
  | 'profile_signature'
  | 'push_notifications'

export interface SettingsDraft {
  nickname: string
  searchAccount: boolean
  searchMobile: boolean
  friendPolicy: string
  groupPolicy: string
  audioVideo: boolean
  gender: string
  birth: string
  signature: string
  notifications: Row
}

export const friendPolicies = [
  { value: 'FRIEND_NEED_APPROVAL', label: '需要验证' },
  { value: 'FRIEND_ANY_ONE', label: '允许任何人' }
]
export const groupPolicies = [
  { value: 'GROUP_REJECT_JOIN', label: '拒绝邀请' },
  { value: 'GROUP_APPROVAL_JOIN', label: '允许邀请' },
  { value: 'GROUP_ASK_JOIN', label: '需要本人同意' }
]
export const chiliFriendPolicies = [
  ...friendPolicies,
  { value: 'FRIEND_REJECT_ALL', label: '拒绝好友申请' }
]
export const chiliGroupPolicies = groupPolicies.filter(
  (option) => option.value !== 'GROUP_ASK_JOIN'
)
export const settingLabels: Record<SettingKind, string> = {
  nickname: '原生昵称',
  member_nickname: '群内昵称',
  search: '允许搜索我的方式',
  friend_policy: '好友申请',
  group_policy: '群邀请',
  audio_video: '语音视频通话',
  profile_gender: '性别',
  profile_birth: '生日',
  profile_signature: '个性签名',
  push_notifications: '原生消息通知'
}

export function settingsDraft(snapshot: Row | null): SettingsDraft {
  const value = snapshot?.settings
  return {
    nickname: snapshot?.member?.card ?? value?.nickname ?? '',
    searchAccount: value?.searchable_account === true,
    searchMobile: value?.searchable_mobile === true,
    friendPolicy: value?.friend_policy ?? '',
    groupPolicy: value?.group_policy ?? '',
    audioVideo: value?.audio_video === true,
    gender: String(value?.profile_details?.gender ?? ''),
    birth: value?.profile_details?.birth > 0 ? String(value.profile_details.birth) : '',
    signature: value?.profile_details?.signature ?? '',
    notifications: { ...(value?.profile_details?.notifications || {}) }
  }
}

export function settingAvailable(kind: SettingKind, snapshot: Row | null): boolean {
  if (!snapshot) return false
  const value = snapshot.settings
  switch (kind) {
    case 'nickname':
      return typeof value?.nickname === 'string'
    case 'member_nickname':
      return (
        typeof snapshot.member?.card === 'string' &&
        snapshot.native_actions_enabled === true &&
        snapshot.nickname_locked === false
      )
    case 'search':
      return (
        typeof value?.searchable_account === 'boolean' &&
        typeof value?.searchable_mobile === 'boolean'
      )
    case 'friend_policy':
      return (snapshot.platform === 'lajiaoliao' ? chiliFriendPolicies : friendPolicies).some(
        (option) => option.value === value?.friend_policy
      )
    case 'group_policy':
      return (snapshot.platform === 'lajiaoliao' ? chiliGroupPolicies : groupPolicies).some(
        (option) => option.value === value?.group_policy
      )
    case 'audio_video':
      return typeof value?.audio_video === 'boolean'
    case 'profile_gender':
      return (
        snapshot.platform === 'lajiaoliao' && [0, 1, 2].includes(value?.profile_details?.gender)
      )
    case 'profile_birth':
    case 'profile_signature':
      return snapshot.platform === 'lajiaoliao' && !!value?.profile_details
    case 'push_notifications': {
      const n = value?.profile_details?.notifications
      return (
        snapshot.platform === 'lajiaoliao' &&
        !!n &&
        ['showMessageDetail', 'receiveNewMessage', 'groupMessageNotification', 'vibration'].every(
          (k) => typeof n[k] === 'boolean'
        ) &&
        typeof n.notificationSound === 'string' &&
        n.notificationSound.length > 0 &&
        n.notificationSound.length <= 64
      )
    }
  }
}

export function settingChanged(
  kind: SettingKind,
  draft: SettingsDraft,
  snapshot: Row | null
): boolean {
  if (!settingAvailable(kind, snapshot)) return false
  const original = settingsDraft(snapshot)
  switch (kind) {
    case 'nickname':
    case 'member_nickname':
      return draft.nickname !== original.nickname
    case 'search':
      return (
        draft.searchAccount !== original.searchAccount ||
        draft.searchMobile !== original.searchMobile
      )
    case 'friend_policy':
      return draft.friendPolicy !== original.friendPolicy
    case 'group_policy':
      return draft.groupPolicy !== original.groupPolicy
    case 'audio_video':
      return draft.audioVideo !== original.audioVideo
    case 'profile_gender':
      return draft.gender !== original.gender
    case 'profile_birth':
      return draft.birth !== original.birth
    case 'profile_signature':
      return draft.signature !== original.signature
    case 'push_notifications':
      return JSON.stringify(draft.notifications) !== JSON.stringify(original.notifications)
  }
}

export function settingNameError(name: string, allowEmpty: boolean): string {
  if (!allowEmpty && !name.trim()) return '请输入昵称'
  if (name !== name.trim()) return '昵称不能包含首尾空格'
  if (/[\u0000-\u001f\u007f-\u009f]/u.test(name)) return '昵称不能包含换行或控制字符'
  if (name.length > 20) return '昵称超过 20 字符限制，表情符号通常占 2 字符'
  // Unpaired surrogates cannot be represented by the native UTF-8 contract.
  if (!Array.from(name).every((char) => char.length === 2 || !/[\ud800-\udfff]/u.test(char)))
    return '昵称包含不完整的字符'
  return ''
}

export function settingValue(kind: SettingKind, draft: SettingsDraft): string {
  switch (kind) {
    case 'nickname':
    case 'member_nickname':
      return draft.nickname || '未设置'
    case 'search':
      return (
        [draft.searchAccount && '账号', draft.searchMobile && '手机号']
          .filter(Boolean)
          .join('、') || '均不允许'
      )
    case 'friend_policy':
      return (
        chiliFriendPolicies.find((option) => option.value === draft.friendPolicy)?.label || '未适配'
      )
    case 'group_policy':
      return groupPolicies.find((option) => option.value === draft.groupPolicy)?.label || '未适配'
    case 'audio_video':
      return draft.audioVideo ? '开启' : '关闭'
    case 'profile_gender':
      return (
        ({ '0': '未设置', '1': '男', '2': '女' } as Record<string, string>)[draft.gender] ||
        '未采集'
      )
    case 'profile_birth':
      return draft.birth ? new Date(Number(draft.birth)).toLocaleDateString('zh-CN') : '未设置'
    case 'profile_signature':
      return draft.signature || '未设置'
    case 'push_notifications':
      return '所选通知设置'
  }
}

export function settingChange(kind: SettingKind, draft: SettingsDraft): Row {
  switch (kind) {
    case 'nickname':
    case 'member_nickname':
      return { kind, text: draft.nickname }
    case 'search':
      return {
        kind,
        searchable_account: draft.searchAccount,
        searchable_mobile: draft.searchMobile
      }
    case 'friend_policy':
      return { kind, text: draft.friendPolicy }
    case 'group_policy':
      return { kind, text: draft.groupPolicy }
    case 'audio_video':
      return { kind, enabled: draft.audioVideo }
    case 'profile_gender':
      return { kind, text: draft.gender }
    case 'profile_birth':
      return { kind, text: draft.birth }
    case 'profile_signature':
      return { kind, text: draft.signature }
    case 'push_notifications':
      return { kind, text: JSON.stringify(draft.notifications) }
  }
}

export function restoreSetting(kind: SettingKind, draft: SettingsDraft, snapshot: Row): void {
  const original = settingsDraft(snapshot)
  switch (kind) {
    case 'nickname':
    case 'member_nickname':
      draft.nickname = original.nickname
      break
    case 'search':
      draft.searchAccount = original.searchAccount
      draft.searchMobile = original.searchMobile
      break
    case 'friend_policy':
      draft.friendPolicy = original.friendPolicy
      break
    case 'group_policy':
      draft.groupPolicy = original.groupPolicy
      break
    case 'audio_video':
      draft.audioVideo = original.audioVideo
      break
    case 'profile_gender':
      draft.gender = original.gender
      break
    case 'profile_birth':
      draft.birth = original.birth
      break
    case 'profile_signature':
      draft.signature = original.signature
      break
    case 'push_notifications':
      draft.notifications = { ...original.notifications }
      break
  }
}
