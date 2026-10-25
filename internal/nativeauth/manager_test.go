package nativeauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedRequestIDsAreUniqueAndDoNotWrap(t *testing.T) {
	var counter atomic.Uint64
	var workers sync.WaitGroup
	ids := make(chan uint64, 100)
	for range 100 {
		workers.Go(func() {
			id, err := nextRequestID(&counter, 1000)
			if err != nil {
				t.Error(err)
			}
			ids <- id
		})
	}
	workers.Wait()
	close(ids)
	seen := map[uint64]bool{}
	for id := range ids {
		if seen[id] || id < 1000 || id >= 1100 {
			t.Fatal("duplicate or invalid request ID")
		}
		seen[id] = true
	}
	counter.Store(^uint64(0))
	if _, err := nextRequestID(&counter, 1000); err == nil {
		t.Fatal("request ID wrapped")
	}
}

func syntheticDeployment() Deployment {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	return Deployment{Origin: "https://example.invalid", Headers: map[string]string{}, Metadata: make([]uint64, 14),
		SigningSeed: key, BodyKey: key, ServerKey: key, CaptchaID: "synthetic-site", DeviceID: "synthetic-device", ProtocolVersion: "3.7.3"}
}

func TestDeploymentAndPublicNetwork(t *testing.T) {
	d := syntheticDeployment()
	b, _ := json.Marshal(d)
	if _, err := ParseDeployment(b); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://example.invalid", "https://user:pass@example.invalid", "https://example.invalid/path", "https://example.invalid?key=x"} {
		d.Origin = origin
		b, _ = json.Marshal(d)
		if _, err := ParseDeployment(b); err == nil {
			t.Fatal("accepted unsafe origin")
		}
	}
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "169.254.169.254", "100.100.100.200", "10.0.0.1", "192.168.1.1", "198.18.0.1"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Fatal("accepted non-public address", ip)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("rejected public address")
	}
}

type fakeClient struct {
	safe    atomic.Bool
	block   bool
	unknown bool
	device  bool
	params  map[string]any
	calls   atomic.Int32
}

func (f *fakeClient) Request(_ context.Context, route string, params map[string]any) (map[string]json.RawMessage, error) {
	f.calls.Add(1)
	f.params = params
	if route == "/v1/user/get-change-device-verify" {
		return map[string]json.RawMessage{
			"phone": json.RawMessage(`{"countryCode":86,"nationalNumber":13800000000}`),
			"sms":   json.RawMessage(`{"key":"synthetic-original"}`),
		}, nil
	}
	return map[string]json.RawMessage{"key": json.RawMessage(`"synthetic-challenge"`)}, nil
}
func (f *fakeClient) Login(ctx context.Context, in map[string]any) error {
	f.calls.Add(1)
	f.params = in
	if f.device && in["type"] != "LOGIN_TYPE_SMS" {
		return &Error{Code: "native_device_verification_required", Number: 1069}
	}
	if f.block {
		<-ctx.Done()
		return rejection("native_cancelled")
	}
	if f.unknown {
		return &Error{Code: "native_transport_unknown", Submitted: true}
	}
	return nil
}

func TestDeviceChallengeAndResendPreserveOriginalKey(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	fake := &fakeClient{device: true}
	m.Factory = func(Deployment) (AuthClient, error) { return fake, nil }
	view, err := m.Start("owner", "account", syntheticDeployment(), func(context.Context, Session, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Action("owner", "account", view.ID, Input{Action: "password", Account: "synthetic", Password: "not-real", Captcha: "synthetic-human"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, view.ID, "sms_required")
	m.mu.Lock()
	m.items[view.ID].view.ResendAt = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "sms_resend", Captcha: "synthetic-human"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, view.ID, "sms_required")
	if fake.params["Key"] != "synthetic-original" {
		t.Fatal("device resend lost original key")
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "sms_verify", Code: "123456", Captcha: "synthetic-human"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, view.ID, "authenticated")
	if fake.params["key"] != "synthetic-challenge" || fake.params["type"] != "LOGIN_TYPE_SMS" {
		t.Fatal("SMS login did not use latest challenge")
	}
}

func TestExpiryAndStartBudget(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	m.Factory = func(Deployment) (AuthClient, error) { return &fakeClient{}, nil }
	for i := range 4 {
		view, err := m.Start("owner", "account", syntheticDeployment(), func(context.Context, Session, string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			m.mu.Lock()
			m.items[view.ID].view.ExpiresAt = time.Now().Add(-time.Second)
			m.mu.Unlock()
			if _, err = m.Poll("owner", "account", view.ID); err == nil {
				t.Fatal("expired transaction polled")
			}
			if _, err = m.Action("owner", "account", view.ID, Input{Action: "password"}); err == nil {
				t.Fatal("expired transaction accepted action")
			}
		}
		if _, err = m.Cancel("owner", "account", view.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Start("owner", "account", syntheticDeployment(), nil); err == nil {
		t.Fatal("start budget bypassed")
	}
}
func (f *fakeClient) VerifySecurity(context.Context, string) error { f.safe.Store(false); return nil }
func (f *fakeClient) Finish(context.Context) (Session, string, error) {
	return Session{UID: 123, JWT: "synthetic-jwt", NIMID: "synthetic-peer", NIMToken: "synthetic-im"}, "synthetic", nil
}
func (f *fakeClient) SecurityRequired() bool { return f.safe.Load() }
func (f *fakeClient) Close()                 {}

func waitState(t *testing.T, m *Manager, id, state string) View {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view, err := m.Poll("owner", "account", id)
		if err != nil {
			t.Fatal(err)
		}
		if view.Status == state {
			return view
		}
		time.Sleep(time.Millisecond)
	}
	view, _ := m.Poll("owner", "account", id)
	t.Fatalf("wanted %s, got %s (%s)", state, view.Status, view.Error)
	return View{}
}

func TestSMSVerificationDoesNotWaitForResendCooldown(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	fake := &fakeClient{}
	m.Factory = func(Deployment) (AuthClient, error) { return fake, nil }
	var saved atomic.Int32
	view, err := m.Start("owner", "account", syntheticDeployment(), func(context.Context, Session, string) error { saved.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action("intruder", "account", view.ID, Input{Action: "sms_request"}); err == nil {
		t.Fatal("foreign owner accepted")
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "sms_request", Account: "13800000000", Captcha: "synthetic-human"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, view.ID, "sms_required")
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "sms_resend", Captcha: "synthetic-human"}); err == nil {
		t.Fatal("resend cooldown bypassed")
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "sms_verify", Code: "123456", Captcha: "synthetic-human"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, view.ID, "authenticated")
	if saved.Load() != 1 {
		t.Fatal("credential commit count")
	}
}

func TestCancelUnknownAndSecondarySecurity(t *testing.T) {
	for _, mode := range []string{"cancel", "unknown", "security"} {
		t.Run(mode, func(t *testing.T) {
			m := NewManager()
			t.Cleanup(m.Close)
			f := &fakeClient{block: mode == "cancel", unknown: mode == "unknown"}
			f.safe.Store(mode == "security")
			m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
			var saved atomic.Int32
			view, err := m.Start("owner", "account", syntheticDeployment(), func(context.Context, Session, string) error { saved.Add(1); return nil })
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.Action("owner", "account", view.ID, Input{Action: "password", Account: "synthetic", Password: "not-real", Captcha: "synthetic-human"}); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "cancel":
				if _, err = m.Cancel("owner", "account", view.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = m.Poll("owner", "account", view.ID); err == nil {
					t.Fatal("cancelled transaction still visible")
				}
			case "unknown":
				waitState(t, m, view.ID, "unknown")
				if _, err = m.Action("owner", "account", view.ID, Input{Action: "password", Account: "synthetic", Password: "not-real", Captcha: "synthetic-human"}); err == nil {
					t.Fatal("unknown operation retried")
				}
				if f.calls.Load() != 1 {
					t.Fatal("automatic retry")
				}
			case "security":
				waitState(t, m, view.ID, "security_required")
				if saved.Load() != 0 {
					t.Fatal("saved before security verification")
				}
				if _, err = m.Action("owner", "account", view.ID, Input{Action: "security_verify", Code: "123456"}); err != nil {
					t.Fatal(err)
				}
				waitState(t, m, view.ID, "authenticated")
			}
			if mode != "security" && saved.Load() != 0 {
				t.Fatal("unexpected commit")
			}
		})
	}
}
