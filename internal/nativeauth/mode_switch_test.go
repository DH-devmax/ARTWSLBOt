package nativeauth

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type switchingClient struct {
	fakeClient
	closed atomic.Bool
}

func (f *switchingClient) Close() { f.closed.Store(true) }

func testSignupHooks() SignupHooks {
	return SignupHooks{
		BeforeSubmit: func(context.Context, string) error { return nil },
		Complete:     func(context.Context, string, string, error) error { return nil },
	}
}

func requireNativeCode(t *testing.T, err error, code string) {
	t.Helper()
	var native *Error
	if !errors.As(err, &native) || native.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestModeSwitchReplacesOnlyOwnIdleFlowAndPreservesLimits(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	var clients []*switchingClient
	m.Factory = func(Deployment) (AuthClient, error) {
		client := &switchingClient{}
		clients = append(clients, client)
		return client, nil
	}
	view, err := m.Start("owner", "account", syntheticDeployment(), nil)
	if err != nil {
		t.Fatal(err)
	}
	expires, retry, resend := view.ExpiresAt, time.Now().Add(time.Minute), time.Now().Add(2*time.Minute)
	m.mu.Lock()
	m.items[view.ID].attempts = 11
	m.items[view.ID].view.RetryAt, m.items[view.ID].view.ResendAt = retry, resend
	m.mu.Unlock()
	for i := range 12 {
		old := view
		if i%2 == 0 {
			view, err = m.StartSignup("owner", "account", syntheticDeployment(), testSignupHooks(), nil)
		} else {
			view, err = m.Start("owner", "account", syntheticDeployment(), nil)
		}
		if err != nil || view.ID == old.ID || !view.ExpiresAt.Equal(expires) ||
			!view.RetryAt.Equal(retry) || !view.ResendAt.Equal(resend) {
			t.Fatal("tab switch reset or failed the flow", view, err)
		}
		if _, err = m.Poll("owner", "account", old.ID); err == nil {
			t.Fatal("replaced flow still visible")
		}
		_, err = m.Action("owner", "account", old.ID, Input{Action: "password"})
		requireNativeCode(t, err, "native_auth_not_found")
		_, err = m.Cancel("owner", "account", old.ID)
		requireNativeCode(t, err, "native_auth_not_found")
		if !clients[i].closed.Load() || clients[i].calls.Load() != 0 {
			t.Fatal("switch leaked a client or submitted an upstream request")
		}
	}
	m.mu.Lock()
	count, attempts, budget := len(m.items), m.items[view.ID].attempts, m.starts["account"].count
	m.mu.Unlock()
	if count != 1 || attempts != 11 || budget != 1 {
		t.Fatal("switch bypassed transaction, attempt or start limits")
	}
	same, err := m.Start("owner", "account", syntheticDeployment(), nil)
	if err != nil || same.ID != view.ID {
		t.Fatal("reopening login did not reuse the active flow", err)
	}
	_, err = m.StartSignup("other-owner", "account", syntheticDeployment(), testSignupHooks(), nil)
	requireNativeCode(t, err, "native_auth_account_busy")
	_, err = m.Start("other-owner", "account", syntheticDeployment(), nil)
	requireNativeCode(t, err, "native_auth_account_busy")
	_, err = m.Poll("other-owner", "account", view.ID)
	requireNativeCode(t, err, "native_auth_not_found")
}

func TestModeSwitchCannotReplaceWorkingOrUnknownFlow(t *testing.T) {
	for _, state := range []string{"working", "unknown", "registered", "authenticated"} {
		t.Run(state, func(t *testing.T) {
			m := NewManager()
			t.Cleanup(m.Close)
			m.Factory = func(Deployment) (AuthClient, error) { return &switchingClient{}, nil }
			view, err := m.Start("owner", "account", syntheticDeployment(), nil)
			if err != nil {
				t.Fatal(err)
			}
			m.mu.Lock()
			m.items[view.ID].view.Status = state
			m.items[view.ID].busy = state == "working"
			m.mu.Unlock()
			_, err = m.StartSignup("other-owner", "account", syntheticDeployment(), testSignupHooks(), nil)
			requireNativeCode(t, err, "native_auth_account_busy")
			next, err := m.StartSignup("owner", "account", syntheticDeployment(), testSignupHooks(), nil)
			switch state {
			case "working":
				requireNativeCode(t, err, "native_auth_busy")
			case "unknown":
				requireNativeCode(t, err, "native_auth_review_required")
			default:
				if err != nil || next.ID == view.ID {
					t.Fatal("completed own flow blocked the next step", err)
				}
			}
			if state == "working" || state == "unknown" {
				current, err := m.Poll("owner", "account", view.ID)
				if err != nil || current.Status != state {
					t.Fatal("blocked switch changed the active flow", err)
				}
			}
		})
	}
}

func TestModeSwitchFailureKeepsPreviousFlowUsable(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	client := &switchingClient{}
	m.Factory = func(Deployment) (AuthClient, error) { return client, nil }
	view, err := m.Start("owner", "account", syntheticDeployment(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.Factory = func(Deployment) (AuthClient, error) { return nil, errors.New("synthetic factory failure") }
	if _, err = m.StartSignup("owner", "account", syntheticDeployment(), testSignupHooks(), nil); err == nil {
		t.Fatal("failed client creation was accepted")
	}
	current, err := m.Poll("owner", "account", view.ID)
	if err != nil || current.Status != "pending" || client.closed.Load() {
		t.Fatal("failed switch destroyed the previous flow", err)
	}
}
