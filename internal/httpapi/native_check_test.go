package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

type isolatedDeploymentCheck struct {
	isolatedAuth
	onRequest func()
	calls     int
}

func (c *isolatedDeploymentCheck) Request(_ context.Context, route string, params map[string]any) (map[string]json.RawMessage, error) {
	c.calls++
	if route != "/v1/settings/get-sys-cfg" || len(params) != 0 {
		panic("non-anonymous check")
	}
	if c.onRequest != nil {
		c.onRequest()
	}
	return map[string]json.RawMessage{"enableYidunVerify": json.RawMessage("true")}, nil
}

func exerciseDeploymentCheck(t *testing.T, s *Server, admin, tenant testClient) {
	t.Helper()
	fake := &isolatedDeploymentCheck{}
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return fake, nil }
	defer func() { s.nativeCheckUntil = time.Time{} }()
	expect(t, tenant, "POST", "/native-deployment/check", map[string]int{"version": 1}, 403)
	expect(t, admin, "POST", "/native-deployment/check", map[string]int{"version": 99}, 409)
	value := expect(t, admin, "POST", "/native-deployment/check", map[string]int{"version": 1}, 200)
	if field(t, value, "scope") != "anonymous_settings_only" || fake.calls != 1 {
		t.Fatal("anonymous check result invalid")
	}
	expect(t, admin, "POST", "/native-deployment/check", map[string]int{"version": 1}, 429)
	if fake.calls != 1 {
		t.Fatal("cooldown allowed another upstream request")
	}
	s.nativeCheckUntil = time.Time{}
	fake.onRequest = func() {
		if _, err := s.DB.Platform.Exec(context.Background(), `UPDATE native_deployment SET version=version+1 WHERE singleton`); err != nil {
			t.Fatal(err)
		}
	}
	expect(t, admin, "POST", "/native-deployment/check", map[string]int{"version": 1}, 409)
	// Restore only the isolated fixture's version, never production data.
	if _, err := s.DB.Platform.Exec(context.Background(), `UPDATE native_deployment SET version=1 WHERE singleton`); err != nil {
		t.Fatal(err)
	}
}
