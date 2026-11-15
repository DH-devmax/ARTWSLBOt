package gateway

import (
	"context"
	"strconv"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

// Received applications are not an invitation attribution/history API.
func (m *ReceiverManager) chiliInvitations(ctx context.Context, account, group string) (InvitationSnapshot, error) {
	if !lajiaoliao.ValidID(group) {
		return InvitationSnapshot{}, ErrMemberScope
	}
	value, err := m.readValue(ctx, account, "chili-applications:"+group, ReadRefresh, func(work context.Context) (any, error) {
		session, base, err := m.openChiliNative(work, account)
		if err != nil {
			return nil, err
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		self, err := client.Member(work, session, group, session.UserID)
		if err != nil {
			return nil, err
		}
		if self.Role != 60 && self.Role != 100 {
			return nil, ErrMemberScope
		}
		out := InvitationSnapshot{SettingSnapshot: base, Group: group, Records: []nativeauth.InvitationRecord{}}
		seen := map[string]bool{}
		for page := 1; page <= 100; page++ {
			rows, more, err := client.GroupRequests(work, session, group, page)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				id := row.User + ":" + strconv.FormatInt(row.Created, 10)
				if seen[id] {
					return nil, ErrMemberScope
				}
				seen[id] = true
				at := time.Unix(row.Created, 0).UTC()
				state := map[int]string{-1: "MEMBER_STATE_MANAGER_REJECT", 0: "MEMBER_STATE_APPLIED", 1: "MEMBER_STATE_GOOD"}[*row.Result]
				out.Records = append(out.Records, nativeauth.InvitationRecord{ID: id, Group: group,
					Type: "APPLY", State: state, Applicant: nativeauth.Friend{ID: row.User, Name: row.Name}, At: &at})
			}
			if !more {
				break
			}
			if page == 100 {
				return nil, ErrMemberDirectory
			}
		}
		if err := m.checkSettingSession(work, account, base); err != nil {
			return nil, err
		}
		return out, nil
	})
	if err != nil {
		return InvitationSnapshot{}, err
	}
	out, ok := value.(InvitationSnapshot)
	if !ok {
		return out, ErrMemberScope
	}
	out.Records = append([]nativeauth.InvitationRecord{}, out.Records...)
	for i := range out.Records {
		if out.Records[i].At != nil {
			copy := *out.Records[i].At
			out.Records[i].At = &copy
		}
	}
	return out, nil
}
