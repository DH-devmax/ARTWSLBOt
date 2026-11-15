package httpapi

import (
	"testing"
	"time"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

func TestSocialEvidencePreservesReceiverGeneration(t *testing.T) {
	snapshot := gateway.SocialSnapshot{SettingSnapshot: gateway.SettingSnapshot{
		Platform: "lajiaoliao", SelfID: "self-string", SelfRole: "owner",
		ReceiverOwner: "synthetic-owner", ReceiverRevision: 11,
		AccountEpoch: 3, AccountVersion: 5, Digest: "synthetic-digest", SampledAt: time.Now().UTC(),
	}, Group: "group-string", Present: true, DirectoryKnown: true, MembersKnown: true, FriendsKnown: true,
		Friends: []nativeauth.Friend{{ID: "peer-string", Name: "Synthetic peer"}}}
	proof := socialEvidence(snapshot, nativeauth.SettingChange{
		Kind: "friend_delete", Group: snapshot.Group, Target: "peer-string",
	})
	if !proof.SocialAllowed || proof.Target != "peer-string" || proof.ReceiverOwner != snapshot.ReceiverOwner ||
		proof.ReceiverRevision != snapshot.ReceiverRevision || proof.Epoch != snapshot.AccountEpoch ||
		proof.Version != snapshot.AccountVersion || proof.ObservedAt != snapshot.SampledAt {
		t.Fatal("social submission lost its server-owned receiver evidence")
	}
}
