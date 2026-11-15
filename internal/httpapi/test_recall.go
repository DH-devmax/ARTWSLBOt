package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

func (s *Server) testMessageCandidates(w http.ResponseWriter, r *http.Request, p Principal) error {
	workspace, account := requestWorkspace(r), r.URL.Query().Get("account_id")
	if workspace != "wsl" && workspace != "lajiaoliao" || !control.ValidID(account) || s.Receiver == nil {
		return fail(400, "native_test_recall_required")
	}
	scope, err := s.nativeSettingsService().TestScope(r.Context(), assistantRef(r), workspace,
		r.URL.Query().Get("test_window_id"), account, "recall_message", "")
	if err != nil {
		return err
	}
	candidates, err := s.Receiver.MessageCandidates(r.Context(), workspace, account, scope.NativeGroup, "")
	if err != nil {
		return nativeSettingsError(w, err)
	}
	items := []map[string]any{}
	for _, candidate := range candidates {
		items = append(items, map[string]any{"event_id": strconv.FormatInt(candidate.Reference.EventID, 10),
			"sender": candidate.Reference.Sender, "preview": candidate.Preview, "created_at": candidate.Reference.CreatedAt})
	}
	send(w, 200, map[string]any{"items": items, "read_only": true})
	return nil
}

// Only the retained event identifier comes from the browser. Native route,
// sender and digest are resolved from this account's authenticated sealed input.
func (s *Server) testRecall(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Account string `json:"account_id"`
		Window  string `json:"test_window_id"`
		Event   string `json:"event_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	event, err := strconv.ParseInt(in.Event, 10, 64)
	workspace := requestWorkspace(r)
	if err != nil || event < 1 || workspace != "wsl" && workspace != "lajiaoliao" || s.Receiver == nil {
		return fail(400, "native_test_recall_required")
	}
	service, ref := s.nativeSettingsService(), assistantRef(r)
	if existing, e := service.Inspect(r.Context(), ref, in.Account, "", r.Header.Get("Idempotency-Key")); e == nil {
		if existing.Kind != "recall_message" {
			return fail(409, "idempotency_conflict")
		}
		send(w, 200, existing)
		return nil
	} else {
		var denied *control.Failure
		if !errors.As(e, &denied) || denied.Code != "operation_not_found" {
			return e
		}
	}
	scope, err := service.TestScope(r.Context(), ref, workspace, in.Window, in.Account, "recall_message", "")
	if err != nil {
		return err
	}
	candidates, err := s.Receiver.MessageCandidates(r.Context(), workspace, in.Account, scope.NativeGroup, "")
	if err != nil {
		return nativeSettingsError(w, err)
	}
	var reference *nativeauth.MessageReference
	for _, candidate := range candidates {
		if candidate.Reference.EventID == event {
			copy := candidate.Reference
			reference = &copy
		}
	}
	if reference == nil {
		return fail(409, "native_message_reference_expired")
	}
	if _, err = service.TestScope(r.Context(), ref, workspace, in.Window, in.Account, "recall_message", reference.Sender); err != nil {
		return err
	}
	snapshot, err := s.Receiver.Moderation(r.Context(), in.Account, scope.NativeGroup)
	if err != nil {
		return nativeSettingsError(w, err)
	}
	encoded, _ := json.Marshal(reference)
	defer clear(encoded)
	req := control.SettingRequest{Account: in.Account, Workspace: workspace, TestWindowID: in.Window,
		Key: r.Header.Get("Idempotency-Key"), Digest: snapshot.Digest,
		Epoch: snapshot.AccountEpoch, Version: snapshot.AccountVersion,
		Change: nativeauth.SettingChange{Kind: "recall_message", Group: scope.NativeGroup, Text: string(encoded)}}
	if err = s.DB.Platform.QueryRow(r.Context(), `SELECT owner,revision FROM account_receivers WHERE account_id=$1`,
		in.Account).Scan(&req.ReceiverOwner, &req.ReceiverRevision); err != nil {
		return err
	}
	out, err := service.ExecuteTestRecall(r.Context(), ref, req, func(work context.Context, fence control.SettingFence) error {
		operation, err := service.Inspect(work, ref, in.Account, "", req.Key)
		if err != nil || operation.Status != "preparing" {
			return control.Denied(409, "native_setting_expired")
		}
		current, err := s.Receiver.Moderation(work, in.Account, scope.NativeGroup)
		if err != nil {
			return err
		}
		proof := settingEvidence(current.SettingSnapshot)
		proof.Target, proof.TargetRole = reference.Sender, current.TargetRole(reference.Sender)
		intent := gateway.AssistantMessageIntent{
			Account: in.Account, Owner: req.ReceiverOwner, Revision: req.ReceiverRevision,
			Epoch: req.Epoch, Sender: proof.UID, Group: scope.NativeGroup, Reference: reference,
			ClientID: operation.ID,
		}
		guard := func(final context.Context, submit func(context.Context) error) error {
			return fence(final, proof, submit)
		}
		if workspace == "lajiaoliao" {
			return s.Receiver.SubmitAssistantChiliMessage(work, intent, guard)
		}
		return s.Receiver.RecallAssistantWSL(work, intent, guard)
	})
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}
