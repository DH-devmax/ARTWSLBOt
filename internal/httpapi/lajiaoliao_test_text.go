package httpapi

import (
	"context"
	"net/http"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

// This endpoint has no assistant/production admission and no arbitrary route.
// Its client message ID is the existing durable operation ID.
func (s *Server) testChiliText(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Account string `json:"account_id"`
		Window  string `json:"test_window_id"`
		Text    string `json:"text"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if requestWorkspace(r) != "lajiaoliao" || s.Receiver == nil || !control.ValidID(in.Account) {
		return fail(400, "native_test_text_required")
	}
	service, ref := s.nativeSettingsService(), assistantRef(r)
	change := nativeauth.SettingChange{Kind: "send_text", Text: in.Text}
	if old, found, err := service.InspectTestTool(r.Context(), ref, in.Account,
		r.Header.Get("Idempotency-Key"), in.Window, change); err != nil {
		return err
	} else if found {
		send(w, 200, old)
		return nil
	}
	scope, err := service.TestScope(r.Context(), ref, "lajiaoliao", in.Window, in.Account, "send_text", "")
	if err != nil {
		return err
	}
	change.Group = scope.NativeGroup
	if err = control.ValidateNativeChange("lajiaoliao", change); err != nil {
		return err
	}
	snapshot, err := s.Receiver.Moderation(r.Context(), in.Account, scope.NativeGroup)
	if err != nil {
		return nativeSettingsError(w, err)
	}
	req := control.SettingRequest{Account: in.Account, Workspace: "lajiaoliao", TestWindowID: in.Window,
		Key: r.Header.Get("Idempotency-Key"), Digest: snapshot.Digest,
		Epoch: snapshot.AccountEpoch, Version: snapshot.AccountVersion, Change: change}
	if err = s.DB.Platform.QueryRow(r.Context(), `SELECT owner,revision FROM account_receivers WHERE account_id=$1`,
		in.Account).Scan(&req.ReceiverOwner, &req.ReceiverRevision); err != nil {
		return err
	}
	out, err := service.ExecuteTestChiliText(r.Context(), ref, req, func(work context.Context, fence control.SettingFence) error {
		operation, err := service.Inspect(work, ref, in.Account, "", req.Key)
		if err != nil || operation.Status != "preparing" {
			return control.Denied(409, "native_setting_expired")
		}
		current, err := s.Receiver.Moderation(work, in.Account, scope.NativeGroup)
		if err != nil {
			return err
		}
		proof := settingEvidence(current.SettingSnapshot)
		proof.ModerationAllowed = current.Present
		return s.Receiver.SubmitAssistantChiliMessage(work, gateway.AssistantMessageIntent{
			Account: in.Account, Owner: req.ReceiverOwner, Revision: req.ReceiverRevision,
			Epoch: req.Epoch, Sender: proof.UID, Group: scope.NativeGroup,
			ClientID: operation.ID, Text: in.Text,
		}, func(final context.Context, submit func(context.Context) error) error {
			return fence(final, proof, submit)
		})
	})
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}
