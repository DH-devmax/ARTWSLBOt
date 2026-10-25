//go:build sodium && cgo

package nativeauth

import "testing"

func TestResumeBindsSavedSessionWithoutLogin(t *testing.T) {
	d := syntheticDeployment()
	base, err := NewClient(d)
	if err != nil {
		t.Fatal(err)
	}
	client := base.(*Client)
	saved := Session{Origin: d.Origin, ProtocolVersion: d.ProtocolVersion, UID: 123, Device: client.fields[0],
		JWT: "synthetic-jwt", MAC: make([]byte, 16), NIMID: "synthetic-peer", NIMToken: "synthetic-im"}
	client.Close()
	resumed, err := Resume(d, saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.fields[3] != 123 || resumed.jwt != saved.JWT {
		t.Fatal("saved identity not restored")
	}
	resumed.Close()
	for _, mutate := range []func(*Session){
		func(s *Session) { s.Origin = "https://other.example" },
		func(s *Session) { s.Device++ },
		func(s *Session) { s.ProtocolVersion = "unsupported" },
		func(s *Session) { s.MAC = nil },
		func(s *Session) { s.NIMAuthType = 3 },
	} {
		changed := saved
		mutate(&changed)
		if bad, err := Resume(d, changed, nil); err == nil {
			bad.Close()
			t.Fatal("unbound session restored")
		}
	}
}
