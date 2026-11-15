package gateway

import (
	"context"
	"slices"
	"time"

	"wangshangliao/internal/nativeauth"
)

type InvitationSnapshot struct {
	SettingSnapshot
	Group    string                        `json:"group_id"`
	Records  []nativeauth.InvitationRecord `json:"records"`
	Cursor   string                        `json:"cursor"`
	Complete bool                          `json:"complete"`
}

type invitationCache struct {
	Base   SettingSnapshot
	Rows   []nativeauth.InvitationRecord
	Groups []nativeauth.DirectoryGroup
	Cursor string
}

func (m *ReceiverManager) Invitations(ctx context.Context, account, group string) (InvitationSnapshot, error) {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return InvitationSnapshot{}, err
	} else if chili {
		return m.chiliInvitations(ctx, account, group)
	}
	return m.invitationRecords(ctx, account, group, defaultSettingsClient)
}

func (m *ReceiverManager) invitationRecords(ctx context.Context, account, group string,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error)) (InvitationSnapshot, error) {
	if (nativeauth.SettingChange{Kind: "group_leave", Group: group}).Validate() != nil {
		return InvitationSnapshot{}, ErrMemberScope
	}
	value, err := m.readValue(ctx, account, "invitation-account-window", ReadBackground, func(work context.Context) (any, error) {
		client, saved, base, err := m.openSettingsClient(work, account, factory)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		m.mu.Lock()
		cached, ok := m.invitations[account]
		m.mu.Unlock()
		if !ok || time.Since(cached.Base.SampledAt) > 30*time.Second ||
			cached.Base.CredentialDigest != base.CredentialDigest || cached.Base.AccountEpoch != base.AccountEpoch ||
			cached.Base.AccountVersion != base.AccountVersion || cached.Base.DeploymentVersion != base.DeploymentVersion {
			reader, ok := client.(interface {
				InvitationRecords(context.Context) ([]nativeauth.InvitationRecord, string, error)
			})
			if !ok {
				return nil, &nativeauth.Error{Code: "native_invitation_unavailable"}
			}
			cached = invitationCache{Base: base}
			cached.Groups, err = client.Directory(work, saved.Session.UID)
			if err != nil {
				return nil, err
			}
			cached.Rows, cached.Cursor, err = reader.InvitationRecords(work)
			if err != nil {
				return nil, err
			}
			if err = m.checkSettingSession(work, account, base); err != nil {
				return nil, err
			}
			m.mu.Lock()
			if m.invitations == nil {
				m.invitations = map[string]invitationCache{}
			}
			if len(m.invitations) >= 20 {
				for id, entry := range m.invitations {
					if time.Since(entry.Base.SampledAt) > 30*time.Second {
						delete(m.invitations, id)
					}
				}
				if len(m.invitations) >= 20 {
					for id := range m.invitations {
						delete(m.invitations, id)
						break
					}
				}
			}
			m.invitations[account] = cached
			m.mu.Unlock()
		}
		if err := m.checkSettingSession(work, account, base); err != nil {
			return nil, err
		}
		return cached, nil
	})
	if err != nil {
		return InvitationSnapshot{}, err
	}
	cached, ok := value.(invitationCache)
	if !ok || !slices.ContainsFunc(cached.Groups, func(g nativeauth.DirectoryGroup) bool { return g.ID == group && g.Team != "" }) {
		return InvitationSnapshot{}, ErrMemberDirectory
	}
	out := InvitationSnapshot{SettingSnapshot: cached.Base, Group: group, Cursor: cached.Cursor,
		Records: []nativeauth.InvitationRecord{}, Complete: false}
	for _, record := range cached.Rows {
		if record.Group == group {
			if record.Inviter != nil {
				copy := *record.Inviter
				record.Inviter = &copy
			}
			if record.At != nil {
				copy := *record.At
				record.At = &copy
			}
			out.Records = append(out.Records, record)
		}
	}
	return out, nil
}
