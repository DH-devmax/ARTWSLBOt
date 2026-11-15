package nativeauth

import (
	"encoding/json"
	"testing"
)

func TestDesktopRenewalProfileIsOnePinnedArtifact(t *testing.T) {
	d := Deployment{ProtocolVersion: DesktopProtocolVersion, DesktopPlatform: "macos",
		DeviceID: "synthetic-existing-device",
		Headers:  map[string]string{"client": "untrusted", "x-client": "11", "x-version": "fake"}}
	first, err := desktopRenewalParams(d)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Device string `json:"nimDeviceId"`
		Client struct {
			Kind, Version, V1, V2 string
		} `json:"client"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Device != d.DeviceID ||
		body.Client.Kind != "CLIENT_KIND_DESKTOP_MAC_ARM64" || body.Client.Version != "2.8.4" ||
		body.Client.V1 != "2d3366f53767465ac3833c82c5c095e9c1edbadf47c0f6b776ac6201ef829aeb" ||
		body.Client.V2 != "34251676bf6ecfacb4a14365b14e5e5690d4b818f06c71a088b2847225ec6cef" ||
		len(first) != 2 || first["uid"] != nil || first["loginToken"] != nil {
		t.Fatal("renewal did not serialize the verified complete artifact tuple")
	}
	first["client"] = desktopRenewalClient{Kind: "CLIENT_KIND_DESKTOP_WIN"}
	next, err := desktopRenewalParams(d)
	if err != nil || next["client"].(desktopRenewalClient).Kind != "CLIENT_KIND_DESKTOP_MAC_ARM64" {
		t.Fatal("one request mutated the pinned profile for another request")
	}
	for _, platform := range []string{"", "windows", "android", "MACOS", "unknown"} {
		d.DesktopPlatform = platform
		if _, err := desktopRenewalParams(d); err == nil {
			t.Fatal("unverified platform acquired the ARM64 artifact identity", platform)
		}
	}
	d.DesktopPlatform = "macos"
	for _, version := range []string{"", "2.8.4", MobileProtocolVersion, "unknown"} {
		d.ProtocolVersion = version
		if _, err := desktopRenewalParams(d); err == nil {
			t.Fatal("an unreviewed saved codec acquired renewal compatibility", version)
		}
	}
}

func TestDesktopRenewalCallbackContract(t *testing.T) {
	for _, kind := range []string{"", `"NIM_LOGIN_KIND_CALLBACK"`} {
		result := map[string]json.RawMessage{"nimToken": json.RawMessage(`"synthetic-new-token"`)}
		if kind != "" {
			result["kind"] = json.RawMessage(kind)
		}
		token, err := decodeRenewedCallback(result)
		if err != nil || token != "synthetic-new-token" {
			t.Fatal("reviewed callback response was rejected")
		}
		if _, exists := result["kind"]; exists != (kind != "") {
			t.Fatal("decoding mutated the provider response")
		}
	}
	for _, kind := range []string{`"NIM_LOGIN_KIND_STATIC"`, `"NIM_LOGIN_KIND_DYNC"`,
		`"NIM_LOGIN_KIND_DYNAMIC"`, `"NIM_LOGIN_KIND_THIRD"`, `null`, `2`, `""`, `"unknown"`} {
		if _, err := decodeRenewedCallback(map[string]json.RawMessage{
			"nimToken": json.RawMessage(`"synthetic-new-token"`), "kind": json.RawMessage(kind),
		}); err == nil {
			t.Fatal("a conflicting or unverified callback kind was accepted", kind)
		}
	}
	for _, token := range []string{`""`, `null`, `42`, `"has whitespace"`} {
		if _, err := decodeRenewedCallback(map[string]json.RawMessage{"nimToken": json.RawMessage(token)}); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}
