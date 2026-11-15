package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

func assistantEvidenceSnapshot() gateway.ModerationSnapshot {
	return gateway.ModerationSnapshot{
		SettingSnapshot: gateway.SettingSnapshot{
			Platform: "wsl", SelfID: "10001", SelfRole: "admin", AccountEpoch: 1, AccountVersion: 2,
			DeploymentVersion: 3, CredentialDigest: [32]byte{7}, Digest: "synthetic",
			SampledAt: time.Now().Add(-time.Second),
		},
		Group: "20001", Present: true,
		Members: []nativeauth.GroupMember{
			{ID: "owner", Role: "owner"}, {ID: "10001", Role: "admin"}, {ID: "30001", Role: "member"},
		},
	}
}

func TestModerationEvidenceRequiresOneNonemptyOwner(t *testing.T) {
	change := nativeauth.SettingChange{Kind: "mute_member", Group: "20001", Target: "30001", Minutes: 1}
	for _, row := range []struct {
		name    string
		members []nativeauth.GroupMember
		owner   string
	}{
		{"one", assistantEvidenceSnapshot().Members, "owner"},
		{"none", []nativeauth.GroupMember{{ID: "30001", Role: "member"}}, ""},
		{"two", []nativeauth.GroupMember{{ID: "owner-a", Role: "owner"}, {ID: "owner-b", Role: "owner"}}, ""},
		{"duplicate", []nativeauth.GroupMember{{ID: "owner", Role: "owner"}, {ID: "owner", Role: "owner"}}, ""},
		{"empty", []nativeauth.GroupMember{{ID: "", Role: "owner"}, {ID: "owner", Role: "owner"}}, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			snapshot := assistantEvidenceSnapshot()
			snapshot.Members = row.members
			proof := moderationEvidence(snapshot, change)
			if proof.OwnerID != row.owner {
				t.Fatalf("owner evidence = %q, want %q", proof.OwnerID, row.owner)
			}
		})
	}
	proof := moderationEvidence(assistantEvidenceSnapshot(), change)
	if proof.Target != change.Target || proof.TargetRole != "member" || !proof.ModerationAllowed {
		t.Fatal("fresh roster target was not retained", proof)
	}
	change.Target = "missing-member"
	proof = moderationEvidence(assistantEvidenceSnapshot(), change)
	if proof.TargetRole != "" || proof.ModerationAllowed {
		t.Fatal("missing member was inferred from request", proof)
	}
}

func TestAssistantMemberEvidenceBindsBothFreshSnapshots(t *testing.T) {
	snapshot := assistantEvidenceSnapshot()
	proof := moderationEvidence(snapshot, nativeauth.SettingChange{
		Kind: "member_nickname", Group: snapshot.Group, Target: "30001", Text: "synthetic-card",
	})
	proof.ReceiverOwner, proof.ReceiverRevision = "synthetic-receiver", 8
	card := snapshot.SettingSnapshot
	card.Digest = "card-snapshot"
	card.SampledAt = card.SampledAt.Add(time.Second)
	card.Member = &nativeauth.GroupMember{ID: "30001", Role: "member"}
	out, err := assistantMemberEvidence(card, proof, "30001")
	if err != nil || out.OwnerID != "owner" || out.ReceiverOwner != proof.ReceiverOwner ||
		out.ReceiverRevision != 8 || out.Digest != card.Digest || !out.ObservedAt.Equal(proof.ObservedAt) {
		t.Fatal("card lost ownership, connection, or freshness evidence", out, err)
	}
	for _, row := range []struct {
		name   string
		change func(*gateway.SettingSnapshot, *control.SettingEvidence)
	}{
		{"owner", func(_ *gateway.SettingSnapshot, p *control.SettingEvidence) { p.OwnerID = "" }},
		{"receiver", func(_ *gateway.SettingSnapshot, p *control.SettingEvidence) { p.ReceiverOwner = "" }},
		{"revision", func(_ *gateway.SettingSnapshot, p *control.SettingEvidence) { p.ReceiverRevision = 0 }},
		{"role", func(c *gateway.SettingSnapshot, _ *control.SettingEvidence) { c.SelfRole = "member" }},
		{"epoch", func(c *gateway.SettingSnapshot, _ *control.SettingEvidence) { c.AccountEpoch++ }},
		{"version", func(c *gateway.SettingSnapshot, _ *control.SettingEvidence) { c.AccountVersion++ }},
		{"deployment", func(c *gateway.SettingSnapshot, _ *control.SettingEvidence) { c.DeploymentVersion++ }},
		{"credential", func(c *gateway.SettingSnapshot, _ *control.SettingEvidence) { c.CredentialDigest[0]++ }},
		{"target", func(c *gateway.SettingSnapshot, _ *control.SettingEvidence) {
			c.Member = &nativeauth.GroupMember{ID: "another", Role: "member"}
		}},
		{"target-role", func(c *gateway.SettingSnapshot, _ *control.SettingEvidence) {
			c.Member = &nativeauth.GroupMember{ID: "30001", Role: "admin"}
		}},
		{"roster-target", func(_ *gateway.SettingSnapshot, p *control.SettingEvidence) { p.Target = "another" }},
	} {
		t.Run(row.name, func(t *testing.T) {
			current, roster := card, proof
			row.change(&current, &roster)
			if _, err := assistantMemberEvidence(current, roster, "30001"); err == nil {
				t.Fatal("changed evidence crossed the member-card boundary")
			}
		})
	}
}

func TestAssistantEvidenceRejectsUnprovenOwnershipBeforeConnectionRead(t *testing.T) {
	req := control.SettingRequest{Change: nativeauth.SettingChange{Kind: "mute_member", Group: "20001", Target: "30001", Minutes: 1}}
	for _, row := range []struct {
		name   string
		change func(*gateway.ModerationSnapshot)
	}{
		{"group", func(s *gateway.ModerationSnapshot) { s.Group = "another-group" }},
		{"absent", func(s *gateway.ModerationSnapshot) { s.Present = false }},
		{"owner", func(s *gateway.ModerationSnapshot) { s.Members = nil }},
		{"self-owner", func(s *gateway.ModerationSnapshot) { s.SelfRole = "owner" }},
	} {
		t.Run(row.name, func(t *testing.T) {
			snapshot := assistantEvidenceSnapshot()
			row.change(&snapshot)
			_, err := (&Server{}).assistantNativeEvidence(context.Background(), req, snapshot)
			var denied *control.Failure
			if !errors.As(err, &denied) || denied.Code != "group_ownership_evidence_required" {
				t.Fatal("unproven ownership reached connection lookup", err)
			}
		})
	}
}
