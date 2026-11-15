// Package platform defines native identity scopes independently of UI workspaces.
// It contains no transport, database or authorization implementation.
package platform

import "errors"

type ID string
type Purpose string

const (
	WSL        ID      = "wsl"
	LajiaoLiao ID      = "lajiaoliao"
	Management Purpose = "management"
	Chat       Purpose = "chat"

	AccountsPerPool  = 10
	GroupsPerAccount = 30
	GroupsPerPool    = AccountsPerPool * GroupsPerAccount
)

func (p ID) Valid() bool      { return p == WSL || p == LajiaoLiao }
func (p Purpose) Valid() bool { return p == Management || p == Chat }

func Workspace(p ID, purpose Purpose) (string, error) {
	if !p.Valid() || !purpose.Valid() {
		return "", errors.New("invalid_account_scope")
	}
	if purpose == Chat {
		return "maibot", nil
	}
	return string(p), nil
}

// Identity is a platform-qualified native identifier, never a display name.
type Identity struct {
	Platform ID
	ID       string
}

func (i Identity) Valid() bool {
	return i.Platform.Valid() && len(i.ID) > 0 && len(i.ID) <= 200
}
