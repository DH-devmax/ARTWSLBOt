package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"wangshangliao/internal/nativeauth"
)

var ErrMemberScope = errors.New("native_member_scope_changed")
var ErrMemberDirectory = errors.New("native_member_directory_required")

type MemberSnapshot struct {
	nativeauth.MemberDirectory
	AccountEpoch      int64     `json:"account_epoch"`
	DeploymentVersion int64     `json:"deployment_version"`
	Team              string    `json:"team"`
	SampledAt         time.Time `json:"sampled_at"`
	SelfRole          string    `json:"self_role"`
	SelfID            string    `json:"self_id,omitempty"`
	CacheHit          bool      `json:"cache_hit,omitempty"`
}

type memberReadClient interface {
	Members(context.Context, string) (nativeauth.MemberDirectory, error)
	Close()
}

// Members is explicitly requested, read-only and superadmin-facing. It never
// writes account_groups or grants SaaS ownership/role evidence. HTTP callers
// must authorize both before requesting and before delivering this projection.
func (m *ReceiverManager) Members(ctx context.Context, account, group string) (MemberSnapshot, error) {
	return m.members(ctx, account, group, func(d nativeauth.Deployment, saved nativeauth.Session, ids nativeauth.RequestIDSource) (memberReadClient, error) {
		return nativeauth.Resume(d, saved, ids)
	})
}

func (m *ReceiverManager) members(ctx context.Context, account, group string,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (memberReadClient, error)) (MemberSnapshot, error) {
	var epoch, version int64
	err := m.DB.QueryRow(ctx, `SELECT a.epoch,d.version FROM accounts a CROSS JOIN native_deployment d
        WHERE a.id=$1 AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL`, account).Scan(&epoch, &version)
	if err != nil {
		return MemberSnapshot{}, ErrMemberScope
	}
	value, err := m.readValue(ctx, account, fmt.Sprintf("members:%d:%d:%s", epoch, version, group), ReadRefresh,
		func(work context.Context) (any, error) {
			var credential, deployment, directory []byte
			var sampled *time.Time
			var directoryError string
			err := m.DB.QueryRow(work, `SELECT a.credential_sealed,d.sealed,n.payload_sealed,n.sampled_at,n.error
                FROM accounts a CROSS JOIN native_deployment d
                JOIN native_directory_snapshots n ON n.account_id=$1
                WHERE a.id=$1 AND a.epoch=$2 AND d.version=$3 AND a.auth_state='authenticated'`,
				account, epoch, version).Scan(&credential, &deployment, &directory, &sampled, &directoryError)
			if err != nil {
				return nil, ErrMemberScope
			}
			if sampled == nil || time.Since(*sampled) > 10*time.Minute || sampled.After(time.Now()) || directoryError != "" {
				return nil, ErrMemberDirectory
			}
			open := func(aad string, sealed []byte, out any) error {
				plain, err := m.Vault.Open(aad, sealed)
				if err != nil {
					return err
				}
				defer clear(plain)
				return json.Unmarshal(plain, out)
			}
			var groups []nativeauth.DirectoryGroup
			if open("native-directory:"+account, directory, &groups) != nil {
				return nil, ErrMemberDirectory
			}
			index := slices.IndexFunc(groups, func(item nativeauth.DirectoryGroup) bool { return item.ID == group && item.Team != "" })
			if index < 0 {
				return nil, ErrMemberDirectory
			}
			var d nativeauth.Deployment
			var saved savedNativeSession
			if open("native-deployment:v1", deployment, &d) != nil ||
				open("native-session:"+account, credential, &saved) != nil || saved.DeploymentVersion != version {
				return nil, ErrMemberScope
			}
			var ids nativeauth.RequestIDSource
			if m.IDs != nil {
				ids = m.IDs(version)
			}
			client, err := factory(d, saved.Session, ids)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			roster, err := client.Members(work, group)
			if err != nil {
				return nil, err
			}
			var current []byte
			err = m.DB.QueryRow(work, `SELECT a.credential_sealed FROM accounts a CROSS JOIN native_deployment d
                WHERE a.id=$1 AND a.epoch=$2 AND d.version=$3 AND a.auth_state='authenticated'`,
				account, epoch, version).Scan(&current)
			if err != nil || !bytes.Equal(current, credential) {
				return nil, ErrMemberScope
			}
			selfRole := "unknown"
			for _, member := range roster.Members {
				if member.ID == fmt.Sprint(saved.Session.UID) {
					if member.Transport != "" && member.Transport != saved.Session.NIMID {
						return nil, &nativeauth.Error{Code: "native_member_identity"}
					}
					if member.Transport != "" {
						selfRole = member.Role
					}
				}
			}
			return MemberSnapshot{MemberDirectory: roster, AccountEpoch: epoch, DeploymentVersion: version,
				Team: groups[index].Team, SampledAt: time.Now().UTC(), SelfRole: selfRole, SelfID: fmt.Sprint(saved.Session.UID)}, nil
		})
	if err != nil {
		return MemberSnapshot{}, err
	}
	out, ok := value.(MemberSnapshot)
	if !ok {
		return MemberSnapshot{}, ErrMemberScope
	}
	out.Members = slices.Clone(out.Members)
	return out, nil
}
