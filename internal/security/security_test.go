package security

import (
	"encoding/base64"
	"testing"
)

func TestPasswords(t *testing.T) {
	h, err := Password("a-long-test-password-123")
	if err != nil || !VerifyPassword(h, "a-long-test-password-123") || VerifyPassword(h, "incorrect") {
		t.Fatal("password verification failed", err)
	}
	if VerifyPassword("$argon2id$v=19$m=99999999,t=2,p=2$invalid$invalid", "a") {
		t.Fatal("unbounded parameters accepted")
	}
}

func TestVaultScope(t *testing.T) {
	v, err := NewVault(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := v.Seal("tenant-a/group-a", []byte("private"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Open("tenant-b/group-a", blob); err == nil {
		t.Fatal("cross-scope decryption succeeded")
	}
	got, err := v.Open("tenant-a/group-a", blob)
	if err != nil || string(got) != "private" {
		t.Fatal("roundtrip failed")
	}
}

func TestMoney(t *testing.T) {
	for input, expected := range map[string]int64{"0": 0, "1.01": 101, "-12.5": -1250, "1000000000.00": 100000000000} {
		got, err := Money(input)
		if err != nil || got != expected {
			t.Fatalf("%s: %d %v", input, got, err)
		}
	}
	for _, input := range []string{"1.001", "1e2", "+1", " 1", ".", "-.", "1.", "NaN", "1000000000.01"} {
		if _, err := Money(input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
