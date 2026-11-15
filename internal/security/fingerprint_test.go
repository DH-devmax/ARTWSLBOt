package security

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestSecretFingerprintIsKeyedAndScoped(t *testing.T) {
	a, _ := NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{45}, 32)))
	b, _ := NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{46}, 32)))
	input := []byte("synthetic-old-password")
	first := a.Fingerprint("account-a", input)
	if first != a.Fingerprint("account-a", input) || first == a.Fingerprint("account-b", input) ||
		first == b.Fingerprint("account-a", input) || len(first) != 64 {
		t.Fatal("secret fingerprint not stable/keyed/scoped")
	}
}
