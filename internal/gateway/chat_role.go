package gateway

import (
	"context"
	"time"

	"wangshangliao/internal/lajiaoliao"
)

// ChatRole is a fresh identity read, outside the projection cache. Chili needs
// only its own member row; sending independently verifies mute and group state.
func (m *ReceiverManager) ChatRole(ctx context.Context, platform, account, group string) (MemberSnapshot, error) {
	if platform == "wsl" {
		out, err := m.Members(ctx, account, group)
		if err == nil && !out.Complete {
			return MemberSnapshot{}, ErrMemberScope
		}
		return out, err
	}
	if platform != "lajiaoliao" {
		return MemberSnapshot{}, ErrMemberScope
	}
	value, err := m.readValue(ctx, account, "chat-role:"+group, ReadRefresh, func(work context.Context) (any, error) {
		session, epoch, err := m.chiliDirectorySession(work, account)
		if err != nil {
			return nil, err
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		self, err := client.Member(work, session, group, session.UserID)
		if err != nil || self.UserID != session.UserID {
			return nil, ErrMemberScope
		}
		return MemberSnapshot{AccountEpoch: epoch, SampledAt: time.Now().UTC(), SelfID: session.UserID,
			SelfRole: map[int]string{20: "member", 60: "admin", 100: "owner"}[self.Role]}, nil
	})
	if err != nil {
		return MemberSnapshot{}, err
	}
	out, ok := value.(MemberSnapshot)
	if !ok {
		return out, ErrMemberScope
	}
	return out, nil
}
