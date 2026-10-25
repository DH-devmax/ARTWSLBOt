package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/database"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/monitor"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type Principal struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id,omitempty"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type Server struct {
	DB               *database.DB
	Vault            *security.Vault
	Origin           string
	SecureCookie     bool
	mux              *http.ServeMux
	loginMu          sync.Mutex
	accountLifecycle sync.RWMutex
	loginSlots       chan struct{}
	loginAttempts    map[string]*loginAttempt
	dummyHash        string
	Native           *nativeauth.Manager
	Monitor          *monitor.Client
	Receiver         *gateway.ReceiverManager
}

type loginAttempt struct {
	count int
	until time.Time
}

type contextKey struct{}

type apiError struct {
	status int
	code   string
}

func (e *apiError) Error() string        { return e.code }
func fail(status int, code string) error { return &apiError{status: status, code: code} }

func New(db *database.DB, vault *security.Vault, origin string, secure bool) *Server {
	dummy, err := security.Password("unavailable-account-dummy-password")
	if err != nil {
		panic(err)
	}
	s := &Server{
		DB: db, Vault: vault, Origin: strings.TrimRight(origin, "/"), SecureCookie: secure,
		mux: http.NewServeMux(), loginSlots: make(chan struct{}, 2),
		loginAttempts: make(map[string]*loginAttempt), dummyHash: dummy,
		Native:  nativeauth.NewManager(),
		Monitor: monitor.New(os.Getenv("SIMON_URL")),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		requestID, err := security.Token()
		if err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-Request-ID", requestID)
		defer func() {
			if recover() != nil {
				slog.Error("request panic", "request_id", requestID, "path", r.URL.Path)
				sendError(w, fail(500, "internal_error"))
			}
		}()
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := s.DB.Platform.Ping(ctx); err != nil {
			sendError(w, fail(503, "database_unavailable"))
			return
		}
		send(w, 200, map[string]any{"status": "ok", "native_ready": false, "ai_ready": false})
	})
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.handle("POST /api/v1/auth/logout", false, s.logout)
	s.handle("GET /api/v1/me", false, s.me)
	s.handle("GET /api/v1/workspaces", false, s.workspaces)
	s.handle("GET /api/v1/access/catalog", true, s.accessCatalog)
	s.handle("GET /api/v1/users/{id}/access", true, s.userAccess)
	s.handle("PUT /api/v1/users/{id}/access", true, s.saveUserAccess)
	s.handle("GET /api/v1/overview", false, s.overview)
	s.handle("GET /api/v1/system/monitor", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		send(w, 200, s.Monitor.Read(r.Context()))
		return nil
	})
	s.handle("GET /api/v1/accounts", false, s.accounts)
	s.handle("GET /api/v1/accounts/{id}/dashboard", false, s.accountDashboard)
	s.handle("GET /api/v1/accounts/{id}/receiver", true, s.receiver)
	s.handle("PUT /api/v1/accounts/{id}/receiver", true, s.setReceiver)
	s.handle("GET /api/v1/native-im-profile", true, s.imProfile)
	s.handle("PUT /api/v1/native-im-profile", true, s.saveIMProfile)
	s.handle("POST /api/v1/accounts", true, s.createAccount)
	s.handle("PATCH /api/v1/accounts/{id}", true, s.updateAccount)
	s.handle("GET /api/v1/accounts/{id}/removal-preview", true, s.accountRemovalPreview)
	s.handle("DELETE /api/v1/accounts/{id}", true, s.removeAccount)
	s.handle("GET /api/v1/native-deployment", true, s.nativeDeployment)
	s.handle("PUT /api/v1/native-deployment", true, s.saveNativeDeployment)
	s.handle("POST /api/v1/accounts/{id}/native-auth", true, s.startNativeAuth)
	s.handle("GET /api/v1/accounts/{id}/native-auth/{transaction}", true, s.pollNativeAuth)
	s.handle("POST /api/v1/accounts/{id}/native-auth/{transaction}", true, s.nativeAuthAction)
	s.handle("DELETE /api/v1/accounts/{id}/native-auth/{transaction}", true, s.cancelNativeAuth)
	s.handle("POST /api/v1/accounts/{id}/forget-session", true, s.forgetNativeSession)
	s.handle("POST /api/v1/native-signup", true, s.startNativeSignup)
	s.handle("GET /api/v1/groups", false, s.groups)
	s.handle("PATCH /api/v1/groups/{id}", false, s.updateGroup)
	s.handle("PUT /api/v1/groups/{id}/ai-service", false, s.requestGroupAI)
	s.handle("POST /api/v1/groups/{id}/revoke", false, s.revokeGroup)
	s.handle("GET /api/v1/claims", false, s.claims)
	s.handle("POST /api/v1/claims", false, s.createClaim)
	s.handle("POST /api/v1/claims/{id}/review", true, s.reviewClaim)
	s.handle("GET /api/v1/users", true, s.users)
	s.handle("POST /api/v1/users", true, s.createUser)
	s.handle("PATCH /api/v1/users/{id}", true, s.updateUser)
	s.handle("GET /api/v1/plans", true, s.plans)
	s.handle("POST /api/v1/plans", true, s.createPlan)
	s.handle("PUT /api/v1/subscriptions/{id}", true, s.subscription)
	s.handle("GET /api/v1/personas", true, s.personas)
	s.handle("PUT /api/v1/personas/{id}", true, s.savePersona)
	s.handle("GET /api/v1/maibot/capabilities", true, s.maiBotCapabilities)
	s.handle("GET /api/v1/maibot/events", true, s.maiBotEvents)
	s.handle("GET /api/v1/accounts/{id}/maibot-settings/{section}", true, s.maiBotSettings)
	s.handle("PUT /api/v1/accounts/{id}/maibot-settings/{section}", true, s.saveMaiBotSettings)
	s.handle("GET /api/v1/operations", false, s.operations)
	s.handle("POST /api/v1/operations", false, s.createOperation)
	s.handle("POST /api/v1/operations/{id}/cancel", false, s.cancelOperation)
	s.handle("GET /api/v1/test-windows", true, s.testWindows)
	s.handle("POST /api/v1/test-windows", true, s.createTestWindow)
	s.handle("POST /api/v1/test-windows/{id}/cancel", true, s.cancelTestWindow)
	s.handle("GET /api/v1/audit", false, s.audit)
	s.handle("GET /api/v1/ledger", false, s.ledger)
	s.handle("POST /api/v1/ledger", false, s.createLedgerEntry)
	s.handle("GET /api/v1/activities", false, s.activities)
	s.handle("POST /api/v1/activities", false, s.createActivity)
	s.handle("POST /api/v1/activities/{id}/draw", false, s.drawActivity)
}

type endpoint func(http.ResponseWriter, *http.Request, Principal) error

func (s *Server) handle(pattern string, admin bool, f endpoint) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		authContext, authCancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer authCancel()
		r = r.WithContext(authContext)
		cookie, err := r.Cookie("wsl_session")
		if err != nil || len(cookie.Value) != 43 {
			sendError(w, fail(401, "authentication_required"))
			return
		}
		var p Principal
		var csrf []byte
		err = s.DB.Platform.QueryRow(r.Context(), `SELECT u.id::text,coalesce(u.tenant_id::text,''),
            u.username,u.role,se.csrf_hash FROM sessions se JOIN users u ON u.id=se.user_id
            WHERE se.token_hash=$1 AND se.expires_at>now() AND NOT u.disabled`,
			security.Digest(cookie.Value)).Scan(&p.ID, &p.TenantID, &p.Username, &p.Role, &csrf)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				sendError(w, fail(401, "authentication_required"))
			} else {
				sendError(w, fail(503, "database_unavailable"))
			}
			return
		}
		if admin && p.Role != "superadmin" {
			sendError(w, fail(403, "superadmin_required"))
			return
		}
		if id := r.PathValue("id"); id != "" && !validUUID(id) {
			sendError(w, fail(400, "invalid_resource_id"))
			return
		}
		if err := s.guardWorkspace(r, p, pattern); err != nil {
			sendError(w, err)
			return
		}
		if code := routePermissions[pattern]; code != "" && p.Role != "superadmin" {
			permissions, err := s.permissions(r.Context(), p)
			if err != nil {
				sendError(w, fail(503, "database_unavailable"))
				return
			}
			if !slices.Contains(permissions, code) {
				sendError(w, fail(403, "permission_denied"))
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.validOrigin(r) || subtle.ConstantTimeCompare(security.Digest(r.Header.Get("X-CSRF-Token")), csrf) != 1 {
				sendError(w, fail(403, "csrf_rejected"))
				return
			}
		}
		if err := f(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, p)), p); err != nil {
			sendError(w, err)
		}
	})
}

func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == s.Origin
}

func decode(w http.ResponseWriter, r *http.Request, target any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fail(415, "json_required")
	}
	reader := http.MaxBytesReader(w, r.Body, 64*1024)
	d := json.NewDecoder(reader)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fail(400, "invalid_request")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fail(400, "invalid_request")
	}
	return nil
}

func send(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": data})
}

func sendError(w http.ResponseWriter, err error) {
	var e *apiError
	if !errors.As(err, &e) {
		// Database details may contain user input; never expose them in responses.
		e = &apiError{status: 500, code: "internal_error"}
		slog.Error("request failed", "error_type", "internal")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.status)
	json.NewEncoder(w).Encode(map[string]any{"code": e.status, "msg": e.code, "data": nil})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, p Principal, sql string, args ...any) error {
	query := `SELECT coalesce(jsonb_agg(to_jsonb(result)),'[]'::jsonb) FROM (` + sql + `) result`
	var data []byte
	var err error
	if p.Role == "superadmin" {
		err = s.DB.Platform.QueryRow(r.Context(), query, args...).Scan(&data)
	} else {
		err = s.DB.TenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(r.Context(), query, args...).Scan(&data)
		})
	}
	if err != nil {
		return err
	}
	send(w, 200, json.RawMessage(data))
	return nil
}

func auditTx(ctx context.Context, tx pgx.Tx, p Principal, tenant, action, resource string, detail any) error {
	blob, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES(nullif($1,'')::uuid,$2,$3,$4,$5)`, tenant, p.ID, action, resource, blob)
	return err
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	// X-Forwarded-For is deliberately not trusted without an explicit proxy boundary.
	return host
}
