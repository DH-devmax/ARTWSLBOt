package httpapi

import (
	"net/http"
	"wangshangliao/internal/control"
)

// These recovery routes do not resolve current bindings or depend on seven-day
// message projections. Control checks the current website session, superadmin
// role, account and original actor on every lookup. No dispatch lives here.
func (s *Server) inspectAccountNativeText(w http.ResponseWriter, r *http.Request, p Principal) error {
	if conversation := r.URL.Query().Get("conversation"); conversation != "" {
		if r.PathValue("operation") != "" || r.URL.Query().Get("key") != "" {
			return fail(400, "invalid_operation")
		}
		out, err := s.nativeTextService().PendingConversationText(r.Context(), assistantRef(r),
			r.PathValue("id"), conversation)
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"items": out})
		return nil
	}
	out, err := s.nativeTextService().InspectAccount(r.Context(), assistantRef(r), r.PathValue("id"),
		r.PathValue("operation"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) reconcileAccountNativeText(w http.ResponseWriter, r *http.Request, p Principal) error {
	if _, err := s.nativeTextService().InspectAccount(r.Context(), assistantRef(r), r.PathValue("id"),
		r.PathValue("operation"), ""); err != nil {
		return err
	}
	if s.Receiver == nil {
		return fail(503, "native_text_evidence_unavailable")
	}
	var out control.TextOperation
	var err error
	service := s.nativeTextService()
	if requestWorkspace(r) == "lajiaoliao" {
		out, err = service.ReconcileChiliMedia(r.Context(), assistantRef(r), r.PathValue("operation"), s.Receiver)
	} else {
		out, err = service.Reconcile(r.Context(), assistantRef(r), r.PathValue("operation"), s.Receiver)
	}
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}
