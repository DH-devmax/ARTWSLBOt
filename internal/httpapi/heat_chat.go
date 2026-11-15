package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/heatbot"
	"wangshangliao/internal/nativewire"
)

func (s *Server) RunHeatChat(ctx context.Context) (<-chan struct{}, error) {
	if err := s.HeatChat.Recover(ctx); err != nil {
		return nil, err
	}
	if s.Receiver == nil {
		return nil, errors.New("heat_chat_receiver_unavailable")
	}
	s.HeatChat.Cache = s.Receiver.ReadCache
	s.HeatChat.Conversation = func(work context.Context, scope control.HeatScope) (string, error) {
		return s.Receiver.ChatConversation(work, scope.Platform, scope.Account, scope.NativeGroup)
	}
	s.HeatChat.Decode = func(scope control.HeatScope, key string, event int64, projection, source []byte) (control.HeatMessage, error) {
		m, err := s.Receiver.ReadChatSource(scope.Platform, scope.Account, key, event, projection, source)
		return control.HeatMessage{Group: m.Group, Sender: m.Sender, Text: m.Text, ClientID: m.ClientID,
			ReplyClientID: m.ReplyClientID, Mentions: m.Mentions}, err
	}
	s.HeatChat.FreshRole = func(work context.Context, scope control.HeatScope) error {
		snapshot, err := s.Receiver.ChatRole(work, scope.Platform, scope.Account, scope.NativeGroup)
		if err != nil {
			return err
		}
		if snapshot.AccountEpoch != scope.AccountEpoch || snapshot.SelfID != scope.NativeSelf ||
			snapshot.SelfRole != "member" && snapshot.SelfRole != "admin" && snapshot.SelfRole != "owner" {
			return control.Denied(409, "heat_chat_native_role_unavailable")
		}
		_, err = control.ApplyNativeRole(work, s.DB.Platform, scope.Account, scope.Group, snapshot.SelfRole, snapshot.SampledAt)
		return err
	}
	token := os.Getenv("AI_BRIDGE_TOKEN")
	client := &http.Client{Timeout: 95 * time.Second, Transport: &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second,
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("heat_chat_redirect_rejected") }}
	s.HeatChat.Generate = func(work context.Context, in heatbot.Request) (heatbot.Proposal, error) {
		if len(token) < 32 {
			return heatbot.Proposal{}, errors.New("heat_chat_bridge_unavailable")
		}
		raw, err := json.Marshal(in)
		if err != nil || len(raw) > 64<<10 {
			return heatbot.Proposal{}, errors.New("heat_chat_request_limit")
		}
		defer clear(raw)
		req, err := http.NewRequestWithContext(work, http.MethodPost, "http://heatbot:8090/internal/v1/chat/generate", bytes.NewReader(raw))
		if err != nil {
			return heatbot.Proposal{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return heatbot.Proposal{}, errors.New("heat_chat_generation_unconfirmed")
		}
		defer res.Body.Close()
		raw, err = io.ReadAll(io.LimitReader(res.Body, 32<<10+1))
		if err != nil || len(raw) > 32<<10 || res.StatusCode != http.StatusOK {
			clear(raw)
			return heatbot.Proposal{}, errors.New("heat_chat_generation_unconfirmed")
		}
		defer clear(raw)
		var out heatbot.Proposal
		if json.Unmarshal(raw, &out) != nil {
			return out, errors.New("heat_chat_generation_unconfirmed")
		}
		return out, nil
	}
	s.HeatChat.Send = s.sendHeatText
	done := s.HeatChat.Run(ctx)
	go func() { <-done; client.CloseIdleConnections() }()
	return done, nil
}

func (s *Server) sendHeatText(ctx context.Context, in control.HeatSend,
	fence func(context.Context, func(context.Context) error) error) (bool, error) {
	scope := in.Scope
	if scope.Platform == "lajiaoliao" {
		receipt, err := s.Receiver.SubmitChiliGroupText(ctx, gateway.AssistantMessageIntent{
			Account: scope.Account, Owner: in.Owner, Sender: scope.NativeSelf, Group: scope.NativeGroup,
			ClientID: in.ClientID, Text: in.Text, Epoch: scope.AccountEpoch, Revision: in.Revision,
		}, fence)
		return err == nil && receipt.ClientID == in.ClientID && receipt.ServerID != "", err
	}
	if scope.Platform != "wsl" {
		return false, control.Denied(409, "heat_chat_platform_invalid")
	}
	sender, err := strconv.ParseUint(scope.NativeSelf, 10, 30)
	if err != nil {
		return false, err
	}
	group, err := strconv.ParseUint(scope.NativeGroup, 10, 30)
	if err != nil {
		return false, err
	}
	result, err := s.Receiver.SubmitText(ctx, gateway.TextIntent{
		AccountID: scope.Account, ReceiverOwner: in.Owner, AccountEpoch: scope.AccountEpoch, Revision: in.Revision,
		Message: nativewire.GroupText{SenderID: sender, GroupID: group, ClientID: in.ClientID, Text: in.Text, CreatedAt: in.MessageAt},
		Nonce:   in.Nonce, AAD: in.AAD, RecordRoute: in.RecordRoute,
	}, fence)
	return err == nil && result.Attempted && result.Code == 200 &&
		result.Receipt.ClientID == in.ClientID && result.Receipt.ServerID != "", err
}

func (s *Server) heatChatRuntime(w http.ResponseWriter, r *http.Request, p Principal) error {
	group := r.PathValue("id")
	if !control.ValidID(group) {
		return fail(400, "invalid_group")
	}
	if err := s.heatChatRead(r.Context(), p, group); err != nil {
		return err
	}
	view := s.HeatChat.View(r.Context(), group)
	// A late result cannot disclose a group after its access was revoked.
	if err := s.heatChatRead(r.Context(), p, group); err != nil {
		return err
	}
	send(w, 200, view)
	return nil
}

func (s *Server) heatChatRead(ctx context.Context, p Principal, group string) error {
	a := principalActor(p)
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	grants, err := control.LiveActorPermissions(ctx, tx, a, tenantPermissions)
	if err != nil {
		return err
	}
	if p.Role != "superadmin" && !slices.Contains(grants, "groups:ai") {
		return fail(403, "permission_denied")
	}
	if err = control.RequireGroup(ctx, tx, a, group); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) heatAdmission(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Platform string `json:"platform"`
		Enabled  bool   `json:"enabled"`
		Version  int64  `json:"version"`
		Evidence string `json:"evidence_sha256"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	version, err := s.HeatChat.SetHeatAdmission(r.Context(), assistantRef(r), in.Platform, in.Enabled, in.Version, in.Evidence)
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"version": version, "enabled": in.Enabled, "native_ready": false, "ai_ready": false})
	return nil
}

func (s *Server) reconcileHeatChat(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := s.HeatChat.Reconcile(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	send(w, 200, map[string]bool{"reconciled": true})
	return nil
}
