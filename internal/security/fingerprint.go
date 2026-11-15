package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Fingerprint binds retries without persisting a guessable password/OTP hash.
func (v *Vault) Fingerprint(scope string, data []byte) string {
	mac := hmac.New(sha256.New, v.fingerprintKey[:])
	mac.Write([]byte(scope))
	mac.Write([]byte{0})
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
