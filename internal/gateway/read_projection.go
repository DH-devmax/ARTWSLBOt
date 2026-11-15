package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"wangshangliao/internal/scopecache"
)

// ProjectionKey contains no credential material. Live versions are checked on
// both sides of the read, including cache hits and coalesced in-flight results.
func (m *ReceiverManager) projectionKey(ctx context.Context, actor, tenant, account, group string) (scopecache.Key, error) {
	k := scopecache.Key{Account: account, Group: group, Actor: actor, Tenant: tenant, Kind: "member_projection", Owner: m.owner}
	var connectionOwner string
	if tenant == "" {
		k.Tenant = "platform"
	}
	err := m.DB.QueryRow(ctx, `SELECT a.platform,a.purpose,a.epoch,a.version,
		coalesce(r.revision,0),coalesce(d.version,0),coalesce(b.version,0),coalesce(b.epoch,0),coalesce(r.owner,'')
		FROM accounts a LEFT JOIN account_receivers r ON r.account_id=a.id
		LEFT JOIN native_deployment d ON a.platform='wsl'
		LEFT JOIN native_groups g ON g.platform=a.platform AND g.native_id=$2
		LEFT JOIN group_bindings b ON b.group_id=g.id
		WHERE a.id=$1 AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL`,
		account, group).Scan(&k.Platform, &k.Purpose, &k.AccountEpoch, &k.AccountVersion,
		&k.ReceiverVersion, &k.DeploymentVersion, &k.BindingVersion, &k.ConfigVersion, &connectionOwner)
	ownerHash := sha256.Sum256([]byte(m.owner + "\x00" + connectionOwner))
	k.Owner = fmt.Sprintf("%x", ownerHash)
	return k, err
}

// MemberProjection is for display/candidate lookup only. Execution continues
// to obtain fresh role and credential evidence through its existing ports.
func (m *ReceiverManager) MemberProjection(ctx context.Context, actor, tenant, account, group string) (MemberSnapshot, error) {
	return m.memberProjection(ctx, actor, tenant, account, group, func(work context.Context, key scopecache.Key) (MemberSnapshot, error) {
		if key.Platform == "lajiaoliao" {
			return m.ChiliMemberSnapshot(work, account, group)
		}
		return m.Members(work, account, group)
	})
}

func (m *ReceiverManager) memberProjection(ctx context.Context, actor, tenant, account, group string,
	fetch func(context.Context, scopecache.Key) (MemberSnapshot, error)) (MemberSnapshot, error) {
	key, err := m.projectionKey(ctx, actor, tenant, account, group)
	if err != nil || actor == "" {
		return MemberSnapshot{}, ErrMemberScope
	}
	if m.ReadCache == nil {
		return MemberSnapshot{}, ErrReadOwner
	}
	raw, hit, err := m.ReadCache.Load(ctx, key, func(work context.Context) ([]byte, error) {
		view, e := fetch(work, key)
		if e != nil {
			return nil, e
		}
		view.SelfRole = "unknown"
		return json.Marshal(view)
	})
	if err != nil {
		return MemberSnapshot{}, err
	}
	current, e := m.projectionKey(ctx, actor, tenant, account, group)
	if e != nil || current != key {
		m.ReadCache.InvalidateAccount(account)
		return MemberSnapshot{}, ErrMemberScope
	}
	var view MemberSnapshot
	if json.Unmarshal(raw, &view) != nil || view.AccountEpoch != key.AccountEpoch {
		return MemberSnapshot{}, errors.New("native_projection_invalid")
	}
	view.CacheHit = hit
	return view, nil
}
