package control

import "wangshangliao/internal/platform"

// These are admission limits, not a claim that production load has been verified.
const (
	MaxAccountsPerWorkspace = platform.AccountsPerPool
	MaxGroupsPerAccount     = platform.GroupsPerAccount
	MaxAuthorizedGroups     = platform.GroupsPerPool
	MaxAIConcurrentCalls    = 4
)
