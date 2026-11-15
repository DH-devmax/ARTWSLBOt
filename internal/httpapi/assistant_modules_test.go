package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"wangshangliao/internal/control"
	"wangshangliao/internal/features"
	"wangshangliao/internal/security"
)

// Explicit synthetic policy administration, never a product default.
func openSyntheticAssistantModules(t *testing.T, admin testClient) {
	t.Helper()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		c := admin
		c.workspace = platform
		raw := expect(t, c, "GET", "/assistant/module-exposure", nil, 200)
		var before control.AssistantExposure
		if err := json.Unmarshal(raw, &before); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, d := range features.AssistantCapabilities(platform) {
			if d.Implemented {
				ids = append(ids, d.ID)
			}
		}
		expect(t, c, "PUT", "/assistant/module-exposure", map[string]any{"version": before.Version, "capabilities": ids}, 200)
	}
}

func TestPostgresAssistantModuleExposureHTTP(t *testing.T) {
	db := integrationDB(t)
	password := "synthetic-module-policy-password"
	hash, _ := security.Password(password)
	if _, err := db.Platform.Exec(context.Background(), `INSERT INTO users(username,password_hash,role) VALUES('module-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{83}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	defer s.StopAssistant()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": password}, 200)
		return c
	}
	admin := login("module-admin")
	expect(t, admin, "POST", "/users", map[string]string{"username": "module-owner", "password": password, "tenant_name": "synthetic"}, 201)
	owner := login("module-owner")
	expect(t, owner, "GET", "/assistant/module-exposure", nil, 403)
	expect(t, owner, "PUT", "/assistant/module-exposure", map[string]any{"version": 1, "capabilities": []string{"blocked_words.query"}}, 403)
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		admin.workspace = platform
		var p control.AssistantExposure
		if err := json.Unmarshal(expect(t, admin, "GET", "/assistant/module-exposure", nil, 200), &p); err != nil {
			t.Fatal(err)
		}
		if p.Version != 1 || len(p.Capabilities) != 0 {
			t.Fatal("new platform was not closed", p)
		}
		expect(t, admin, "PUT", "/assistant/module-exposure", map[string]any{"version": 1, "capabilities": []string{"unregistered.query"}}, 400)
		expect(t, admin, "PUT", "/assistant/module-exposure", map[string]any{"version": 1, "capabilities": []string{"blocked_words.query", "blocked_words.query"}}, 400)
		expect(t, admin, "PUT", "/assistant/module-exposure", map[string]any{"version": 1, "capabilities": []string{"blocked_words.query"}}, 200)
		expect(t, admin, "PUT", "/assistant/module-exposure", map[string]any{"version": 1, "capabilities": []string{}}, 409)
	}
}
