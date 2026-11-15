package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func (m *ReceiverManager) chiliAccount(ctx context.Context, account string) (bool, error) {
	var platform string
	err := m.DB.QueryRow(ctx, `SELECT platform FROM accounts WHERE id=$1`, account).Scan(&platform)
	return platform == "lajiaoliao", err
}

func chiliChange(c nativeauth.SettingChange) lajiaoliao.Change {
	return lajiaoliao.Change{Kind: c.Kind, Group: c.Group, Target: c.Target, Text: c.Text, Minutes: c.Minutes,
		Enabled: c.Enabled, SearchAccount: c.SearchAccount, SearchMobile: c.SearchMobile}
}

func (m *ReceiverManager) openChiliNative(ctx context.Context, account string) (lajiaoliao.Session, SettingSnapshot, error) {
	var sealed []byte
	var session lajiaoliao.Session
	out := SettingSnapshot{Platform: "lajiaoliao"}
	err := m.DB.QueryRow(ctx, `SELECT a.native_uid,a.credential_sealed,a.epoch,a.version,a.connection_enabled,
		coalesce(r.owner,''),coalesce(r.revision,0)
		FROM accounts a LEFT JOIN account_receivers r ON r.account_id=a.id
		WHERE a.id=$1 AND a.platform='lajiaoliao' AND a.auth_state='authenticated'
		AND a.credential_sealed IS NOT NULL`, account).Scan(&out.SelfID, &sealed, &out.AccountEpoch,
		&out.AccountVersion, &out.ActionsEnabled, &out.ReceiverOwner, &out.ReceiverRevision)
	if err != nil {
		return session, out, ErrMemberScope
	}
	plain, err := m.Vault.Open("lajiaoliao-session:"+account, sealed)
	if err != nil {
		return session, out, err
	}
	defer clear(plain)
	if json.Unmarshal(plain, &session) != nil || session.UserID != out.SelfID || session.IMToken == "" {
		return session, out, ErrMemberScope
	}
	out.CredentialDigest = sha256.Sum256(sealed)
	out.SampledAt = time.Now().UTC()
	return session, out, nil
}

func (m *ReceiverManager) chiliModeration(ctx context.Context, account, group, operation string,
	change *nativeauth.SettingChange, fence ModerationFence) (ModerationSnapshot, error) {
	if !lajiaoliao.ValidID(group) {
		return ModerationSnapshot{}, ErrMemberScope
	}
	value, err := m.readValue(ctx, account, "chili-moderation:"+group+":"+operation, ReadRefresh, func(work context.Context) (any, error) {
		session, base, err := m.openChiliNative(work, account)
		if err != nil {
			return nil, err
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		info, err := client.GroupInfo(work, session, group)
		if err != nil {
			return nil, err
		}
		out := ModerationSnapshot{SettingSnapshot: base, Group: group, GroupName: info.Name, Members: []nativeauth.GroupMember{},
			MutedIDs: []string{}, MutesKnown: true, Notices: []nativeauth.GroupNotice{},
			Policy: nativeauth.GroupPolicy{Group: group, Muted: *info.Status == lajiaoliao.GroupStatusMuted}}
		if info.Notification != "" {
			out.Notices = append(out.Notices, nativeauth.GroupNotice{ID: group, Text: info.Notification})
		}
		seen := map[string]bool{}
		complete := false
		for page := 1; page <= 100; page++ {
			members, more, err := client.Members(work, session, group, page)
			if err != nil {
				return nil, err
			}
			for _, member := range members {
				if seen[member.UserID] {
					return nil, ErrMemberScope
				}
				seen[member.UserID] = true
				role := map[int]string{20: "member", 60: "admin", 100: "owner"}[member.Role]
				out.Members = append(out.Members, nativeauth.GroupMember{ID: member.UserID, Transport: member.UserID,
					Name: member.Name, Card: member.Name, Role: role})
				if member.UserID == base.SelfID {
					out.SelfRole, out.Present = role, true
				}
				if member.MuteEnd > time.Now().Unix() {
					out.MutedIDs = append(out.MutedIDs, member.UserID)
				}
			}
			if !more {
				complete = true
				break
			}
		}
		if !complete || !out.Present {
			return nil, ErrMemberScope
		}
		if operation == "group-bans" || change != nil && lajiaoliao.BanAction(change.Kind) {
			if out.SelfRole != "owner" && out.SelfRole != "admin" {
				return nil, &lajiaoliao.Failure{Code: "lajiaoliao_native_role_required"}
			}
			seenBans := map[string]bool{}
			for page := 1; page <= 100; page++ {
				bans, total, err := client.GroupBans(work, session, group, page)
				if err != nil {
					return nil, err
				}
				for _, ban := range bans {
					if seenBans[ban.User] {
						return nil, &lajiaoliao.Failure{Code: "lajiaoliao_group_ban_shape"}
					}
					seenBans[ban.User] = true
					out.GroupBans = append(out.GroupBans, ban)
				}
				if len(out.GroupBans) == total {
					out.BansKnown = true
					break
				}
				if len(bans) < 100 {
					break
				}
			}
			if !out.BansKnown {
				return nil, &lajiaoliao.Failure{Code: "lajiaoliao_group_ban_incomplete"}
			}
			sort.Slice(out.GroupBans, func(i, j int) bool { return out.GroupBans[i].User < out.GroupBans[j].User })
		}
		sort.Slice(out.Members, func(i, j int) bool { return out.Members[i].ID < out.Members[j].ID })
		sort.Strings(out.MutedIDs)
		out.Digest = nativeauth.SettingsDigest(struct {
			Group, Self, Role, Name string
			Members                 []nativeauth.GroupMember
			Policy                  nativeauth.GroupPolicy
			Notices                 []nativeauth.GroupNotice
			Bans                    []lajiaoliao.GroupBan
			BansKnown               bool
		}{group, out.SelfID, out.SelfRole, out.GroupName, out.Members, out.Policy, out.Notices, out.GroupBans, out.BansKnown})
		if err := m.checkSettingSession(work, account, out.SettingSnapshot); err != nil {
			return nil, err
		}
		if change != nil {
			if fence == nil || out.ValidateChange(*change) != nil {
				return nil, &lajiaoliao.Failure{Code: "lajiaoliao_native_role_required"}
			}
			err = client.Execute(work, session, chiliChange(*change), func(write context.Context, submit func(context.Context) error) error {
				if time.Since(out.SampledAt) > 5*time.Second {
					return ErrMemberScope
				}
				return fence(write, out, submit)
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
		return out, ErrMemberScope
	}
	out.Members = append([]nativeauth.GroupMember{}, out.Members...)
	out.MutedIDs = append([]string{}, out.MutedIDs...)
	out.Notices = append([]nativeauth.GroupNotice{}, out.Notices...)
	out.GroupBans = append([]lajiaoliao.GroupBan{}, out.GroupBans...)
	return out, nil
}

func (m *ReceiverManager) chiliSocial(ctx context.Context, account, group, operation string,
	change *nativeauth.SettingChange, fence SocialFence, applications ...bool) (SocialSnapshot, error) {
	if group != "" && !lajiaoliao.ValidID(group) {
		return SocialSnapshot{}, ErrMemberScope
	}
	includeApplications := len(applications) > 0 && applications[0] || change != nil && (change.Kind == "friend_approve" || change.Kind == "friend_reject")
	readKey := "chili-social:"
	if includeApplications {
		readKey = "chili-social-applications:"
	}
	value, err := m.readValue(ctx, account, readKey+group+":"+operation, ReadRefresh, func(work context.Context) (any, error) {
		session, base, err := m.openChiliNative(work, account)
		if err != nil {
			return nil, err
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		out := SocialSnapshot{SettingSnapshot: base, Group: group, Friends: []nativeauth.Friend{},
			Applications: []nativeauth.FriendApplication{}, MemberIDs: []string{}}
		if group == "" || includeApplications {
			seen := map[string]bool{}
			for page := 1; page <= 100; page++ {
				rows, more, err := client.FriendRequests(work, session, page)
				if err != nil {
					return nil, err
				}
				for _, row := range rows {
					key := row.From + ":" + strconv.FormatInt(row.Created, 10)
					if seen[key] {
						return nil, ErrMemberScope
					}
					seen[key] = true
					state := map[int]string{-1: "FRIENDLOG_STATE_REFUSE", 0: "FRIENDLOG_STATE_PROCESSING", 1: "FRIENDLOG_STATE_AGREE"}[*row.Result]
					out.Applications = append(out.Applications, nativeauth.FriendApplication{ID: strconv.FormatInt(row.Created, 10),
						User: nativeauth.Friend{ID: row.From, Name: row.Name}, Direction: "FROM_HIM", State: state})
				}
				if !more {
					break
				}
				if page == 100 {
					return nil, ErrMemberScope
				}
			}
		}
		if group != "" {
			for page := 1; page <= 100; page++ {
				rows, more, err := client.Groups(work, session, page)
				if err != nil {
					return nil, err
				}
				for _, row := range rows {
					if row.ID == group {
						out.Present = true
					}
				}
				if !more {
					out.DirectoryKnown = true
					break
				}
			}
			if !out.DirectoryKnown {
				return nil, ErrMemberScope
			}
			if out.Present {
				seen := map[string]bool{}
				for page := 1; page <= 100; page++ {
					rows, more, err := client.Members(work, session, group, page)
					if err != nil {
						return nil, err
					}
					for _, row := range rows {
						if seen[row.UserID] {
							return nil, ErrMemberScope
						}
						seen[row.UserID] = true
						out.MemberIDs = append(out.MemberIDs, row.UserID)
						if row.UserID == out.SelfID {
							out.SelfRole = map[int]string{20: "member", 60: "admin", 100: "owner"}[row.Role]
						}
					}
					if !more {
						out.MembersKnown = true
						break
					}
				}
				if !out.MembersKnown || out.SelfRole == "" {
					return nil, ErrMemberScope
				}
			} else {
				seen := map[string]bool{}
				for page := 1; page <= 100; page++ {
					rows, more, err := client.SelfInvitations(work, session, group, page)
					if err != nil {
						return nil, err
					}
					for _, row := range rows {
						id := row.ID()
						if seen[id] {
							return nil, ErrMemberScope
						}
						seen[id] = true
						if row.Result == 0 {
							at := row.Time()
							out.Invitations = append(out.Invitations, nativeauth.InvitationRecord{
								ID: id, Group: group, Type: "INVITATION", State: "MEMBER_STATE_INVITED",
								Applicant: nativeauth.Friend{ID: row.User, Name: row.Name},
								Inviter:   &nativeauth.Friend{ID: row.Inviter}, At: &at,
							})
						}
					}
					if !more {
						out.InvitesKnown = true
						break
					}
				}
				if !out.InvitesKnown {
					return nil, ErrMemberScope
				}
			}
		}
		seenFriends := map[string]bool{}
		for page := 1; (group == "" || out.Present) && page <= 100; page++ {
			rows, more, err := client.Friends(work, session, page)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				if seenFriends[row.ID] {
					return nil, ErrMemberScope
				}
				seenFriends[row.ID] = true
				out.Friends = append(out.Friends, nativeauth.Friend{ID: row.ID, Name: row.Name})
			}
			if !more {
				out.FriendsKnown = true
				break
			}
		}
		if (group == "" || out.Present) && !out.FriendsKnown {
			return nil, ErrMemberScope
		}
		sort.Strings(out.MemberIDs)
		sort.Slice(out.Friends, func(i, j int) bool { return out.Friends[i].ID < out.Friends[j].ID })
		sort.Slice(out.Applications, func(i, j int) bool {
			a, b := out.Applications[i], out.Applications[j]
			if a.User.ID == b.User.ID {
				return a.ID < b.ID
			}
			return a.User.ID < b.User.ID
		})
		sort.Slice(out.Invitations, func(i, j int) bool { return out.Invitations[i].ID < out.Invitations[j].ID })
		out.Digest = nativeauth.SettingsDigest(struct {
			Group, Self, Role     string
			Present, InvitesKnown bool
			Members               []string
			Friends               []nativeauth.Friend
			Applications          []nativeauth.FriendApplication
			Invitations           []nativeauth.InvitationRecord
		}{group, out.SelfID, out.SelfRole, out.Present, out.InvitesKnown,
			out.MemberIDs, out.Friends, out.Applications, out.Invitations})
		if err := m.checkSettingSession(work, account, out.SettingSnapshot); err != nil {
			return nil, err
		}
		if change != nil {
			if fence == nil {
				return nil, ErrMemberScope
			}
			if _, err := out.ValidateChange(*change); err != nil {
				return nil, err
			}
			err = client.Execute(work, session, chiliChange(*change), func(write context.Context, submit func(context.Context) error) error {
				if time.Since(out.SampledAt) > 5*time.Second {
					return ErrMemberScope
				}
				return fence(write, out, submit)
			})
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	})
	if err != nil {
		return SocialSnapshot{}, err
	}
	out, ok := value.(SocialSnapshot)
	if !ok {
		return out, ErrMemberScope
	}
	out.Friends = append([]nativeauth.Friend{}, out.Friends...)
	out.Applications = append([]nativeauth.FriendApplication{}, out.Applications...)
	out.MemberIDs = append([]string{}, out.MemberIDs...)
	out.Invitations = cloneInvitations(out.Invitations)
	return out, nil
}
