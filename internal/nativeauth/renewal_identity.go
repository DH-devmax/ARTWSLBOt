package nativeauth

import "encoding/json"

// This changes the business renewal contract, not the persisted 2.8.3 codec.
// The tuple is derived from one manifest-verified official macOS ARM64 archive.
const desktopRenewalRelease = "2.8.4"

type desktopRenewalClient struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	V1      string `json:"v1"`
	V2      string `json:"v2"`
}

func desktopRenewalParams(d Deployment) (map[string]any, error) {
	if d.ProtocolVersion != DesktopProtocolVersion || d.DesktopPlatform != "macos" {
		return nil, rejection("native_client_profile_unavailable")
	}
	return map[string]any{
		"nimDeviceId": d.DeviceID,
		"client": desktopRenewalClient{
			Kind:    "CLIENT_KIND_DESKTOP_MAC_ARM64",
			Version: desktopRenewalRelease,
			V1:      "2d3366f53767465ac3833c82c5c095e9c1edbadf47c0f6b776ac6201ef829aeb",
			V2:      "34251676bf6ecfacb4a14365b14e5e5690d4b818f06c71a088b2847225ec6cef",
		},
	}, nil
}

func decodeRenewedCallback(result map[string]json.RawMessage) (string, error) {
	token, mode, err := decodeIMToken(result)
	if err != nil {
		return "", err
	}
	// Both reviewed desktop versions use the current token as callback loginExt.
	// The 2.8.4 initializer fixes mode 2 even when the response omits kind.
	if _, present := result["kind"]; present && mode != 2 {
		return "", rejection("native_nim_token_invalid")
	}
	return token, nil
}
