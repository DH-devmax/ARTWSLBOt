package control

import "testing"

func TestPostgresBusinessRuntimeDerivedRowsFollowGroupDeletion(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("wsl", 1)
	scope := f.group("wsl", account, f.tenant, 1, false)
	f.exec(`UPDATE business_capability_gates SET state='pilot' WHERE platform='wsl' AND action='mute_member' AND source='automatic'`)
	f.exec(`INSERT INTO business_capability_pilots(platform,action,source,group_id,gate_version)
		SELECT platform,action,source,$1,version FROM business_capability_gates
		WHERE platform='wsl' AND action='mute_member' AND source='automatic'`, scope.Group)
	f.exec(`DELETE FROM group_bindings WHERE group_id=$1`, scope.Group)
	f.exec(`DELETE FROM native_groups WHERE id=$1`, scope.Group)
	if f.count(`SELECT count(*) FROM group_business_runtime WHERE group_id=$1`, scope.Group) != 0 ||
		f.count(`SELECT count(*) FROM business_capability_pilots WHERE group_id=$1`, scope.Group) != 0 {
		t.Fatal("derived runtime or pilot outlived the deleted native group")
	}
}
