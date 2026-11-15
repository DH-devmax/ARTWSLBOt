package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wangshangliao/internal/control"
)

func TestSendErrorFreshRoleIsActionableAndPrivate(t *testing.T) {
	for name, err := range map[string]error{
		"direct":  control.ErrFreshRole,
		"wrapped": fmt.Errorf("synthetic private role context: %w", control.ErrFreshRole),
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			sendError(w, err)
			var response struct {
				Code int             `json:"code"`
				Msg  string          `json:"msg"`
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &response) != nil ||
				w.Code != http.StatusConflict || response.Code != http.StatusConflict ||
				response.Msg != "fresh_native_role_required" || string(response.Data) != "null" {
				t.Fatal("fresh role error lost its fixed conflict diagnosis", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" ||
				strings.Contains(w.Body.String(), "synthetic private") {
				t.Fatal("fresh role diagnosis was cacheable or exposed wrapped context")
			}
		})
	}
}

func TestSendErrorUnknownRoleErrorsRemainInternal(t *testing.T) {
	for name, err := range map[string]error{
		"unrelated": errors.New("synthetic private upstream failure"),
		"same text": errors.New(control.ErrFreshRole.Error()),
		"wrapped":   fmt.Errorf("synthetic private role context: %w", errors.New("unreviewed failure")),
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			sendError(w, err)
			var response struct {
				Code int             `json:"code"`
				Msg  string          `json:"msg"`
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &response) != nil ||
				w.Code != http.StatusInternalServerError || response.Code != http.StatusInternalServerError ||
				response.Msg != "internal_error" || string(response.Data) != "null" {
				t.Fatal("unknown role error became a public business diagnosis", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" ||
				strings.Contains(w.Body.String(), "synthetic private") {
				t.Fatal("unknown error was cacheable or exposed its private cause")
			}
		})
	}
}
