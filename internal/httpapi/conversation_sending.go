package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativewire"
)

const manualTextLimit = 3000 // Leaves room for authenticated application metadata.

type conversationSendScope struct {
	Platform            string                   `json:"platform"`
	GroupID             string                   `json:"group_id"`
	AccountEpoch        int64                    `json:"account_epoch"`
	BindingEpoch        int64                    `json:"binding_epoch"`
	Scene               string                   `json:"scene"`
	PrivateConversation string                   `json:"-"`
	PrivateEventID      int64                    `json:"-"`
	PrivatePeer         nativewire.PrivateTarget `json:"-"`
	ChiliPeer           string                   `json:"-"`
	proofWindowID       string
}

// Resolve the target from an authenticated, account-scoped projection, never a
// browser-supplied native ID, transport route or tenant.
func (s *Server) conversationSendScope(r *http.Request) (conversationSendScope, error) {
	var out conversationSendScope
	if !conversationPattern.MatchString(r.PathValue("conversation")) {
		return out, fail(400, "invalid_conversation")
	}
	var platform, workspace, scene string
	var event int64
	var sealed []byte
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT a.platform,a.workspace,a.epoch,
        d.event_id,d.scene,d.payload_sealed FROM accounts a JOIN native_decoded_events d ON d.account_id=a.id
        WHERE a.id=$1 AND d.conversation_key=$2 AND (d.state='decoded' OR
            (a.platform='lajiaoliao' AND d.scene='private' AND d.state='unsupported'
            AND d.reason='lajiaoliao_content_type'))
        AND d.received_at>clock_timestamp()-interval '7 days'
        ORDER BY d.event_id DESC LIMIT 1`, r.PathValue("id"), r.PathValue("conversation")).
		Scan(&platform, &workspace, &out.AccountEpoch, &event, &scene, &sealed)
	if err == pgx.ErrNoRows {
		return out, fail(404, "conversation_not_found")
	}
	if err != nil {
		return out, err
	}
	if workspace != requestWorkspace(r) {
		return out, fail(404, "account_not_found")
	}
	if platform != workspace || platform != "wsl" && platform != "lajiaoliao" {
		return out, fail(409, "native_text_platform_unavailable")
	}
	out.Scene, out.Platform = scene, platform
	if scene == "private" {
		out.PrivateConversation = r.PathValue("conversation")
		if platform == "lajiaoliao" {
			out.ChiliPeer, out.PrivateEventID, err = s.nativeTextService().ChiliPrivateConversation(
				r.Context(), assistantRef(r), r.PathValue("id"), out.PrivateConversation)
			return out, err
		}
		out.PrivatePeer, out.PrivateEventID, err = s.nativeTextService().PrivateConversation(
			r.Context(), assistantRef(r), r.PathValue("id"), out.PrivateConversation)
		return out, err
	}
	if scene != "group" {
		return out, fail(409, "native_text_platform_unavailable")
	}
	plain, err := s.Vault.Open(gateway.DecodedAAD(r.PathValue("id"), event), sealed)
	if err != nil {
		return out, fail(503, "native_projection_unreadable")
	}
	var decoded gateway.DecodedMessage
	err = json.Unmarshal(plain, &decoded)
	clear(plain)
	if err != nil {
		return out, fail(503, "native_projection_unreadable")
	}
	target := decoded.NativeTarget
	if platform == "wsl" {
		target = strconv.FormatUint(decoded.Message.Target.ID, 10)
	}
	if target == "" {
		return out, fail(409, "native_text_group_unbound")
	}
	actor, _ := r.Context().Value(contextKey{}).(Principal)
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT g.id::text,b.epoch FROM native_groups g
        JOIN group_bindings b ON b.group_id=g.id WHERE g.platform=$3 AND g.native_id=$1
        AND (b.primary_account_id=$2 OR g.platform='wsl' AND EXISTS(SELECT 1 FROM test_windows w
            WHERE w.group_id=g.id AND w.actor_id::text=$4 AND $2::uuid=ANY(w.account_ids)
            AND w.admission_mode='native_debug' AND w.actions=ARRAY['send_text']::text[]
            AND w.account_epochs->>($2::text)=$5::text))`,
		target, r.PathValue("id"), platform, actor.ID, strconv.FormatInt(out.AccountEpoch, 10)).
		Scan(&out.GroupID, &out.BindingEpoch)
	if err == pgx.ErrNoRows && platform == "lajiaoliao" {
		proofs, proofErr := s.nativeTextService().OnboardingControlledProofWindows(
			r.Context(), assistantRef(r), r.PathValue("id"), "group", target)
		if proofErr != nil {
			return out, proofErr
		}
		if len(proofs) != 0 {
			out.GroupID, out.BindingEpoch = proofs[0].GroupID, proofs[0].BindingEpoch
			out.proofWindowID = proofs[0].ID
			return out, nil
		}
	}
	if err == pgx.ErrNoRows {
		return out, fail(409, "native_text_group_unbound")
	}
	return out, err
}

// A private conversation has no group ownership. Its human test window supplies
// the independent authorization group, never the recipient or transport route.
func (s *Server) conversationTextRequest(r *http.Request, p Principal, scope conversationSendScope,
	window string, epoch, binding int64) (control.TextRequest, error) {
	out := control.TextRequest{AccountID: r.PathValue("id"), GroupID: scope.GroupID, TestWindowID: window,
		AccountEpoch: epoch, BindingEpoch: binding, IdempotencyKey: r.Header.Get("Idempotency-Key"),
		PrivateConversation: scope.PrivateConversation, PrivateEventID: scope.PrivateEventID}
	if epoch != scope.AccountEpoch {
		return out, fail(409, "native_text_account_changed")
	}
	expected := scope.BindingEpoch
	if scope.PrivateConversation != "" {
		if !control.ValidID(window) {
			return out, fail(400, "invalid_test_scope")
		}
		peer := scope.ChiliPeer
		if scope.Platform == "wsl" {
			peer = strconv.FormatUint(scope.PrivatePeer.ID, 10)
		}
		err := s.DB.Platform.QueryRow(r.Context(), `SELECT w.group_id::text,b.epoch FROM test_windows w
            JOIN group_bindings b ON b.group_id=w.group_id
            WHERE w.id=$1 AND w.actor_id=$2 AND $3::uuid=ANY(w.account_ids) AND $4=ANY(w.target_ids)`,
			window, p.ID, r.PathValue("id"), peer).Scan(&out.GroupID, &expected)
		if err == pgx.ErrNoRows {
			return out, fail(409, "native_text_test_target_invalid")
		}
		if err != nil {
			return out, err
		}
	}
	if binding != expected {
		return out, fail(409, "native_text_binding_changed")
	}
	return out, nil
}

func (s *Server) nativeTextService() *control.NativeTextService {
	return &control.NativeTextService{DB: s.DB, Vault: s.Vault}
}

func (s *Server) chiliSendingChoices(w http.ResponseWriter, r *http.Request, p Principal) error {
	scope, err := s.conversationSendScope(r)
	if err != nil {
		return err
	}
	if scope.Platform != "lajiaoliao" || scope.Scene != "group" {
		return fail(409, "native_text_platform_unavailable")
	}
	request, err := s.conversationTextRequest(r, p, scope, r.URL.Query().Get("window"),
		scope.AccountEpoch, scope.BindingEpoch)
	if err != nil {
		return err
	}
	if scope.proofWindowID != "" && request.TestWindowID == scope.proofWindowID {
		send(w, 200, map[string]any{"members": []any{}, "quotes": []any{}})
		return nil
	}
	request.IdempotencyKey = "read-only-choices"
	members, quotes, err := s.nativeTextService().ChiliSendChoices(r.Context(), assistantRef(r), request, r.PathValue("conversation"))
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"members": members, "quotes": quotes})
	return nil
}

func (s *Server) nativeSending(w http.ResponseWriter, r *http.Request, p Principal) error {
	scope, err := s.conversationSendScope(r)
	if err != nil {
		var failure *apiError
		if errors.As(err, &failure) && failure.status == 409 {
			send(w, 200, map[string]any{"enabled": false, "reason": failure.code,
				"max_text_bytes": manualTextLimit, "mode": "unavailable", "native_ready": false})
			return nil
		}
		return err
	}
	type window struct {
		ID           string    `json:"id"`
		ExpiresAt    time.Time `json:"expires_at"`
		Remaining    int       `json:"remaining"`
		Actions      []string  `json:"actions"`
		BindingEpoch int64     `json:"binding_epoch"`
		GroupName    string    `json:"group_name"`
		ProofKind    string    `json:"proof_kind,omitempty"`
	}
	windows := []window{}
	peer := ""
	if scope.PrivateConversation != "" {
		peer = scope.ChiliPeer
		if scope.Platform == "wsl" {
			peer = strconv.FormatUint(scope.PrivatePeer.ID, 10)
		}
	}
	rows, err := s.DB.Platform.Query(r.Context(), `SELECT w.id::text,w.expires_at,w.budget-w.used,w.actions,b.epoch,g.name
        FROM test_windows w JOIN group_bindings b ON b.group_id=w.group_id JOIN native_groups g ON g.id=w.group_id
        WHERE w.actor_id=$1 AND ($2='' OR w.group_id::text=$2) AND $3::uuid=ANY(w.account_ids)
        AND ($5='' OR $5=ANY(w.target_ids)) AND b.active
        AND (w.admission_mode='native_debug' OR b.management_enabled)
        AND (b.primary_account_id=$3 OR g.platform='wsl' AND w.admission_mode='native_debug' AND w.actions=ARRAY['send_text']::text[])
        AND NOT EXISTS(SELECT 1 FROM unnest(w.target_ids) AS t(value) WHERE starts_with(t.value,'onboarding_proof:'))
        AND g.platform=$6 AND g.isolated_test
        AND (($5='' AND actions && ARRAY['send_text','send_image','send_voice','send_video','send_file',
            'upload_image','upload_voice','upload_video']::text[])
            OR ($5<>'' AND actions && ARRAY['send_private_text','send_private_image','upload_private_image']::text[]))
        AND NOT w.cancelled AND w.used<w.budget
        AND w.account_epochs->>($3::text)=$4::text
        AND w.expires_at>clock_timestamp()+interval '3 seconds' ORDER BY w.created_at DESC LIMIT 10`,
		p.ID, scope.GroupID, r.PathValue("id"), strconv.FormatInt(scope.AccountEpoch, 10), peer, scope.Platform)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item window
		if err = rows.Scan(&item.ID, &item.ExpiresAt, &item.Remaining, &item.Actions, &item.BindingEpoch, &item.GroupName); err != nil {
			rows.Close()
			return err
		}
		windows = append(windows, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	proofKind, proofTarget := "group", ""
	if scope.PrivateConversation != "" {
		proofKind, proofTarget = "private", peer
	} else if scope.GroupID != "" {
		if err = s.DB.Platform.QueryRow(r.Context(), `SELECT native_id FROM native_groups WHERE id=$1 AND platform=$2`,
			scope.GroupID, scope.Platform).Scan(&proofTarget); err != nil {
			return err
		}
	}
	if proofTarget != "" {
		proofs, err := s.nativeTextService().OnboardingControlledProofWindows(
			r.Context(), assistantRef(r), r.PathValue("id"), proofKind, proofTarget)
		if err != nil {
			return err
		}
		for _, proof := range proofs {
			if len(windows) >= 10 {
				break
			}
			if proof.Remaining != 1 || proof.AccountEpoch != scope.AccountEpoch ||
				scope.GroupID != "" && (proof.GroupID != scope.GroupID || proof.BindingEpoch != scope.BindingEpoch) {
				continue
			}
			action := "send_text"
			if proofKind == "private" {
				action = "send_private_text"
			}
			windows = append(windows, window{ID: proof.ID, ExpiresAt: proof.ExpiresAt,
				Remaining: proof.Remaining, Actions: []string{action}, BindingEpoch: proof.BindingEpoch,
				GroupName: proof.GroupName, ProofKind: proof.ProofKind})
		}
	}
	type recentSend struct {
		control.TextOperation
		Kind         string `json:"kind"`
		TestWindowID string `json:"test_window_id"`
	}
	recent := []recentSend{}
	rows, err = s.DB.Platform.Query(r.Context(), `SELECT o.id::text,o.status,o.result_code,o.kind,o.test_window_id::text FROM operations o
        WHERE o.actor_id=$1 AND o.account_id=$2 AND ($3='' OR o.group_id::text=$3) AND o.conversation_key=$4
        AND (EXISTS(SELECT 1 FROM native_text_attempts t WHERE t.operation_id=o.id)
            OR EXISTS(SELECT 1 FROM lajiaoliao_media_attempts t WHERE t.operation_id=o.id))
        AND o.kind IN('send_text','send_image','send_voice','send_video','send_file')
        ORDER BY o.created_at DESC LIMIT 10`, p.ID, r.PathValue("id"), scope.GroupID, scope.PrivateConversation)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item recentSend
		if err = rows.Scan(&item.ID, &item.Status, &item.ResultCode, &item.Kind, &item.TestWindowID); err != nil {
			return err
		}
		recent = append(recent, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	uploads := []control.MediaUploadOperation{}
	uploadRows, err := s.DB.Platform.Query(r.Context(), `SELECT id::text,status,result_code,kind,test_window_id::text
        FROM native_media_uploads WHERE actor_id=$1 AND account_id=$2 AND ($3='' OR group_id::text=$3) AND conversation_key=$4
        AND operation_id IS NULL AND expires_at>clock_timestamp() ORDER BY created_at DESC LIMIT 10`,
		p.ID, r.PathValue("id"), scope.GroupID, scope.PrivateConversation)
	if err != nil {
		return err
	}
	for uploadRows.Next() {
		var item control.MediaUploadOperation
		if err = uploadRows.Scan(&item.ID, &item.Status, &item.ResultCode, &item.Kind, &item.TestWindowID); err != nil {
			uploadRows.Close()
			return err
		}
		uploads = append(uploads, item)
	}
	err = uploadRows.Err()
	uploadRows.Close()
	if err != nil {
		return err
	}
	reason := ""
	if scope.Platform == "lajiaoliao" && s.chiliMediaTestPort == nil {
		reason = "native_media_acceptance_pending"
	} else if scope.Platform == "wsl" && s.nativeTextTestPort == nil {
		reason = "native_text_acceptance_pending"
	} else if len(windows) == 0 {
		reason = "native_text_test_window_required"
	}
	mediaKinds := []string{"image", "voice", "video"}
	if scope.PrivateConversation != "" {
		mediaKinds = []string{"image"}
	}
	send(w, 200, map[string]any{"enabled": reason == "", "reason": reason, "scope": scope,
		"mode": "isolated_test", "test_windows": windows, "recent": recent,
		"recent_uploads": uploads,
		"media_enabled":  s.mediaPortAvailable(scope.Platform),
		"upload_enabled": s.mediaPortAvailable(scope.Platform) && len(windows) > 0,
		"media_kinds":    mediaKinds,
		"media_limits": map[string]any{"image": nativewire.MaxImageBytes, "voice": nativewire.MaxUploadBytes,
			"video": nativewire.MaxUploadBytes},
		"max_text_bytes": manualTextLimit, "sampled_at": time.Now().UTC(), "native_ready": false})
	return nil
}

func (s *Server) stageNativeText(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) == "lajiaoliao" && s.chiliMediaTestPort == nil ||
		requestWorkspace(r) != "lajiaoliao" && s.nativeTextTestPort == nil {
		return fail(409, "native_text_acceptance_pending")
	}
	scope, err := s.conversationSendScope(r)
	if err != nil {
		return err
	}
	var in struct {
		Text         string                      `json:"text"`
		TestWindowID string                      `json:"test_window_id"`
		AccountEpoch int64                       `json:"account_epoch"`
		BindingEpoch int64                       `json:"binding_epoch"`
		ReplyTo      string                      `json:"reply_to"`
		Mentions     []nativewire.MessageMention `json:"mentions"`
		ChiliMention string                      `json:"chili_mention_id"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	if len(in.Text) > manualTextLimit || !utf8.ValidString(in.Text) {
		return fail(400, "invalid_text_message")
	}
	var quoteEvent int64
	if in.ReplyTo != "" {
		quoteEvent, err = strconv.ParseInt(in.ReplyTo, 10, 64)
		if err != nil || quoteEvent < 1 || strconv.FormatInt(quoteEvent, 10) != in.ReplyTo {
			return fail(400, "invalid_message_reference")
		}
	}
	request, err := s.conversationTextRequest(r, p, scope, in.TestWindowID, in.AccountEpoch, in.BindingEpoch)
	if err != nil {
		return err
	}
	request.Text, request.Mentions, request.QuoteEventID = in.Text, in.Mentions, quoteEvent
	request.ChiliMentionID = in.ChiliMention
	var out control.TextOperation
	if scope.Platform == "lajiaoliao" {
		out, err = s.nativeTextService().StageChiliMessage(r.Context(), assistantRef(r), request)
	} else {
		out, err = s.nativeTextService().Stage(r.Context(), assistantRef(r), request)
	}
	if err != nil {
		return err
	}
	send(w, 201, out)
	return nil
}

func (s *Server) nativeTextOperation(r *http.Request) (control.TextOperation, error) {
	scope, err := s.conversationSendScope(r)
	if err != nil {
		return control.TextOperation{}, err
	}
	if scope.PrivateConversation != "" {
		return s.nativeTextService().InspectConversation(r.Context(), assistantRef(r), r.PathValue("id"), scope.PrivateConversation,
			r.PathValue("operation"), r.URL.Query().Get("key"))
	}
	return s.nativeTextService().Inspect(r.Context(), assistantRef(r), r.PathValue("id"), scope.GroupID,
		r.PathValue("operation"), r.URL.Query().Get("key"))
}

func (s *Server) inspectNativeText(w http.ResponseWriter, r *http.Request, p Principal) error {
	out, err := s.nativeTextOperation(r)
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) dispatchNativeText(w http.ResponseWriter, r *http.Request, p Principal) error {
	if s.nativeTextTestPort == nil && requestWorkspace(r) != "lajiaoliao" ||
		requestWorkspace(r) == "lajiaoliao" && s.chiliMediaTestPort == nil {
		return fail(409, "native_text_acceptance_pending")
	}
	if !control.ValidID(r.PathValue("operation")) {
		return fail(400, "invalid_operation")
	}
	if _, err := s.nativeTextOperation(r); err != nil {
		return err
	}
	var out control.TextOperation
	var err error
	service := s.nativeTextService()
	if requestWorkspace(r) == "lajiaoliao" {
		out, err = service.DispatchChiliMedia(r.Context(), assistantRef(r), r.PathValue("operation"), s.chiliMediaTestPort)
	} else {
		out, err = service.Dispatch(r.Context(), assistantRef(r), r.PathValue("operation"), s.nativeTextTestPort)
	}
	if err != nil && out.Status != "unknown" {
		return err
	}
	// Preserve uncertainty as state, not an HTTP error that encourages a retry.
	send(w, 200, out)
	return nil
}

func (s *Server) reconcileNativeText(w http.ResponseWriter, r *http.Request, p Principal) error {
	if !control.ValidID(r.PathValue("operation")) {
		return fail(400, "invalid_operation")
	}
	if _, err := s.nativeTextOperation(r); err != nil {
		return err
	}
	if s.Receiver == nil {
		return fail(503, "native_text_evidence_unavailable")
	}
	var out control.TextOperation
	var err error
	if requestWorkspace(r) == "lajiaoliao" {
		out, err = s.nativeTextService().ReconcileChiliMedia(r.Context(), assistantRef(r), r.PathValue("operation"), s.Receiver)
	} else {
		out, err = s.nativeTextService().Reconcile(r.Context(), assistantRef(r), r.PathValue("operation"), s.Receiver)
	}
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}
