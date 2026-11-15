package nativeauth

import (
	"context"
	"encoding/json"
	"testing"
)

func TestOnlyDesktopCanBeParsedOrStarted(t *testing.T) {
	for _, version := range []string{"", "3.7.1", "3.7.3", "3.7.4", "unknown"} {
		t.Run(version, func(t *testing.T) {
			d := syntheticDeployment()
			d.ProtocolVersion = version
			raw, _ := json.Marshal(d)
			if _, err := ParseDeployment(raw); err == nil || err.Error() != "native_desktop_required" {
				t.Fatal("removed protocol accepted by deployment parser")
			}
			if client, err := NewClient(d); err == nil {
				client.Close()
				t.Fatal("removed protocol constructed an authentication client")
			}
			m := NewManager()
			defer m.Close()
			m.Factory = func(Deployment) (AuthClient, error) {
				t.Fatal("removed protocol reached the authentication factory")
				return nil, nil
			}
			if _, err := m.Start("owner", "account", d, nil); err == nil {
				t.Fatal("removed protocol started a login flow")
			}
			if _, err := m.StartSignup("owner", "account", d, SignupHooks{
				BeforeSubmit: func(context.Context, string) error { return nil },
				Complete:     func(context.Context, string, string, error) error { return nil },
			}, nil); err == nil || err.Error() != "native_desktop_required" {
				t.Fatal("removed protocol started a signup flow")
			}
		})
	}
}

func TestDesktopPlatformValidation(t *testing.T) {
	for _, platform := range []string{"", "windows", "macos"} {
		d := syntheticDeployment()
		d.DesktopPlatform = platform
		raw, _ := json.Marshal(d)
		if _, err := ParseDeployment(raw); err != nil {
			t.Fatal("valid desktop platform rejected", platform, err)
		}
	}
	for _, platform := range []string{"android", "ios", "MACOS", "other", "10"} {
		d := syntheticDeployment()
		d.DesktopPlatform = platform
		raw, _ := json.Marshal(d)
		if _, err := ParseDeployment(raw); err == nil {
			t.Fatal("unsupported desktop platform accepted")
		}
		if c, err := NewClient(d); err == nil {
			c.Close()
			t.Fatal("unsupported platform constructed a client")
		}
	}
}
