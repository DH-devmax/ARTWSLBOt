package nativeauth

import (
	"crypto/sha256"
	"encoding/hex"

	"wangshangliao/internal/nativewire"
)

type DeviceInfo struct {
	BusinessClient  string `json:"business_client"`
	DesktopPlatform string `json:"desktop_platform,omitempty"`
	BusinessID      string `json:"business_fingerprint"`
	IMClient        string `json:"im_client"`
	IMID            string `json:"im_fingerprint"`
	Coexistence     string `json:"coexistence"`
}

// Fingerprints identify this deployment, not the browser or a physical computer.
// No credential or raw native device identifier is returned.
func DescribeDevice(d Deployment) DeviceInfo {
	out := DeviceInfo{BusinessClient: "unsupported", IMClient: "web", Coexistence: "unverified"}
	if !supportedProtocol(d.ProtocolVersion) || !supportedDesktop(d.DesktopPlatform) {
		return out
	}
	out.BusinessClient = "desktop"
	out.DesktopPlatform = d.DesktopPlatform
	if out.DesktopPlatform == "" {
		out.DesktopPlatform = "windows"
	}
	fingerprint := func(value []byte) string {
		digest := sha256.Sum256(value)
		return hex.EncodeToString(digest[:8])
	}
	out.IMID = fingerprint([]byte(d.DeviceID))
	seed, err := decodeKey(d.SigningSeed)
	if err != nil {
		return out
	}
	defer clear(seed)
	public, private, err := nativewire.SigningKeypair(seed)
	clear(private)
	if err == nil {
		out.BusinessID = fingerprint(public)
	}
	return out
}
