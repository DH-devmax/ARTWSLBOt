package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/gateway"
)

// Tests construct Server without starting native network connections.
func (s *Server) RunReceiver(ctx context.Context) <-chan struct{} {
	s.Receiver = gateway.NewReceiverManager(s.DB.Platform, s.Vault, s.nativeRequestIDs)
	done := make(chan struct{})
	go func() { defer close(done); s.Receiver.Run(ctx) }()
	return done
}

type receiverView struct {
	Enabled    bool                      `json:"enabled"`
	Revision   int64                     `json:"revision"`
	Observed   bool                      `json:"observed"`
	ObservedAt *time.Time                `json:"observed_at"`
	Snapshot   *gateway.ReceiverSnapshot `json:"snapshot"`
}

func (s *Server) readReceiver(r *http.Request, account string) (receiverView, error) {
	var out receiverView
	var raw []byte
	var leased bool
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT enabled,revision,observed_at,snapshot,
        coalesce(lease_until>now() AND observed_at>now()-interval '5 seconds',false)
        FROM account_receivers WHERE account_id=$1`, account).Scan(
		&out.Enabled, &out.Revision, &out.ObservedAt, &raw, &leased)
	if err == pgx.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if len(raw) > 2 && json.Unmarshal(raw, &out.Snapshot) == nil {
		out.Observed = leased && out.Enabled
	}
	return out, nil
}

func (s *Server) receiver(w http.ResponseWriter, r *http.Request, p Principal) error {
	out, err := s.readReceiver(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) setReceiver(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Enabled  bool  `json:"enabled"`
		Revision int64 `json:"revision"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	s.accountLifecycle.Lock()
	defer s.accountLifecycle.Unlock()
	if in.Enabled && s.Native.ActiveAccount(r.PathValue("id")) {
		return fail(409, "native_auth_busy")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798429)`); err != nil {
		return err
	}
	var authenticated bool
	if err = tx.QueryRow(r.Context(), `SELECT auth_state='authenticated' AND credential_sealed IS NOT NULL
        FROM accounts WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&authenticated); err != nil {
		return err
	}
	if in.Enabled && !authenticated {
		return fail(409, "native_login_required")
	}
	if in.Enabled {
		if s.Receiver == nil {
			return fail(503, "receiver_unavailable")
		}
		var configured bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM native_im_profile)`).Scan(&configured); err != nil {
			return err
		}
		if !configured {
			return fail(409, "native_im_profile_missing")
		}
	}
	var revision int64
	err = tx.QueryRow(r.Context(), `SELECT revision FROM account_receivers WHERE account_id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&revision)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if revision != in.Revision || in.Revision < 0 {
		return fail(409, "version_conflict")
	}
	if err = tx.QueryRow(r.Context(), `INSERT INTO account_receivers(account_id,enabled)
        VALUES($1,$2) ON CONFLICT(account_id) DO UPDATE SET enabled=excluded.enabled,revision=account_receivers.revision+1,
        snapshot=CASE WHEN excluded.enabled THEN '{}'::jsonb ELSE account_receivers.snapshot END,
        observed_at=CASE WHEN excluded.enabled THEN NULL ELSE account_receivers.observed_at END
        RETURNING revision`, r.PathValue("id"), in.Enabled).Scan(&revision); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "native_receiver_changed", r.PathValue("id"),
		map[string]any{"enabled": in.Enabled, "revision": revision}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	if s.Receiver != nil {
		s.Receiver.Stop(r.PathValue("id"))
	}
	send(w, 200, map[string]any{"enabled": in.Enabled, "revision": revision})
	return nil
}

var imKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,128}$`)

func (s *Server) imProfile(w http.ResponseWriter, r *http.Request, p Principal) error {
	var version int64
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT version FROM native_im_profile`).Scan(&version)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	send(w, 200, map[string]any{"configured": version > 0, "version": version})
	return nil
}

func (s *Server) saveIMProfile(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64  `json:"version"`
		AppKey  string `json:"app_key"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !imKeyPattern.MatchString(in.AppKey) {
		return fail(400, "native_im_profile_invalid")
	}
	plain, _ := json.Marshal(map[string]string{"app_key": in.AppKey})
	sealed, err := s.Vault.Seal("native-im-profile:v1", plain)
	clear(plain)
	if err != nil {
		return err
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798429)`); err != nil {
		return err
	}
	var active bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM account_receivers WHERE enabled)`).Scan(&active); err != nil {
		return err
	}
	if active {
		return fail(409, "native_disconnect_before_profile")
	}
	var version int64
	err = tx.QueryRow(r.Context(), `SELECT version FROM native_im_profile FOR UPDATE`).Scan(&version)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if version != in.Version || in.Version < 0 {
		return fail(409, "version_conflict")
	}
	if err = tx.QueryRow(r.Context(), `INSERT INTO native_im_profile(singleton,sealed) VALUES(true,$1)
        ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=native_im_profile.version+1
        RETURNING version`, sealed).Scan(&version); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "native_im_profile_updated", "platform", map[string]any{"version": version}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"configured": true, "version": version})
	return nil
}
