package control

import (
	"strings"
	"testing"
	"time"
)

func TestGroupBindingTokenPurposeAndBounds(t *testing.T) {
	token := strings.Repeat("t", 43)
	digest := bindingTokenDigest(token)
	for _, tc := range []struct {
		name, text, prefix string
		want               bool
	}{
		{"group", "群绑定 " + token, "群绑定 ", true},
		{"private", "群接入 " + token, "群接入 ", true},
		{"wrong-purpose", "群接入 " + token, "群绑定 ", false},
		{"quote", "> 群绑定 " + token, "群绑定 ", false},
		{"whitespace", "群绑定 " + token + "\n", "群绑定 ", false},
		{"wrong-token", "群绑定 " + strings.Repeat("u", 43), "群绑定 ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bindingTokenMatches(digest, tc.text, tc.prefix); got != tc.want {
				t.Fatalf("match=%v, want %v", got, tc.want)
			}
		})
	}
	privateToken := strings.Repeat("p", 43)
	if bindingTokenMatches(bindingTokenDigest(privateToken), "群接入 "+token, "群接入 ") ||
		bindingTokenMatches(digest, "群绑定 "+privateToken, "群绑定 ") {
		t.Fatal("changing a prefix exchanged the independent private/group secrets")
	}
	m := OnboardingMessage{Group: "chili:group-01", Sender: "native-owner", Recipient: "chili:group-01",
		ClientID: "immutable-client", Text: "群绑定 " + token}
	if !validBindingMessage(m, "native-bot") {
		t.Fatal("valid authenticated source rejected")
	}
	for _, mutate := range []func(*OnboardingMessage){
		func(m *OnboardingMessage) { m.Sender = "native-bot" },
		func(m *OnboardingMessage) { m.Recipient = "other-bot" },
		func(m *OnboardingMessage) { m.ClientID = "" },
		func(m *OnboardingMessage) { m.Text = strings.Repeat("t", 161) },
		func(m *OnboardingMessage) { m.Text = string([]byte{0xff}) },
		func(m *OnboardingMessage) { m.Private = true },
	} {
		bad := m
		mutate(&bad)
		if validBindingMessage(bad, "native-bot") {
			t.Fatal("invalid source accepted")
		}
	}
	private := m
	private.Group, private.Recipient, private.Private = "", "native-bot", true
	if !validBindingMessage(private, "native-bot") {
		t.Fatal("direct incoming identity proof rejected")
	}
}

func TestGroupBindingRoleProofRequiresOriginalOwner(t *testing.T) {
	p := bindingProof{Group: "native-group", Sender: "original-owner", Owner: "original-owner", SenderRole: "owner", RobotRole: "admin"}
	r := GroupBindingRoleEvidence{NativeGroupID: p.Group, NativeSenderID: p.Sender, NativeOwnerID: p.Owner,
		SenderRole: p.SenderRole, RobotRole: p.RobotRole, Present: true}
	if !bindingRolesMatch(p, r) {
		t.Fatal("same verified owner rejected")
	}
	for _, mutate := range []func(*GroupBindingRoleEvidence){
		func(r *GroupBindingRoleEvidence) { r.NativeOwnerID = "new-owner" },
		func(r *GroupBindingRoleEvidence) { r.NativeOwnerID = "" },
		func(r *GroupBindingRoleEvidence) { r.NativeSenderID = "different-sender" },
		func(r *GroupBindingRoleEvidence) { r.SenderRole = "admin" },
		func(r *GroupBindingRoleEvidence) { r.RobotRole = "member" },
		func(r *GroupBindingRoleEvidence) { r.NativeGroupID = "other-platform-group" },
		func(r *GroupBindingRoleEvidence) { r.Present = false },
	} {
		bad := r
		mutate(&bad)
		if bindingRolesMatch(p, bad) {
			t.Fatal("changed ownership or role accepted")
		}
	}
	p.Sender, p.SenderRole = "native-admin", "admin"
	r.NativeSenderID, r.SenderRole = p.Sender, p.SenderRole
	if !bindingRolesMatch(p, r) {
		t.Fatal("native administrator proof should proceed to manual review")
	}
	p.Owner = p.Sender
	r.NativeOwnerID = p.Owner
	if bindingRolesMatch(p, r) {
		t.Fatal("inconsistent owner identity accepted as administrator")
	}
}

func TestCustomerInvitationAndBindingDigestScope(t *testing.T) {
	for _, token := range []string{"", strings.Repeat("t", 42), strings.Repeat("t", 44), strings.Repeat("t", 42) + "/"} {
		if onboardingToken(token) {
			t.Fatal("invalid token accepted")
		}
	}
	if onboardingKey("spaces in-key") || onboardingKey("short") || !onboardingKey("valid-request-key") {
		t.Fatal("idempotency key validation")
	}
	owner := bindingIdentityDigest("wsl", "local-group", "native-owner")
	for _, d := range [][]byte{
		bindingIdentityDigest("lajiaoliao", "local-group", "native-owner"),
		bindingIdentityDigest("wsl", "other-group", "native-owner"),
		bindingIdentityDigest("wsl", "local-group", "another-owner"),
	} {
		if string(owner) == string(d) {
			t.Fatal("ownership digest lost its platform/group/identity scope")
		}
	}
	in := GroupBindingAcceptInput{NativeGroupID: "native-group", InvitationID: "invitation", Version: 2, Key: "initial-key"}
	a := bindingAcceptDigest("challenge-one", in)
	in.Key = "a-different-key"
	if string(a) != string(bindingAcceptDigest("challenge-one", in)) {
		t.Fatal("transport idempotency key must not change the immutable request")
	}
	if string(a) == string(bindingAcceptDigest("challenge-two", in)) {
		t.Fatal("request digest crossed challenge scope")
	}
}

func TestBindingInvitationRequiresSamePendingAndLocalDeadline(t *testing.T) {
	req := GroupBindingAcceptRequest{GroupBindingNativeScope: GroupBindingNativeScope{NativeGroupID: "native-group", NativeSenderID: "native-owner"},
		InvitationID: "one-invitation", RecipientID: "native-bot", InviterID: "native-owner", ExpiresAt: time.Now().Add(time.Minute)}
	e := GroupBindingInvitationEvidence{NativeGroupID: req.NativeGroupID, InvitationID: req.InvitationID,
		RecipientID: req.RecipientID, InviterID: req.InviterID, ExpiresAt: req.ExpiresAt}
	if !bindingInvitationMatches(req, e) {
		t.Fatal("same current pending record rejected")
	}
	e.InviterID = "other-owner"
	if bindingInvitationMatches(req, e) {
		t.Fatal("unproven native inviter accepted")
	}
	e.InviterID, e.ExpiresAt = req.InviterID, time.Now().Add(-time.Second)
	if bindingInvitationMatches(req, e) {
		t.Fatal("expired local evidence accepted")
	}
}
