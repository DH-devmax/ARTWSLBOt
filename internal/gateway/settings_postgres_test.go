package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

type syntheticSettingsClient struct {
	before func()
	writes int
}

type syntheticSocialClient struct{ *syntheticSettingsClient }
type syntheticPendingInvitationClient struct {
	*syntheticSocialClient
	reads int
}

func (*syntheticPendingInvitationClient) Directory(context.Context, uint64) ([]nativeauth.DirectoryGroup, error) {
	return []nativeauth.DirectoryGroup{}, nil
}
func (f *syntheticPendingInvitationClient) InvitationRecords(context.Context) ([]nativeauth.InvitationRecord, string, error) {
	f.reads++
	return []nativeauth.InvitationRecord{
		{ID: "11", Group: "123", Type: "INVITATION", State: "MEMBER_STATE_INVITED",
			Applicant: nativeauth.Friend{ID: "7"}, Inviter: &nativeauth.Friend{ID: "8", Name: "inviter"}},
		{ID: "12", Group: "999", Type: "INVITATION", State: "MEMBER_STATE_INVITED", Applicant: nativeauth.Friend{ID: "7"}},
		{ID: "13", Group: "123", Type: "INVITATION", State: "MEMBER_STATE_INVITED", Applicant: nativeauth.Friend{ID: "9"}},
		{ID: "14", Group: "123", Type: "INVITATION", State: "MEMBER_STATE_GOOD", Applicant: nativeauth.Friend{ID: "7"}},
	}, "14", nil
}

type syntheticModerationClient struct{ *syntheticSettingsClient }

func (*syntheticModerationClient) GroupPolicy(context.Context, string) (nativeauth.GroupPolicy, error) {
	return nativeauth.GroupPolicy{Group: "123"}, nil
}
func (*syntheticModerationClient) GroupNotices(context.Context, string) ([]nativeauth.GroupNotice, error) {
	return []nativeauth.GroupNotice{}, nil
}
func (f *syntheticModerationClient) ChangeModeration(ctx context.Context, _ nativeauth.SettingChange, fence nativewire.SubmissionFence) error {
	return fence(ctx, func(context.Context) error { f.writes++; return nil })
}

type syntheticInvitationClient struct {
	*syntheticSettingsClient
	reads int
}

func (f *syntheticInvitationClient) InvitationRecords(context.Context) ([]nativeauth.InvitationRecord, string, error) {
	f.reads++
	return []nativeauth.InvitationRecord{{ID: "11", Group: "123", Type: "INVITATION",
		State: "MEMBER_STATE_GOOD", Applicant: nativeauth.Friend{ID: "8"}},
		{ID: "12", Group: "999", Type: "INVITATION", Applicant: nativeauth.Friend{ID: "9"}}}, "12", nil
}

func (*syntheticSocialClient) FriendApplications(context.Context) ([]nativeauth.FriendApplication, error) {
	return []nativeauth.FriendApplication{{ID: "9007199254740993", User: nativeauth.Friend{ID: "8", Name: "peer"},
		Direction: "FROM_HIM", State: "FRIENDLOG_STATE_PROCESSING"}}, nil
}
func (*syntheticSocialClient) Friends(context.Context) ([]nativeauth.Friend, error) {
	return []nativeauth.Friend{{ID: "8", Name: "peer"}}, nil
}
func (f *syntheticSocialClient) ChangeSocial(ctx context.Context, _ nativeauth.SettingChange, _ *nativeauth.FriendApplication, fence nativewire.SubmissionFence) error {
	return fence(ctx, func(context.Context) error { f.writes++; return nil })
}

func (f *syntheticSettingsClient) Settings(context.Context) (nativeauth.AccountSettings, error) {
	if f.before != nil {
		f.before()
	}
	return nativeauth.AccountSettings{UID: "7", Nickname: "synthetic-settings"}, nil
}
func (*syntheticSettingsClient) Directory(context.Context, uint64) ([]nativeauth.DirectoryGroup, error) {
	return []nativeauth.DirectoryGroup{{ID: "123", Team: "456"}}, nil
}
func (*syntheticSettingsClient) Members(context.Context, string) (nativeauth.MemberDirectory, error) {
	return nativeauth.MemberDirectory{Group: "123", Complete: true, Members: []nativeauth.GroupMember{
		{ID: "7", Transport: "synthetic-self", Role: "admin", Card: "self-card"},
	}}, nil
}
func (*syntheticSettingsClient) NicknameLocked(context.Context, string) (bool, error) {
	return false, nil
}
func (f *syntheticSettingsClient) ChangeSetting(ctx context.Context, _ nativeauth.SettingChange, fence nativewire.SubmissionFence) error {
	return fence(ctx, func(context.Context) error { f.writes++; return nil })
}
func (*syntheticSettingsClient) Close() {}

func TestPostgresSettingsCurrentSessionScopeAndNoRoleGrant(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)))
	scheduler := NewReadScheduler(ctx)
	defer scheduler.Close()
	manager := NewReceiverManager(db, vault, func(int64) nativeauth.RequestIDSource {
		return func(_ context.Context, now uint64) (uint64, error) { return now, nil }
	})
	manager.reads = scheduler
	var old []byte
	var version int64
	oldErr := db.QueryRow(ctx, `SELECT sealed,version FROM native_deployment`).Scan(&old, &version)
	defer func() {
		if oldErr == nil {
			db.Exec(ctx, `UPDATE native_deployment SET sealed=$1,version=$2`, old, version)
		} else {
			db.Exec(ctx, `DELETE FROM native_deployment`)
		}
	}()
	seal := func(aad string, value any) []byte {
		t.Helper()
		body, _ := json.Marshal(value)
		out, err := vault.Seal(aad, body)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state) VALUES('synthetic-settings-reader','authenticated') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	session := savedNativeSession{DeploymentVersion: 991, Session: nativeauth.Session{UID: 7, NIMID: "synthetic-self"}}
	if _, err := db.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, seal("native-session:"+account, session)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,991)
        ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=excluded.version`, seal("native-deployment:v1", nativeauth.Deployment{})); err != nil {
		t.Fatal(err)
	}
	fake := &syntheticSettingsClient{}
	factory := func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error) {
		return fake, nil
	}
	view, err := manager.settings(ctx, account, "", "", "", nil, nil, factory)
	if err != nil || view.Settings == nil || view.Settings.UID != "7" || view.AccountEpoch != 1 || len(view.Digest) != 64 {
		t.Fatal("safe settings projection", view, err)
	}
	group, err := manager.settings(ctx, account, "123", "7", "", nil, nil, factory)
	if err != nil || group.Member == nil || group.Member.Card != "self-card" || group.SelfID != "7" || group.SelfRole != "admin" {
		t.Fatal("self group nickname scope", group, err)
	}
	var grants int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM account_groups WHERE account_id=$1`, account).Scan(&grants); err != nil || grants != 0 {
		t.Fatal("settings observation granted group ownership", err)
	}
	change := nativeauth.SettingChange{Kind: "nickname", Text: "synthetic-new"}
	fence := func(work context.Context, current SettingSnapshot, submit func(context.Context) error) error {
		if current.Digest != view.Digest || current.CredentialDigest != view.CredentialDigest {
			t.Fatal("wrong session/settings evidence reached control")
		}
		return submit(work)
	}
	if _, err := manager.settings(ctx, account, "", "", "synthetic-operation", &change, fence, factory); err != nil || fake.writes != 1 {
		t.Fatal("settings did not use exactly one account lane", err)
	}
	socialFactory := func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error) {
		return &syntheticSocialClient{fake}, nil
	}
	social, err := manager.social(ctx, account, "123", "", nil, nil, socialFactory)
	if err != nil || !social.Present || social.SelfRole != "admin" || len(social.Friends) != 1 || social.Settings != nil || len(social.Applications) != 0 {
		t.Fatal("social safe projection or fresh group role", social, err)
	}
	applicationView, err := manager.social(ctx, account, "123", "", nil, nil, socialFactory, true)
	if err != nil || !applicationView.Present || len(applicationView.Applications) != 1 || applicationView.Group != "123" {
		t.Fatal("application-specific read lost the group or application", applicationView, err)
	}
	approval := nativeauth.SettingChange{Kind: "friend_approve", Group: "123", Target: "8", Text: "9007199254740993"}
	if _, err := applicationView.ValidateChange(approval); err != nil {
		t.Fatal("combined approval read unusable", err)
	}
	invite := nativeauth.SettingChange{Kind: "group_invite", Group: "123", Target: "8"}
	if _, err := manager.social(ctx, account, "123", "invite-operation", &invite,
		func(work context.Context, current SocialSnapshot, submit func(context.Context) error) error {
			if current.Digest != social.Digest || current.CredentialDigest != social.CredentialDigest {
				t.Fatal("social action changed the reviewed group/account")
			}
			return submit(work)
		}, socialFactory); err != nil || fake.writes != 2 {
		t.Fatal("social action bypassed the account lane or failed to submit", err)
	}
	moderationFactory := func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error) {
		return &syntheticModerationClient{fake}, nil
	}
	moderation, err := manager.moderation(ctx, account, "123", "", nil, nil, moderationFactory)
	if err != nil || moderation.SelfRole != "admin" || moderation.MutesKnown {
		t.Fatal("moderation invented missing owner mute evidence", moderation, err)
	}
	mute := nativeauth.SettingChange{Kind: "mute_group", Group: "123"}
	if _, err := manager.moderation(ctx, account, "123", "mute-test", &mute,
		func(work context.Context, proof ModerationSnapshot, submit func(context.Context) error) error {
			if proof.Digest != moderation.Digest {
				t.Fatal("moderation changed reviewed evidence")
			}
			return submit(work)
		}, moderationFactory); err != nil || fake.writes != 3 {
		t.Fatal("moderation lost final fence", err)
	}
	reader := &syntheticInvitationClient{syntheticSettingsClient: fake}
	invitationFactory := func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error) {
		return reader, nil
	}
	first, err := manager.invitationRecords(ctx, account, "123", invitationFactory)
	if err != nil || len(first.Records) != 1 || first.Complete || first.Records[0].Group != "123" {
		t.Fatal("invitation account history leaked other groups or claimed complete", first, err)
	}
	for range 3 {
		if _, err := manager.invitationRecords(ctx, account, "123", invitationFactory); err != nil {
			t.Fatal(err)
		}
	}
	if reader.reads != 1 {
		t.Fatal("opening group views rescanned the account", reader.reads)
	}
	if _, err := manager.invitationRecords(ctx, account, "999", invitationFactory); err == nil || reader.reads != 1 {
		t.Fatal("unmapped group history became readable", err)
	}
	pending := &syntheticPendingInvitationClient{syntheticSocialClient: &syntheticSocialClient{fake}}
	pendingFactory := func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (settingsClient, error) {
		return pending, nil
	}
	accept := nativeauth.SettingChange{Kind: "group_accept", Group: "123", Target: "7", Text: "11"}
	own, err := manager.social(ctx, account, "123", "", nil, nil, pendingFactory)
	if err != nil || own.Present || !own.DirectoryKnown || !own.InvitesKnown ||
		own.MembersKnown || len(own.Invitations) != 1 || own.Invitations[0].ID != "11" || own.SelfRole != "" {
		t.Fatal("pending self invitation leaked unrelated history or invented membership", own, err)
	}
	if _, err := manager.social(ctx, account, "123", "self-accept", &accept,
		func(work context.Context, proof SocialSnapshot, submit func(context.Context) error) error {
			if proof.Digest != own.Digest || proof.SelfID != "7" || proof.Present || pending.reads != 2 {
				t.Fatal("acceptance used cached or changed invitation evidence", proof)
			}
			return submit(work)
		}, pendingFactory); err != nil || fake.writes != 4 {
		t.Fatal("self acceptance did not pass fresh evidence and account lane", err)
	}
	fake.before = func() { db.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account) }
	if _, err := manager.social(ctx, account, "123", "stale-invitation", &accept, nil, pendingFactory); err == nil || fake.writes != 4 {
		t.Fatal("stale invitation session escaped scope fence", err)
	}
	if _, err := manager.social(ctx, account, "", "stale-social", nil, nil, socialFactory); err == nil || fake.writes != 4 {
		t.Fatal("stale social account response escaped scope fence", err)
	}
	if _, err := manager.settings(ctx, account, "", "", "stale-operation", &change, fence, factory); err == nil || fake.writes != 4 {
		t.Fatal("stale native session escaped the gateway fence")
	}
}
