package gateway

import (
	"context"
	"slices"
	"sort"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type SocialSnapshot struct {
	SettingSnapshot
	Group          string                         `json:"group_id"`
	Applications   []nativeauth.FriendApplication `json:"applications"`
	Friends        []nativeauth.Friend            `json:"friends"`
	Present        bool                           `json:"in_group"`
	MemberIDs      []string                       `json:"member_ids"`
	DirectoryKnown bool                           `json:"directory_known"`
	MembersKnown   bool                           `json:"members_known"`
	FriendsKnown   bool                           `json:"friends_known"`
	Invitations    []nativeauth.InvitationRecord  `json:"invitations,omitempty"`
	InvitesKnown   bool                           `json:"invitations_known"`
}

type socialClient interface {
	settingsClient
	FriendApplications(context.Context) ([]nativeauth.FriendApplication, error)
	Friends(context.Context) ([]nativeauth.Friend, error)
	ChangeSocial(context.Context, nativeauth.SettingChange, *nativeauth.FriendApplication, nativewire.SubmissionFence) error
}

type SocialFence func(context.Context, SocialSnapshot, func(context.Context) error) error

func (m *ReceiverManager) Social(ctx context.Context, account, group string) (SocialSnapshot, error) {
	return m.readSocial(ctx, account, group, false)
}

// SocialApplication includes incoming applications for a controlled group read.
// Other group reads do not acquire this extra account-wide projection.
func (m *ReceiverManager) SocialApplication(ctx context.Context, account, group string) (SocialSnapshot, error) {
	return m.readSocial(ctx, account, group, true)
}

func (m *ReceiverManager) readSocial(ctx context.Context, account, group string, applications bool) (SocialSnapshot, error) {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return SocialSnapshot{}, err
	} else if chili {
		return m.chiliSocial(ctx, account, group, "", nil, nil, applications)
	}
	return m.social(ctx, account, group, "", nil, nil, defaultSettingsClient, applications)
}

func (m *ReceiverManager) ChangeSocial(ctx context.Context, account, operation string, change nativeauth.SettingChange, fence SocialFence) error {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return err
	} else if chili {
		if !nativeauth.SocialKind(change.Kind) || chiliChange(change).Validate() != nil || fence == nil {
			return ErrMemberScope
		}
		_, err = m.chiliSocial(ctx, account, change.Group, operation, &change, fence)
		return err
	}
	if !nativeauth.SocialKind(change.Kind) || change.Validate() != nil || fence == nil {
		return &nativeauth.Error{Code: "native_social_request_invalid"}
	}
	_, err := m.social(ctx, account, change.Group, operation, &change, fence, defaultSettingsClient)
	return err
}

func (m *ReceiverManager) social(ctx context.Context, account, group, operation string, change *nativeauth.SettingChange, fence SocialFence,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error),
	applications ...bool,
) (SocialSnapshot, error) {
	if group != "" && (nativeauth.SettingChange{Kind: "group_leave", Group: group}).Validate() != nil {
		return SocialSnapshot{}, &nativeauth.Error{Code: "native_social_request_invalid"}
	}
	includeApplications := len(applications) > 0 && applications[0] || change != nil && (change.Kind == "friend_approve" || change.Kind == "friend_reject")
	readKey := "social:"
	if includeApplications {
		readKey = "social-applications:"
	}
	value, err := m.readValue(ctx, account, readKey+group+":"+operation, ReadRefresh, func(work context.Context) (any, error) {
		client, saved, base, err := m.openSettingsClient(work, account, factory)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		social, ok := client.(socialClient)
		if !ok {
			return nil, &nativeauth.Error{Code: "native_social_unavailable"}
		}
		out := SocialSnapshot{SettingSnapshot: base, Group: group, Applications: []nativeauth.FriendApplication{},
			Friends: []nativeauth.Friend{}, MemberIDs: []string{}}
		settings, err := client.Settings(work)
		if err != nil {
			return nil, err
		}
		if settings.UID != out.SelfID {
			return nil, ErrMemberScope
		}
		if group == "" || includeApplications {
			out.Applications, err = social.FriendApplications(work)
			if err != nil {
				return nil, err
			}
		}
		if group != "" {
			var groups []nativeauth.DirectoryGroup
			groups, err = client.Directory(work, saved.Session.UID)
			if err == nil && groups == nil {
				err = ErrMemberScope
			}
			if err == nil {
				out.DirectoryKnown = true
				for _, g := range groups {
					if g.ID == group && g.Team != "" {
						out.Present = true
					}
				}
				if out.Present {
					var roster nativeauth.MemberDirectory
					roster, err = client.Members(work, group)
					if err == nil && (!roster.Complete || roster.Group != group) {
						err = ErrMemberScope
					}
					for _, member := range roster.Members {
						out.MemberIDs = append(out.MemberIDs, member.ID)
						if member.ID == out.SelfID && member.Transport == saved.Session.NIMID {
							out.SelfRole = member.Role
						}
					}
					if err == nil && (out.SelfRole == "" || out.SelfRole == "unknown") {
						err = ErrMemberScope
					}
					out.MembersKnown = err == nil
				}
			}
			if err == nil && out.Present {
				out.Friends, err = social.Friends(work)
				out.FriendsKnown = err == nil && out.Friends != nil
			}
			if err == nil && !out.Present {
				if reader, ok := client.(interface {
					InvitationRecords(context.Context) ([]nativeauth.InvitationRecord, string, error)
				}); ok {
					var records []nativeauth.InvitationRecord
					records, _, err = reader.InvitationRecords(work)
					out.InvitesKnown = err == nil && records != nil
					for _, record := range records {
						if record.Group == group && record.Applicant.ID == out.SelfID &&
							record.Type == "INVITATION" && record.State == "MEMBER_STATE_INVITED" {
							out.Invitations = append(out.Invitations, record)
						}
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		sort.Slice(out.Applications, func(i, j int) bool { return out.Applications[i].ID < out.Applications[j].ID })
		sort.Slice(out.Friends, func(i, j int) bool { return out.Friends[i].ID < out.Friends[j].ID })
		sort.Slice(out.Invitations, func(i, j int) bool { return out.Invitations[i].ID < out.Invitations[j].ID })
		sort.Strings(out.MemberIDs)
		privacy := make([][2]bool, 0, len(out.Applications))
		for _, row := range out.Applications {
			privacy = append(privacy, [2]bool{row.NotSeeMe, row.NotSeeHim})
		}
		out.Digest = nativeauth.SettingsDigest(struct {
			Self, Group, Role                          string
			Applications                               []nativeauth.FriendApplication
			Friends                                    []nativeauth.Friend
			Members                                    []string
			Present                                    bool
			DirectoryKnown, MembersKnown, FriendsKnown bool
			Privacy                                    [][2]bool
			Invitations                                []nativeauth.InvitationRecord
			InvitesKnown                               bool
		}{out.SelfID, out.Group, out.SelfRole, out.Applications, out.Friends, out.MemberIDs, out.Present,
			out.DirectoryKnown, out.MembersKnown, out.FriendsKnown, privacy, out.Invitations, out.InvitesKnown})
		if err := m.checkSettingSession(work, account, out.SettingSnapshot); err != nil {
			return nil, err
		}
		if change != nil {
			application, err := out.ValidateChange(*change)
			if err != nil {
				return nil, err
			}
			if time.Since(out.SampledAt) > 5*time.Second {
				return nil, &nativeauth.Error{Code: "native_setting_evidence_expired"}
			}
			if err := social.ChangeSocial(work, *change, application, func(ctx context.Context, submit func(context.Context) error) error {
				return fence(ctx, out, submit)
			}); err != nil {
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
		return SocialSnapshot{}, ErrMemberScope
	}
	out.Applications, out.Friends, out.MemberIDs = slices.Clone(out.Applications), slices.Clone(out.Friends), slices.Clone(out.MemberIDs)
	out.Invitations = cloneInvitations(out.Invitations)
	return out, nil
}

func cloneInvitations(records []nativeauth.InvitationRecord) []nativeauth.InvitationRecord {
	out := slices.Clone(records)
	for i := range out {
		if out[i].Inviter != nil {
			inviter := *out[i].Inviter
			out[i].Inviter = &inviter
		}
		if out[i].At != nil {
			at := *out[i].At
			out[i].At = &at
		}
	}
	return out
}

func (s SocialSnapshot) ValidateChange(change nativeauth.SettingChange) (*nativeauth.FriendApplication, error) {
	validation := change.Validate()
	if s.Platform == "lajiaoliao" {
		validation = chiliChange(change).Validate()
	}
	if !nativeauth.SocialKind(change.Kind) || validation != nil || change.Group != s.Group {
		return nil, &nativeauth.Error{Code: "native_social_request_invalid"}
	}
	switch change.Kind {
	case "friend_approve", "friend_reject":
		if s.Platform == "lajiaoliao" {
			pending := 0
			for _, row := range s.Applications {
				if row.User.ID == change.Target && row.Direction == "FROM_HIM" && row.State == "FRIENDLOG_STATE_PROCESSING" {
					pending++
				}
			}
			// Chili's response route addresses the applicant, not the log ID.
			if pending != 1 {
				return nil, &nativeauth.Error{Code: "native_social_application_changed"}
			}
		}
		for _, row := range s.Applications {
			if row.ID == change.Text && row.User.ID == change.Target && row.User.ID != s.SelfID &&
				row.Direction == "FROM_HIM" && row.State == "FRIENDLOG_STATE_PROCESSING" {
				return &row, nil
			}
		}
		return nil, &nativeauth.Error{Code: "native_social_application_changed"}
	case "friend_delete":
		if !s.DirectoryKnown || !s.MembersKnown || !s.FriendsKnown || !s.Present ||
			s.SelfRole != "owner" && s.SelfRole != "admin" {
			return nil, &nativeauth.Error{Code: "native_social_role_required"}
		}
		if change.Target == s.SelfID ||
			!slices.ContainsFunc(s.Friends, func(f nativeauth.Friend) bool { return f.ID == change.Target }) {
			return nil, &nativeauth.Error{Code: "native_social_friend_required"}
		}
	case "group_invite":
		if !s.DirectoryKnown || !s.MembersKnown || !s.FriendsKnown || !s.Present || s.SelfRole != "owner" && s.SelfRole != "admin" {
			return nil, &nativeauth.Error{Code: "native_social_role_required"}
		}
		if change.Target == s.SelfID || slices.Contains(s.MemberIDs, change.Target) ||
			!slices.ContainsFunc(s.Friends, func(f nativeauth.Friend) bool { return f.ID == change.Target }) {
			return nil, &nativeauth.Error{Code: "native_social_friend_required"}
		}
	case "group_leave":
		if !s.DirectoryKnown || !s.MembersKnown || !s.Present || s.SelfRole != "member" && s.SelfRole != "admin" {
			return nil, &nativeauth.Error{Code: "native_social_leave_owner_or_absent"}
		}
	case "group_accept":
		if !s.DirectoryKnown || s.Present || !s.InvitesKnown || change.Target != s.SelfID ||
			s.Platform == "lajiaoliao" && len(s.Invitations) != 1 ||
			!slices.ContainsFunc(s.Invitations, func(record nativeauth.InvitationRecord) bool {
				return record.ID == change.Text && record.Group == change.Group && record.Applicant.ID == s.SelfID &&
					record.Type == "INVITATION" && record.State == "MEMBER_STATE_INVITED"
			}) {
			return nil, &nativeauth.Error{Code: "native_invitation_self_required"}
		}
	}
	return nil, nil
}

func (s SocialSnapshot) Matches(change nativeauth.SettingChange) bool {
	switch change.Kind {
	case "friend_delete":
		return s.validMembershipChange(change) && change.Group == s.Group && change.Target != s.SelfID &&
			s.FriendsKnown && !slices.ContainsFunc(s.Friends, func(f nativeauth.Friend) bool { return f.ID == change.Target })
	case "friend_approve", "friend_reject":
		state := "FRIENDLOG_STATE_AGREE"
		if change.Kind == "friend_reject" {
			state = "FRIENDLOG_STATE_REFUSE"
		}
		return slices.ContainsFunc(s.Applications, func(f nativeauth.FriendApplication) bool {
			return f.ID == change.Text && f.User.ID == change.Target && f.Direction == "FROM_HIM" && f.State == state
		})
	case "group_invite":
		return s.validMembershipChange(change) && s.Group == change.Group && s.DirectoryKnown && s.MembersKnown &&
			s.Present && slices.Contains(s.MemberIDs, change.Target)
	case "group_leave":
		return s.validMembershipChange(change) && s.Group == change.Group && s.DirectoryKnown && !s.Present
	case "group_accept":
		return s.validMembershipChange(change) && change.Target == s.SelfID && s.Group == change.Group &&
			s.DirectoryKnown && s.MembersKnown && s.Present && slices.Contains(s.MemberIDs, s.SelfID)
	}
	return false
}

func (s SocialSnapshot) validMembershipChange(change nativeauth.SettingChange) bool {
	if s.Platform == "lajiaoliao" {
		return chiliChange(change).Validate() == nil
	}
	return change.Validate() == nil
}
