package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func syntheticMobileDeployment() Deployment {
	d := syntheticDeployment()
	d.ProtocolVersion, d.AppVersion, d.AppHash = MobileProtocolVersion, 337, MobileAppHash
	return d
}

func TestMobileDeploymentIsPinnedAndSeparate(t *testing.T) {
	d := syntheticMobileDeployment()
	raw, _ := json.Marshal(d)
	parsed, err := ParseMobileDeployment(raw)
	if err != nil || parsed.AppVersion != 337 || parsed.ProtocolVersion != MobileProtocolVersion {
		t.Fatal("fixed mobile deployment rejected", err)
	}
	if _, err = ParseDeployment(raw); err == nil {
		t.Fatal("desktop parser accepted a mobile deployment")
	}
	for _, change := range []func(*Deployment){
		func(d *Deployment) { d.ProtocolVersion = DesktopProtocolVersion },
		func(d *Deployment) { d.ProtocolVersion = "3.7.3" },
		func(d *Deployment) { d.AppVersion = 0 },
		func(d *Deployment) { d.AppVersion = 335 },
		func(d *Deployment) { d.AppHash = "" },
		func(d *Deployment) { d.AppHash += "x" },
		func(d *Deployment) { d.DesktopPlatform = "windows" },
		func(d *Deployment) { d.Origin = "http://example.invalid" },
		func(d *Deployment) { d.Metadata = d.Metadata[:13] },
		func(d *Deployment) { d.SigningSeed = "invalid" },
		func(d *Deployment) { d.Headers["accept"] = "bad\r\nheader" },
	} {
		d := syntheticMobileDeployment()
		change(&d)
		raw, _ := json.Marshal(d)
		if _, err := ParseMobileDeployment(raw); err == nil {
			t.Fatal("unpinned or malformed mobile deployment accepted")
		}
		if client, err := NewMobileClient(d); err == nil {
			client.Close()
			t.Fatal("constructor accepted an invalid mobile deployment")
		}
	}
	desktop, _ := json.Marshal(syntheticDeployment())
	for _, field := range []string{`,"app_version":0`, `,"app_version":null`, `,"app_hash":""`, `,"APP_HASH":null`} {
		raw := append(bytes.Clone(desktop[:len(desktop)-1]), []byte(field+"}")...)
		if _, err := ParseDeployment(raw); err == nil {
			t.Fatal("desktop parser accepted explicit mobile fields")
		}
	}
	for _, raw := range [][]byte{nil, []byte("{} {}"), []byte("[]"), bytes.Repeat([]byte("x"), 60001)} {
		if _, err := ParseMobileDeployment(raw); err == nil {
			t.Fatal("invalid mobile document accepted")
		}
	}
}

func TestMobileDeviceJavaAbsoluteValue(t *testing.T) {
	for _, tc := range []struct{ value, want uint64 }{
		{0, 0}, {7, 7}, {^uint64(0), 1}, {1 << 63, 1 << 63}, {1<<63 + 1, 1<<63 - 1},
	} {
		if javaAbs64(tc.value) != tc.want {
			t.Fatal("device identity differs from Java Math.abs(long)")
		}
	}
}

func TestMobileManualLoginInputsAndGsonDefaults(t *testing.T) {
	password := map[string]any{"account": "456", "passwd": "synthetic-password",
		"validateStr": "synthetic-human", "type": "LOGIN_TYPE_ACCOUNT_PWD"}
	params, err := mobileLoginParams(password)
	if err != nil || len(params) != 16 || params["version"] != MobileProtocolVersion ||
		params["appVersion"] != 337 || params["appHash"] != MobileAppHash ||
		params["key"] != "" || params["qrcodeId"] != 0 || params["groupAccount"] != 0 {
		t.Fatal("mobile login omitted the pinned identity or Gson defaults", err)
	}
	if len(password) != 4 {
		t.Fatal("mobile adapter modified the caller's login input")
	}
	sms := map[string]any{"phone": map[string]any{"countryCode": 86, "nationalNumber": 13800000000},
		"key": "synthetic-key", "verificationCode": "123456", "validateStr": "synthetic-human", "type": "LOGIN_TYPE_SMS"}
	params, err = mobileLoginParams(sms)
	if err != nil || params["verificationCode"] != "123456" || params["account"] != "" ||
		params["phone"].(map[string]any)["nationalNumber"] != uint64(13800000000) {
		t.Fatal("mobile SMS login lost its manually requested phone or challenge", err)
	}
	for _, mutate := range []func(map[string]any){
		func(p map[string]any) { p["type"] = "LOGIN_TYPE_ASSIST_CERT" },
		func(p map[string]any) { p["appHash"] = "caller-selected" },
		func(p map[string]any) { p["passwd"] = string(bytes.Repeat([]byte("x"), 1025)) },
		func(p map[string]any) { p["account"] = "456\n" },
		func(p map[string]any) { p["account"] = "0456" },
		func(p map[string]any) { p["validateStr"] = "" },
	} {
		p := map[string]any{}
		for k, v := range password {
			p[k] = v
		}
		mutate(p)
		if _, err := mobileLoginParams(p); err == nil {
			t.Fatal("unbounded or non-manual login accepted")
		}
	}
}

type mobileManagerClient struct {
	fakeClient
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	exited    chan struct{}
	closed    atomic.Int32
}

func (f *mobileManagerClient) Login(ctx context.Context, params map[string]any) error {
	if f.entered != nil {
		f.calls.Add(1)
		close(f.entered)
		<-ctx.Done()
		if f.cancelled != nil {
			close(f.cancelled)
			<-f.release
		}
		close(f.exited)
		return rejection("native_cancelled")
	}
	return f.fakeClient.Login(ctx, params)
}

func (f *mobileManagerClient) Finish(context.Context) (Session, string, error) {
	return Session{UID: 123, ProtocolVersion: MobileProtocolVersion, AccountID: "456"}, "synthetic", nil
}

func (f *mobileManagerClient) Close() { f.closed.Add(1) }

func TestMobileManagerModesAndSingleCommit(t *testing.T) {
	m := NewMobileManager()
	t.Cleanup(m.Close)
	f := &mobileManagerClient{}
	m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
	d := syntheticMobileDeployment()
	if _, err := m.Start("owner", "account", syntheticDeployment(), nil); err == nil {
		t.Fatal("mobile manager accepted desktop configuration")
	}
	if _, err := m.StartSignup("owner", "account", d, SignupHooks{}, nil); err == nil {
		t.Fatal("mobile manager exposed signup")
	}
	var commits atomic.Int32
	v, err := m.Start("owner", "account", d, func(_ context.Context, s Session, _ string) error {
		if s.UID != 123 || s.ProtocolVersion != MobileProtocolVersion || s.NIMToken != "" {
			t.Error("wrong authentication mode reached commit")
		}
		commits.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Action: "password", Account: "456", Password: "synthetic", Captcha: "synthetic-human"}
	if _, err = m.Action("other", "account", v.ID, input); err == nil {
		t.Fatal("foreign actor used a mobile flow")
	}
	if _, err = m.Action("owner", "account", v.ID, input); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, v.ID, "authenticated")
	if _, err = m.Action("owner", "account", v.ID, input); err == nil || commits.Load() != 1 || f.calls.Load() != 1 {
		t.Fatal("authenticated mobile flow replayed")
	}
	m.Close()
	next := &mobileManagerClient{}
	m.Factory = func(Deployment) (AuthClient, error) { return next, nil }
	fresh, err := m.Start("owner", "account", d, func(_ context.Context, s Session, _ string) error {
		commits.Add(1)
		return nil
	})
	if err != nil || fresh.ID == v.ID || fresh.Status != "pending" {
		t.Fatal("configuration reload left the mobile manager permanently closed", err)
	}
	if _, err := m.Action("owner", "account", fresh.ID, input); err != nil {
		t.Fatal("reloaded mobile manager could not authenticate", err)
	}
	waitState(t, m, fresh.ID, "authenticated")
	if commits.Load() != 2 || next.calls.Load() != 1 {
		t.Fatal("reloaded mobile manager lost or duplicated its commit")
	}
}

func TestMobileManagerCloseCancelsAndJoins(t *testing.T) {
	m := NewMobileManager()
	f := &mobileManagerClient{entered: make(chan struct{}), exited: make(chan struct{}),
		cancelled: make(chan struct{}), release: make(chan struct{})}
	m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
	var commits atomic.Int32
	v, err := m.Start("owner", "account", syntheticMobileDeployment(),
		func(context.Context, Session, string) error { commits.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action("owner", "account", v.ID,
		Input{Action: "password", Account: "456", Password: "synthetic", Captcha: "synthetic-human"}); err != nil {
		t.Fatal(err)
	}
	<-f.entered
	var closers sync.WaitGroup
	for range 2 {
		closers.Go(m.Close)
	}
	done := make(chan struct{})
	go func() { closers.Wait(); close(done) }()
	select {
	case <-f.cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not cancel active authentication")
	}
	if _, err := m.Start("owner", "new-account", syntheticMobileDeployment(), nil); err == nil {
		t.Error("manager started new work while joining cancelled authentication")
	}
	if _, err := m.Action("owner", "account", v.ID, Input{Action: "password"}); err == nil {
		t.Error("manager accepted an action while resetting")
	}
	close(f.release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("mobile manager failed to join its cancelled authentication")
	}
	select {
	case <-f.exited:
	default:
		t.Fatal("close returned while authentication was still running")
	}
	if commits.Load() != 0 || f.calls.Load() != 1 || f.closed.Load() != 1 || m.ActiveAccount("account") {
		t.Fatal("cancelled mobile authentication committed, retried or leaked")
	}
}

func TestManagerClosePreservesConfigurationReloadForBothModes(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		d, m := syntheticDeployment(), NewManager()
		if mobile {
			name, d, m = "mobile", syntheticMobileDeployment(), NewMobileManager()
		}
		t.Run(name, func(t *testing.T) {
			defer m.Close()
			m.Factory = func(Deployment) (AuthClient, error) { return &mobileManagerClient{}, nil }
			old, err := m.Start("owner", "account", d, nil)
			if err != nil {
				t.Fatal("initial configuration could not start a transaction", err)
			}
			m.Close()
			if _, err := m.Poll("owner", "account", old.ID); err == nil {
				t.Fatal("configuration reload retained an old authentication transaction")
			}
			var commits atomic.Int32
			fresh, err := m.Start("owner", "account", d,
				func(context.Context, Session, string) error { commits.Add(1); return nil })
			if err != nil || fresh.ID == old.ID {
				t.Fatal("configuration reload disabled its existing manager", err)
			}
			if _, err := m.Action("owner", "account", fresh.ID, Input{Action: "password",
				Account: "456", Password: "synthetic", Captcha: "synthetic-human"}); err != nil {
				t.Fatal("configuration reload blocked a new manual login", err)
			}
			waitState(t, m, fresh.ID, "authenticated")
			if commits.Load() != 1 {
				t.Fatal("configuration reload lost or duplicated session commit")
			}
		})
	}
}
