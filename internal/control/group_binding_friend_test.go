package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGroupBindingFriendChallengeExactFormat(t *testing.T) {
	text, hash, err := newFriendChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if !validFriendChallenge(text, hash) || len(strings.TrimPrefix(text, "群接入 ")) != 20 {
		t.Fatal("friend challenge does not use the exact 20-character Base32 form")
	}
	for _, altered := range []string{
		strings.ToLower(text), text + "\n", text + "A", " " + text,
		strings.Replace(text, "群接入 ", "群绑定 ", 1),
		"群接入 " + strings.Repeat("0", 20), "群接入 " + strings.Repeat("A", 43),
	} {
		if validFriendChallenge(altered, hash) {
			t.Fatal("non-exact application text accepted")
		}
	}
	other, otherHash, err := newFriendChallenge()
	if err != nil || text == other || bytes.Equal(hash, otherHash) || validFriendChallenge(text, otherHash) {
		t.Fatal("friend challenge reused or crossed secrets", err)
	}
}

func TestGroupBindingFriendIdempotencyScope(t *testing.T) {
	c := bindingRecord{ID: NewID(), Applicant: NewID()}
	in := GroupBindingFriendInput{Version: 3, Key: "original-friend-key"}
	old := bindingFriendRecord{Challenge: c.ID, Actor: c.Applicant, Key: in.Key, RequestHash: bindingFriendDigest(c.ID, in)}
	if err := bindingFriendReplay(old, c, in); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []GroupBindingFriendInput{
		{Version: 4, Key: in.Key}, {Version: 3, Key: "different-friend-key"},
	} {
		if bindingFriendReplay(old, c, changed) == nil {
			t.Fatal("another consent version or key reused an attempt")
		}
	}
	other := c
	other.ID = NewID()
	if bindingFriendReplay(old, other, in) == nil {
		t.Fatal("attempt crossed challenge")
	}
	other = c
	other.Applicant = NewID()
	if bindingFriendReplay(old, other, in) == nil {
		t.Fatal("attempt crossed website actor")
	}
}

func TestGroupBindingFriendFinalApplicationIdentity(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	r := GroupBindingFriendApproval{ApplicationID: "application", SenderID: "native-human", RecipientID: "native-robot", ExpiresAt: deadline}
	e := GroupBindingFriendEvidence{ApplicationID: r.ApplicationID, SenderID: r.SenderID,
		RecipientID: r.RecipientID, State: "pending", ExpiresAt: deadline}
	if !bindingFriendMatches(r, e, "pending") {
		t.Fatal("original pending record rejected")
	}
	for _, mutate := range []func(*GroupBindingFriendEvidence){
		func(e *GroupBindingFriendEvidence) { e.ApplicationID = "another" },
		func(e *GroupBindingFriendEvidence) { e.SenderID = "another" },
		func(e *GroupBindingFriendEvidence) { e.RecipientID = "another" },
		func(e *GroupBindingFriendEvidence) { e.State = "approved" },
		func(e *GroupBindingFriendEvidence) { e.State = "unknown" },
		func(e *GroupBindingFriendEvidence) { e.ExpiresAt = time.Now() },
	} {
		changed := e
		mutate(&changed)
		if bindingFriendMatches(r, changed, "pending") {
			t.Fatal("changed pending application accepted")
		}
	}
	r.ExpiresAt = time.Now().Add(-time.Minute)
	e.State, e.ExpiresAt = "approved", r.ExpiresAt
	if !bindingFriendMatches(r, e, "approved") || bindingFriendMatches(r, e, "pending") {
		t.Fatal("expired mutation was admitted or read-only recovery was forbidden")
	}
	r.NativeGroupID = "group"
	if bindingFriendMatches(r, e, "approved") {
		t.Fatal("friend proof supplied a group binding scope")
	}
}

func TestGroupBindingFriendPayloadSealsChallengeDespitePortJSONRedaction(t *testing.T) {
	text, _, err := newFriendChallenge()
	if err != nil {
		t.Fatal(err)
	}
	p := bindingFriendPayload{Challenge: NewID(), Actor: NewID(), Tenant: NewID(), ChallengeVersion: 2, ChallengeText: text,
		Approval: GroupBindingFriendApproval{GroupBindingFriendRequest: GroupBindingFriendRequest{ChallengeText: text}}}
	public, _ := json.Marshal(p.Approval)
	if bytes.Contains(public, []byte(text)) {
		t.Fatal("native port exposed private application text")
	}
	private, _ := json.Marshal(p)
	var reopened bindingFriendPayload
	if json.Unmarshal(private, &reopened) != nil || reopened.ChallengeText != text {
		t.Fatal("sealed persistence lost exact application text")
	}
}

func TestGroupBindingFriendRejectsInvalidInputBeforeDatabase(t *testing.T) {
	s := GroupBindingService{}
	for _, in := range []GroupBindingFriendInput{{Version: 0, Key: "valid-key"}, {Version: 1, Key: "short"}} {
		_, err := s.VerifyFriend(context.Background(), SessionRef{}, NewID(), in)
		var failure *Failure
		if !errors.As(err, &failure) || failure.Code != "group_binding_friend_invalid" {
			t.Fatal("invalid consent reached the database", err)
		}
	}
	for _, state := range []string{"cancelled", "expired", "proven", "bound"} {
		c := bindingRecord{State: state, Version: 1, ExpiresAt: time.Now().Add(time.Minute)}
		if bindingFriendPending(c, 1) == nil {
			t.Fatal("non-waiting challenge admitted", state)
		}
	}
	c := bindingRecord{State: "waiting", Version: 2, ExpiresAt: time.Now().Add(time.Minute)}
	if bindingFriendPending(c, 1) == nil {
		t.Fatal("stale consent version admitted")
	}
}
