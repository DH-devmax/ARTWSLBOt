package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wangshangliao/internal/control"
)

func proofWindowRequest(body, key string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/test-tools/onboarding-friend/challenges/"+
		"11111111-1111-4111-8111-111111111111/proof-windows", strings.NewReader(body))
	r.SetPathValue("id", "11111111-1111-4111-8111-111111111111")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("X-Workspace", "wsl")
	r.AddCookie(&http.Cookie{Name: "wsl_session", Value: strings.Repeat("s", 43)})
	return r
}

func TestOnboardingControlledProofHTTPAuthentication(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.onboardingControlledProofRoutes(&control.GroupBindingService{})
	r := proofWindowRequest(`{}`, "synthetic-proof-window")
	r.Header.Del("Cookie")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var response struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil ||
		w.Code != http.StatusUnauthorized || response.Msg != "authentication_required" {
		t.Fatal("proof window route lost its website authentication", w.Code, response.Msg, err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("proof window response may be cached")
	}
}

func TestOnboardingControlledProofHTTPStrictContract(t *testing.T) {
	body := `{"version":1,"kind":"private","text":"群接入 ` + strings.Repeat("A", 43) + `","seconds":300}`
	s := &Server{}
	handler := s.onboardingControlledProofWindow(&control.GroupBindingService{})
	for name, value := range map[string]struct{ body, key string }{
		"source identity":  {strings.TrimSuffix(body, "}") + `,"account_id":"forged"}`, "synthetic-proof"},
		"trusted scope":    {strings.TrimSuffix(body, "}") + `,"controlled_scope_id":"forged"}`, "synthetic-proof"},
		"body key":         {strings.TrimSuffix(body, "}") + `,"key":"forged-body-key"}`, "synthetic-proof"},
		"extra JSON":       {body + `{}`, "synthetic-proof"},
		"malformed":        {`{"version":`, "synthetic-proof"},
		"missing header":   {body, ""},
		"invalid header":   {body, "contains whitespace"},
		"arbitrary action": {strings.Replace(body, `"private"`, `"send_text"`, 1), "synthetic-proof"},
		"overlong":         {strings.Replace(body, `"seconds":300`, `"seconds":301`, 1), "synthetic-proof"},
	} {
		t.Run(name, func(t *testing.T) {
			r := proofWindowRequest(value.body, value.key)
			w := httptest.NewRecorder()
			err := handler(w, r, Principal{Role: "superadmin"})
			if err == nil {
				t.Fatal("invalid proof contract reached service work")
			}
			sendError(w, err)
			if w.Code != http.StatusBadRequest || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("invalid proof body or key was accepted", w.Code)
			}
		})
	}
	w := httptest.NewRecorder()
	r := proofWindowRequest(body, "synthetic-proof-valid")
	err := handler(w, r, Principal{Role: "superadmin"})
	friendFailure(t, err, 503, "group_binding_native_unavailable")
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("valid window preparation may be cached")
	}
}
