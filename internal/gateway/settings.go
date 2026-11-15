package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type SettingSnapshot struct {
	Platform                string                      `json:"platform,omitempty"`
	Settings                *nativeauth.AccountSettings `json:"settings,omitempty"`
	Member                  *nativeauth.GroupMember     `json:"member,omitempty"`
	SelfID                  string                      `json:"self_id"`
	SelfRole                string                      `json:"self_role,omitempty"`
	NicknameLocked          bool                        `json:"nickname_locked"`
	ActionsEnabled          bool                        `json:"native_actions_enabled"`
	Digest                  string                      `json:"digest"`
	AccountEpoch            int64                       `json:"account_epoch"`
	AccountVersion          int64                       `json:"account_version"`
	DeploymentVersion       int64                       `json:"deployment_version"`
	MobileDeploymentVersion int64                       `json:"-"`
	MobileSessionVersion    int64                       `json:"-"`
	SampledAt               time.Time                   `json:"sampled_at"`
	CredentialDigest        [32]byte                    `json:"-"`
	ReceiverOwner           string                      `json:"-"`
	ReceiverRevision        int64                       `json:"-"`
	AvatarResource          string                      `json:"-"`
	CaptchaID               string                      `json:"captcha_id,omitempty"`
	controlledPeer          *onboardingPeerSnapshot
}

type SettingsFence func(context.Context, SettingSnapshot, func(context.Context) error) error

type settingsClient interface {
	Settings(context.Context) (nativeauth.AccountSettings, error)
	Directory(context.Context, uint64) ([]nativeauth.DirectoryGroup, error)
	Members(context.Context, string) (nativeauth.MemberDirectory, error)
	NicknameLocked(context.Context, string) (bool, error)
	ChangeSetting(context.Context, nativeauth.SettingChange, nativewire.SubmissionFence) error
	Close()
}

func (m *ReceiverManager) Settings(ctx context.Context, account, group, target string) (SettingSnapshot, error) {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return SettingSnapshot{}, err
	} else if chili {
		return m.chiliSettings(ctx, account, group, target, "", nil, nil)
	}
	return m.settings(ctx, account, group, target, "", nil, nil, defaultSettingsClient)
}

func (m *ReceiverManager) SettingsForReconciliation(ctx context.Context, account string, change nativeauth.SettingChange) (SettingSnapshot, error) {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return SettingSnapshot{}, err
	} else if chili {
		return m.chiliSettings(ctx, account, change.Group, change.Target, "history:"+change.Kind, nil, nil)
	}
	return m.Settings(ctx, account, change.Group, change.Target)
}

func (m *ReceiverManager) ChangeSetting(ctx context.Context, account, operation string, change nativeauth.SettingChange, fence SettingsFence) error {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return err
	} else if chili {
		_, err = m.chiliSettings(ctx, account, change.Group, change.Target, operation, &change, fence)
		return err
	}
	if err := change.Validate(); err != nil {
		return err
	}
	_, err := m.settings(ctx, account, change.Group, change.Target, operation, &change, fence, defaultSettingsClient)
	return err
}

func defaultSettingsClient(d nativeauth.Deployment, saved nativeauth.Session, ids nativeauth.RequestIDSource) (settingsClient, error) {
	return nativeauth.Resume(d, saved, ids)
}

func (m *ReceiverManager) ChangeSecurity(ctx context.Context, account, operation, kind string, in nativeauth.SecurityInput, state *nativeauth.SecurityState, fence SettingsFence) error {
	if chili, err := m.chiliAccount(ctx, account); err != nil {
		return err
	} else if chili {
		if kind == "avatar" {
			if err := in.Validate(kind); err != nil {
				return err
			}
			data, err := nativeauth.NormalizeAvatar(in.Image)
			if err != nil {
				return err
			}
			defer clear(data)
			_, err = m.chiliSettings(ctx, account, "", "", operation, nil, nil,
				func(work context.Context, client *lajiaoliao.Client, session lajiaoliao.Session, snapshot SettingSnapshot) error {
					if state == nil || fence == nil {
						return ErrMemberScope
					}
					return client.ChangeAvatar(work, session, operation, data,
						func(ctx context.Context, send func(context.Context) error) error { return fence(ctx, snapshot, send) },
						func(url string) { state.Asset = url })
				})
			return err
		}
		if len(in.Image) != 0 || in.OldPassword != "" {
			return ErrMemberScope
		}
		change := lajiaoliao.SecurityChange{Flow: in.Flow, Captcha: in.Captcha, Code: in.Code, Phone: in.Phone, Password: in.NewPassword}
		if err := change.Validate(kind); err != nil {
			return err
		}
		_, err := m.chiliSettings(ctx, account, "", "", operation, nil, nil,
			func(work context.Context, client *lajiaoliao.Client, session lajiaoliao.Session, snapshot SettingSnapshot) error {
				if state == nil || fence == nil {
					return ErrMemberScope
				}
				saved := lajiaoliao.SecurityState{Phone: state.Phone, Area: state.AreaCode}
				err := client.ChangeSecurity(work, session, kind, change, &saved,
					func(ctx context.Context, send func(context.Context) error) error { return fence(ctx, snapshot, send) })
				if err == nil {
					state.Phone, state.AreaCode = saved.Phone, saved.Area
				}
				return err
			})
		return err
	}
	if err := in.Validate(kind); err != nil {
		return err
	}
	_, err := m.settings(ctx, account, "", "", operation, nil, nil, defaultSettingsClient,
		func(work context.Context, client settingsClient, snapshot SettingSnapshot) error {
			security, ok := client.(interface {
				ChangeSecurity(context.Context, string, nativeauth.SecurityInput, *nativeauth.SecurityState, nativewire.SubmissionFence) error
			})
			if !ok || fence == nil {
				return errors.New("native_security_unavailable")
			}
			return security.ChangeSecurity(work, kind, in, state, func(ctx context.Context, submit func(context.Context) error) error {
				return fence(ctx, snapshot, submit)
			})
		})
	return err
}

func (m *ReceiverManager) settings(ctx context.Context, account, group, target, operation string,
	change *nativeauth.SettingChange, fence SettingsFence,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error),
	action ...func(context.Context, settingsClient, SettingSnapshot) error) (SettingSnapshot, error) {
	key := fmt.Sprintf("settings:%s:%s:%s", group, target, operation)
	value, err := m.readValue(ctx, account, key, ReadRefresh, func(work context.Context) (any, error) {
		client, saved, out, err := m.openSettingsClient(work, account, factory)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		if group == "" {
			settings, err := client.Settings(work)
			if err != nil {
				return nil, err
			}
			if settings.UID != out.SelfID {
				return nil, ErrMemberScope
			}
			out.Settings = &settings
			out.Digest = nativeauth.SettingsDigest(settings)
		} else {
			groups, err := client.Directory(work, saved.Session.UID)
			if err != nil {
				return nil, err
			}
			mapped := false
			for _, row := range groups {
				if row.ID == group && row.Team != "" {
					mapped = true
				}
			}
			if !mapped {
				return nil, ErrMemberDirectory
			}
			roster, err := client.Members(work, group)
			if err != nil {
				return nil, err
			}
			if !roster.Complete || roster.Group != group {
				return nil, ErrMemberScope
			}
			for _, member := range roster.Members {
				if member.ID == out.SelfID && member.Transport == saved.Session.NIMID {
					out.SelfRole = member.Role
				}
				if member.ID == target {
					copy := member
					out.Member = &copy
				}
			}
			if out.Member == nil || out.Member.Transport == "" || out.SelfRole == "" || out.SelfRole == "unknown" {
				return nil, ErrMemberScope
			}
			out.NicknameLocked, err = client.NicknameLocked(work, group)
			if err != nil {
				return nil, err
			}
			out.Digest = nativeauth.SettingsDigest(out.Member)
		}
		if err := m.checkSettingSession(work, account, out); err != nil {
			return nil, err
		}
		if change != nil {
			if fence == nil || time.Since(out.SampledAt) > 5*time.Second {
				return nil, errors.New("native_setting_evidence_expired")
			}
			err := client.ChangeSetting(work, *change, func(ctx context.Context, submit func(context.Context) error) error {
				return fence(ctx, out, submit)
			})
			if err != nil {
				return nil, err
			}
		}
		if len(action) > 0 {
			if time.Since(out.SampledAt) > 5*time.Second {
				return nil, errors.New("native_setting_evidence_expired")
			}
			if err := action[0](work, client, out); err != nil {
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
		return SettingSnapshot{}, ErrMemberScope
	}
	// A coalesced read never shares mutable projection pointers with callers.
	if out.Settings != nil {
		copy := *out.Settings
		clone := func(value *bool) *bool {
			if value == nil {
				return nil
			}
			next := *value
			return &next
		}
		copy.SearchableAccount = clone(copy.SearchableAccount)
		copy.SearchableMobile = clone(copy.SearchableMobile)
		copy.AudioVideo = clone(copy.AudioVideo)
		out.Settings = &copy
	}
	if out.Member != nil {
		copy := *out.Member
		out.Member = &copy
	}
	return out, nil
}

// Profile and social reads share the same sealed-session validation and account
// scheduler. Callers keep this client within a single readValue callback.
func (m *ReceiverManager) openSettingsClient(ctx context.Context, account string,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error),
) (settingsClient, savedNativeSession, SettingSnapshot, error) {
	var credential, deployment []byte
	var out SettingSnapshot
	var saved savedNativeSession
	err := m.DB.QueryRow(ctx, `SELECT a.credential_sealed,a.epoch,a.version,d.sealed,d.version,a.connection_enabled
        FROM accounts a CROSS JOIN native_deployment d WHERE a.id=$1 AND a.platform='wsl'
        AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL`,
		account).Scan(&credential, &out.AccountEpoch, &out.AccountVersion, &deployment, &out.DeploymentVersion, &out.ActionsEnabled)
	if err != nil {
		return nil, saved, out, ErrMemberScope
	}
	open := func(aad string, sealed []byte, value any) error {
		plain, err := m.Vault.Open(aad, sealed)
		if err != nil {
			return err
		}
		defer clear(plain)
		return json.Unmarshal(plain, value)
	}
	var d nativeauth.Deployment
	if open("native-deployment:v1", deployment, &d) != nil || open("native-session:"+account, credential, &saved) != nil ||
		saved.DeploymentVersion != out.DeploymentVersion || m.IDs == nil {
		return nil, saved, out, ErrMemberScope
	}
	client, err := factory(d, saved.Session, m.IDs(out.DeploymentVersion))
	out.CredentialDigest = sha256.Sum256(credential)
	out.CaptchaID = d.CaptchaID
	out.SelfID = strconv.FormatUint(saved.Session.UID, 10)
	out.SampledAt = time.Now().UTC()
	return client, saved, out, err
}

func (m *ReceiverManager) checkSettingSession(ctx context.Context, account string, snapshot SettingSnapshot) error {
	var current []byte
	if err := m.DB.QueryRow(ctx, `SELECT credential_sealed FROM accounts
        WHERE id=$1 AND epoch=$2 AND version=$3 AND auth_state='authenticated'
        AND (platform='lajiaoliao' AND $4=0 OR platform='wsl' AND EXISTS(SELECT 1 FROM native_deployment WHERE version=$4))`,
		account, snapshot.AccountEpoch, snapshot.AccountVersion, snapshot.DeploymentVersion).Scan(&current); err != nil ||
		sha256.Sum256(current) != snapshot.CredentialDigest {
		return ErrMemberScope
	}
	return nil
}
