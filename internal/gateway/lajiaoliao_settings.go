package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func (m *ReceiverManager) chiliSettings(ctx context.Context, account, group, target, operation string,
	change *nativeauth.SettingChange, fence SettingsFence,
	actions ...func(context.Context, *lajiaoliao.Client, lajiaoliao.Session, SettingSnapshot) error) (SettingSnapshot, error) {
	value, err := m.readValue(ctx, account, "chili-settings:"+group+":"+target+":"+operation, ReadRefresh, func(work context.Context) (any, error) {
		session, out, err := m.openChiliNative(work, account)
		if err != nil {
			return nil, err
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		if group == "" {
			var profile lajiaoliao.Profile
			if strings.HasPrefix(operation, "history:") && operation != "history:nickname" && operation != "history:avatar" {
				profile, err = client.FullProfile(work, session)
			} else {
				profile, err = client.BasicProfile(work, session)
			}
			if err != nil {
				return nil, err
			}
			if profile.ID != out.SelfID {
				return nil, ErrMemberScope
			}
			out.AvatarResource = profile.Avatar
			out.Settings = &nativeauth.AccountSettings{UID: profile.ID, Nickname: profile.Name, Avatar: profile.Avatar,
				SearchableAccount: profile.SearchAccount, SearchableMobile: profile.SearchPhone, FriendPolicy: profile.FriendPolicy,
				GroupPolicy: profile.GroupPolicy, AudioVideo: profile.AudioVideo}
			out.Settings.MaskedMobile = profile.MaskedPhone
			out.Settings.ProfileDetails, _ = json.Marshal(profile.Details)
			if out.Settings.Avatar != "" {
				out.Settings.Avatar = "configured"
			}
			out.Digest = nativeauth.SettingsDigest(struct {
				Settings *nativeauth.AccountSettings
				Avatar   string
			}{out.Settings, out.AvatarResource})
		} else {
			self, err := client.Member(work, session, group, session.UserID)
			if err != nil {
				return nil, err
			}
			member, err := client.Member(work, session, group, target)
			if err != nil {
				return nil, err
			}
			out.SelfRole = map[int]string{20: "member", 60: "admin", 100: "owner"}[self.Role]
			out.Member = &nativeauth.GroupMember{ID: member.UserID, Transport: member.UserID, Name: member.Name, Card: member.Name,
				Role: map[int]string{20: "member", 60: "admin", 100: "owner"}[member.Role]}
			out.Digest = nativeauth.SettingsDigest(out.Member)
		}
		if err := m.checkSettingSession(work, account, out); err != nil {
			return nil, err
		}
		if change != nil {
			if fence == nil || chiliChange(*change).Validate() != nil {
				return nil, ErrMemberScope
			}
			submit := func(write context.Context, send func(context.Context) error) error {
				if time.Since(out.SampledAt) > 5*time.Second {
					return ErrMemberScope
				}
				return fence(write, out, send)
			}
			if group == "" {
				err = client.ChangeProfile(work, session, chiliChange(*change), submit)
			} else {
				err = client.Execute(work, session, chiliChange(*change), submit)
			}
			if err != nil {
				return nil, err
			}
		}
		for _, action := range actions {
			if err := action(work, client, session, out); err != nil {
				return nil, err
			}
		}
		return out, nil
	})
	if err != nil {
		return SettingSnapshot{}, err
	}
	out, ok := value.(SettingSnapshot)
	if !ok {
		return out, ErrMemberScope
	}
	if out.Member != nil {
		copy := *out.Member
		out.Member = &copy
	}
	if out.Settings != nil {
		copy := *out.Settings
		clone := func(v *bool) *bool {
			if v == nil {
				return nil
			}
			b := *v
			return &b
		}
		copy.SearchableAccount, copy.SearchableMobile, copy.AudioVideo = clone(copy.SearchableAccount), clone(copy.SearchableMobile), clone(copy.AudioVideo)
		copy.ProfileDetails = append(json.RawMessage(nil), copy.ProfileDetails...)
		out.Settings = &copy
	}
	return out, nil
}

func (m *ReceiverManager) ChiliSecurityChallenge(ctx context.Context, account string, purpose int) (lajiaoliao.Challenge, error) {
	value, err := m.readValue(ctx, account, "chili-security-challenge:"+string(rune('0'+purpose)), ReadRefresh, func(work context.Context) (any, error) {
		session, snapshot, err := m.openChiliNative(work, account)
		if err != nil {
			return nil, err
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		challenge, err := client.SecurityChallenge(work, session.DeviceID, purpose)
		if err != nil {
			return nil, err
		}
		if err := m.checkSettingSession(work, account, snapshot); err != nil {
			return nil, err
		}
		return challenge, nil
	})
	if err != nil {
		return lajiaoliao.Challenge{}, err
	}
	out, ok := value.(lajiaoliao.Challenge)
	if !ok {
		return out, ErrMemberScope
	}
	return out, nil
}
