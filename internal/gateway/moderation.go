package gateway

import (
	"context"
	"slices"
	"sort"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type ModerationSnapshot struct {
	SettingSnapshot
	Group      string                   `json:"group_id"`
	Members    []nativeauth.GroupMember `json:"members"`
	Policy     nativeauth.GroupPolicy   `json:"policy"`
	Notices    []nativeauth.GroupNotice `json:"-"`
	MutedIDs   []string                 `json:"muted_ids"`
	MutesKnown bool                     `json:"mutes_known"`
	Present    bool                     `json:"in_group"`
	GroupName  string                   `json:"group_name"`
	NativeTeam string                   `json:"-"`
	GroupBans  []lajiaoliao.GroupBan    `json:"group_bans,omitempty"`
	BansKnown  bool                     `json:"bans_known,omitempty"`
}

type moderationClient interface {
	settingsClient
	GroupPolicy(context.Context, string) (nativeauth.GroupPolicy, error)
	GroupNotices(context.Context, string) ([]nativeauth.GroupNotice, error)
	ChangeModeration(context.Context, nativeauth.SettingChange, nativewire.SubmissionFence) error
}

type ModerationFence func(context.Context, ModerationSnapshot, func(context.Context) error) error

func (m *ReceiverManager) Moderation(ctx context.Context, account, group string) (ModerationSnapshot, error) {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return ModerationSnapshot{}, err
	} else if chili {
		return m.chiliModeration(ctx, account, group, "", nil, nil)
	}
	return m.moderation(ctx, account, group, "", nil, nil, defaultSettingsClient)
}

func (m *ReceiverManager) ChangeModeration(ctx context.Context, account, operation string, change nativeauth.SettingChange, fence ModerationFence) error {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return err
	} else if chili {
		if (!nativeauth.ModerationKind(change.Kind) && change.Kind != "member_nickname" && !lajiaoliao.BanAction(change.Kind)) || chiliChange(change).Validate() != nil || fence == nil {
			return ErrMemberScope
		}
		_, err = m.chiliModeration(ctx, account, change.Group, operation, &change, fence)
		return err
	}
	if !nativeauth.ModerationKind(change.Kind) || change.Validate() != nil || fence == nil {
		return &nativeauth.Error{Code: "native_moderation_request_invalid"}
	}
	_, err := m.moderation(ctx, account, change.Group, operation, &change, fence, defaultSettingsClient)
	return err
}

func (m *ReceiverManager) moderation(ctx context.Context, account, group, operation string, change *nativeauth.SettingChange,
	fence ModerationFence, factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error),
) (ModerationSnapshot, error) {
	if (nativeauth.SettingChange{Kind: "mute_group", Group: group}).Validate() != nil {
		return ModerationSnapshot{}, &nativeauth.Error{Code: "native_moderation_request_invalid"}
	}
	value, err := m.readValue(ctx, account, "moderation:"+group+":"+operation, ReadRefresh, func(work context.Context) (any, error) {
		client, saved, base, err := m.openSettingsClient(work, account, factory)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		native, ok := client.(moderationClient)
		if !ok {
			return nil, &nativeauth.Error{Code: "native_moderation_unavailable"}
		}
		out := ModerationSnapshot{SettingSnapshot: base, Group: group, Members: []nativeauth.GroupMember{},
			MutedIDs: []string{}, Notices: []nativeauth.GroupNotice{}}
		groups, err := client.Directory(work, saved.Session.UID)
		if err != nil {
			return nil, err
		}
		var team string
		for _, row := range groups {
			if row.ID == group && row.Team != "" {
				if team != "" {
					return nil, ErrMemberScope
				}
				out.Present, team, out.GroupName, out.NativeTeam = true, row.Team, row.Name, row.Team
			}
		}
		if !out.Present {
			return nil, ErrMemberDirectory
		}
		roster, err := client.Members(work, group)
		if err != nil {
			return nil, err
		}
		if !roster.Complete || roster.Group != group {
			return nil, ErrMemberScope
		}
		out.Members = roster.Members
		for _, member := range roster.Members {
			if member.ID == out.SelfID && member.Transport == saved.Session.NIMID {
				out.SelfRole = member.Role
			}
		}
		if out.SelfRole != "admin" && out.SelfRole != "owner" && out.SelfRole != "member" {
			return nil, ErrMemberScope
		}
		out.Policy, err = native.GroupPolicy(work, group)
		if err != nil {
			return nil, err
		}
		out.Notices, err = native.GroupNotices(work, group)
		if err != nil {
			return nil, err
		}
		// A mute list can be read only through the existing connection owner.
		// Missing/offline evidence remains unknown, never an empty known list.
		m.mu.Lock()
		worker := m.jobs[account]
		var connection *ownedTextConnection
		if worker != nil {
			worker.mu.Lock()
			connection = worker.connection
			worker.mu.Unlock()
		}
		m.mu.Unlock()
		if connection != nil && connection.uid == saved.Session.UID && connection.sender == saved.Session.NIMID {
			intent := TextIntent{AccountID: account, ReceiverOwner: m.owner, AccountEpoch: base.AccountEpoch,
				Revision: worker.spec.Revision, Message: nativewire.GroupText{SenderID: connection.uid}}
			if _, current, err := m.textOwner(intent); err == nil && current == connection {
				body, _ := nativewire.MutedMembersRequest(team)
				reply, err := connection.transport.request(work, 8, 27, body)
				if err == nil && reply.Code == 200 {
					muted, err := nativewire.ParseMutedMembers(reply.Body, team)
					if err == nil && verifyTextMuteRoster(roster, muted, "") == nil {
						if _, current, err := m.textOwner(intent); err == nil && current == connection {
							out.MutesKnown = true
							for _, member := range roster.Members {
								if muted[member.Transport] {
									out.MutedIDs = append(out.MutedIDs, member.ID)
								}
							}
						}
					}
				}
			}
		}
		sort.Slice(out.Members, func(i, j int) bool { return out.Members[i].ID < out.Members[j].ID })
		sort.Slice(out.Notices, func(i, j int) bool { return out.Notices[i].ID < out.Notices[j].ID })
		sort.Strings(out.MutedIDs)
		out.Digest = nativeauth.SettingsDigest(struct {
			Self, Group, Role, Name string
			Members                 []nativeauth.GroupMember
			Policy                  nativeauth.GroupPolicy
			Notices                 []nativeauth.GroupNotice
		}{out.SelfID, group, out.SelfRole, out.GroupName, out.Members, out.Policy, out.Notices})
		if err := m.checkSettingSession(work, account, base); err != nil {
			return nil, err
		}
		if change != nil {
			if out.ValidateChange(*change) != nil || time.Since(base.SampledAt) > 5*time.Second {
				return nil, &nativeauth.Error{Code: "native_moderation_evidence_required"}
			}
			err := native.ChangeModeration(work, *change, func(ctx context.Context, submit func(context.Context) error) error {
				return fence(ctx, out, submit)
			})
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	})
	if err != nil {
		return ModerationSnapshot{}, err
	}
	out, ok := value.(ModerationSnapshot)
	if !ok {
		return ModerationSnapshot{}, ErrMemberScope
	}
	out.Members, out.Notices, out.MutedIDs = slices.Clone(out.Members), slices.Clone(out.Notices), slices.Clone(out.MutedIDs)
	return out, nil
}

func (s ModerationSnapshot) TargetRole(target string) string {
	for _, member := range s.Members {
		if member.ID == target {
			return member.Role
		}
	}
	return ""
}

func (s ModerationSnapshot) ValidateChange(change nativeauth.SettingChange) error {
	validation := change.Validate()
	if s.Platform == "lajiaoliao" {
		validation = chiliChange(change).Validate()
		if lajiaoliao.BanAction(change.Kind) {
			if validation != nil || change.Group != s.Group || !s.Present ||
				(s.SelfRole != "admin" && s.SelfRole != "owner") || change.Target == s.SelfID || !s.BansKnown {
				return &nativeauth.Error{Code: "native_moderation_role_required"}
			}
			if change.Kind == "remove_group_ban" {
				if !s.Banned(change.Target) || s.TargetRole(change.Target) == "owner" {
					return &nativeauth.Error{Code: "native_moderation_target_protected"}
				}
				return nil
			}
			role := s.TargetRole(change.Target)
			if role != "member" && !(s.SelfRole == "owner" && role == "admin") {
				return &nativeauth.Error{Code: "native_moderation_target_protected"}
			}
			return nil
		}
		if change.Kind == "member_nickname" {
			role := s.TargetRole(change.Target)
			if validation != nil || change.Group != s.Group || !s.Present || role == "" ||
				s.SelfRole != "member" && s.SelfRole != "admin" && s.SelfRole != "owner" ||
				change.Target != s.SelfID && (role == "owner" || s.SelfRole == "member" ||
					s.SelfRole == "admin" && role != "member") {
				return &nativeauth.Error{Code: "native_setting_role_required"}
			}
			return nil
		}
	}
	if !nativeauth.ModerationKind(change.Kind) || validation != nil || change.Group != s.Group ||
		!s.Present || s.SelfRole != "admin" && s.SelfRole != "owner" {
		return &nativeauth.Error{Code: "native_moderation_role_required"}
	}
	if change.Target != "" {
		role := s.TargetRole(change.Target)
		if change.Target == s.SelfID || role == "" || role == "unknown" || role == "owner" ||
			s.SelfRole == "admin" && role != "member" {
			return &nativeauth.Error{Code: "native_moderation_target_protected"}
		}
	}
	if change.Kind == "grant_admin" || change.Kind == "revoke_admin" {
		expected := "member"
		if change.Kind == "revoke_admin" {
			expected = "admin"
		}
		if s.SelfRole != "owner" || s.TargetRole(change.Target) != expected {
			return &nativeauth.Error{Code: "native_moderation_role_required"}
		}
	}
	return nil
}

func (s ModerationSnapshot) Matches(change nativeauth.SettingChange) bool {
	if s.Group != change.Group || !s.Present {
		return false
	}
	switch change.Kind {
	case "group_info":
		return s.GroupName == change.Text
	case "member_nickname":
		return s.Platform == "lajiaoliao" && slices.ContainsFunc(s.Members,
			func(member nativeauth.GroupMember) bool {
				return member.ID == change.Target && member.Card == change.Text
			})
	case "grant_admin":
		return change.Target != s.SelfID && s.TargetRole(change.Target) == "admin"
	case "revoke_admin":
		return change.Target != s.SelfID && s.TargetRole(change.Target) == "member"
	case "mute_group":
		return s.Policy.Muted
	case "unmute_group":
		return !s.Policy.Muted
	case "mute_member":
		// The list proves mute state only, not the requested duration.
		return false
	case "unmute_member":
		return s.MutesKnown && s.TargetRole(change.Target) != "" && !slices.Contains(s.MutedIDs, change.Target)
	case "kick_member":
		return s.TargetRole(change.Target) == ""
	case "kick_member_ban":
		return s.Platform == "lajiaoliao" && s.BansKnown && s.Banned(change.Target) && s.TargetRole(change.Target) == ""
	case "remove_group_ban":
		return s.Platform == "lajiaoliao" && s.BansKnown && !s.Banned(change.Target)
	case "publish_notice":
		return slices.ContainsFunc(s.Notices, func(n nativeauth.GroupNotice) bool { return n.Text == change.Text })
	}
	return false
}

func (s ModerationSnapshot) Banned(user string) bool {
	return slices.ContainsFunc(s.GroupBans, func(row lajiaoliao.GroupBan) bool { return row.User == user })
}

func (m *ReceiverManager) GroupBans(ctx context.Context, account, group string) (ModerationSnapshot, error) {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return ModerationSnapshot{}, err
	} else if !chili {
		return ModerationSnapshot{}, ErrMemberScope
	}
	return m.chiliModeration(ctx, account, group, "group-bans", nil, nil)
}
