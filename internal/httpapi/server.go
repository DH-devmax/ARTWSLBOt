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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/assistantadapter"
	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/monitor"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
	"wangshangliao/internal/webcaptcha"
)

type Principal struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id,omitempty"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	TenantRole string `json:"tenant_role,omitempty"`
}

type Server struct {
	DB                *database.DB
	Vault             *security.Vault
	Origin            string
	SecureCookie      bool
	mux               *http.ServeMux
	loginMu           sync.Mutex
	nativeCheckMu     sync.Mutex
	nativeCheckUntil  time.Time
	accountLifecycle  sync.RWMutex
	loginSlots        chan struct{}
	loginAttempts     map[string]*loginAttempt
	dummyHash         string
	captcha           *webcaptcha.Manager
	Native            *nativeauth.Manager
	NativeMobile      *nativeauth.Manager
	ResourceSampler   *monitor.Sampler
	monitoring        monitoringState
	Receiver          *gateway.ReceiverManager
	Decoder           *gateway.DecoderManager
	Changes           *gateway.ChangeBus
	liveMu            sync.Mutex
	liveActors        map[string]int
	Assistant         *control.AssistantService
	ManualActions     *control.ManualActionService
	Gateway           *control.GatewayService
	HeatChat          *control.HeatChatService
	BusinessRules     *control.BusinessRulesService
	GroupBinding      *control.GroupBindingService
	AssistantRuntime  assistant.Runtime
	AssistantProvider func(assistant.ModelConfig) (assistant.Provider, error)
	assistantJobs     *assistant.Scheduler
	assistantStreams  chan struct{}
	assistantContext  context.Context
	mediaClient       *http.Client
	chiliMediaClient  *http.Client
	mediaSlots        chan struct{}
	chiliAuthClient   func() chiliAuthenticator
	chiliAuthOnce     sync.Once
	chiliSharedAuth   chiliAuthenticator
	// Message sending remains synthetic until nonce history is accepted.
	// The WSL upload adapter has separate grants and cannot send a message.
	nativeTextTestPort  control.TextPort
	nativeMediaTestPort control.MediaUploadPort
	chiliMediaTestPort  control.ChiliMediaPort
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
		captcha:           webcaptcha.New(webcaptcha.Generate),
		Changes:           gateway.NewChangeBus(),
		liveActors:        map[string]int{},
		ResourceSampler:   monitor.NewSampler("api_self", nil),
		monitoring:        monitoringState{actors: map[string]int{}},
		Assistant:         &control.AssistantService{DB: db, Vault: vault, Catalog: tenantPermissions},
		ManualActions:     &control.ManualActionService{DB: db, Vault: vault, Catalog: tenantPermissions},
		AssistantRuntime:  assistantadapter.Runtime{},
		AssistantProvider: func(c assistant.ModelConfig) (assistant.Provider, error) { return assistant.NewHTTPProvider(c) },
		assistantJobs:     assistant.NewScheduler(), assistantStreams: make(chan struct{}, 4),
		assistantContext: context.Background(),
		mediaClient:      nativeauth.TransportHTTP(), chiliMediaClient: lajiaoliao.ResourceHTTP(), mediaSlots: make(chan struct{}, 2),
	}
	s.Native = nativeauth.NewManagerWithDiagnostics(s.recordNativeAuthDiagnostic)
	s.NativeMobile = nativeauth.NewMobileManagerWithDiagnostics(s.recordNativeAuthDiagnostic)
	s.Gateway = &control.GatewayService{DB: db, Vault: vault, Catalog: tenantPermissions, Scheduler: aigateway.NewScheduler(),
		ProviderFactory: func(c assistant.ModelConfig) (assistant.Provider, error) { return s.AssistantProvider(c) }}
	s.Assistant.Gateway = s.Gateway
	s.HeatChat = &control.HeatChatService{DB: db, Vault: vault, Gateway: s.Gateway}
	s.BusinessRules = &control.BusinessRulesService{DB: db, Vault: vault, Catalog: tenantPermissions}
	s.HeatChat.BusinessRules = s.BusinessRules
	s.GroupBinding = &control.GroupBindingService{DB: db, Vault: vault, Catalog: tenantPermissions, Native: groupBindingAdapter{server: s}}
	s.GroupBinding.Decode = func(platform, account, self, key string, event int64, projection, source []byte) (control.OnboardingMessage, error) {
		if s.Receiver == nil {
			return control.OnboardingMessage{}, control.Denied(503, "group_binding_native_unavailable")
		}
		m, err := s.Receiver.ReadOnboardingSource(platform, account, self, key, event, projection, source)
		return control.OnboardingMessage{Group: m.Group, Sender: m.Sender, Recipient: m.Recipient,
			Text: m.Text, ClientID: m.ClientID, Private: m.Private}, err
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
	s.mobileAuthRoutes()
	s.webCaptchaRoutes()
	s.customerInvitationRoutes()
	s.groupBindingRoutes(s.GroupBinding)
	s.capabilityRoutes()
	s.businessRulesRoutes(s.BusinessRules)
	s.memberRoutes()
	s.billingRoutes()
	s.gatewayRoutes()
	s.assistantRoutes()
	s.platformAssistantRoutes()
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
	s.handle("GET /api/v1/user-center", false, s.userCenter)
	s.handle("GET /api/v1/user-center/groups", false, s.userCenterGroups)
	s.handle("GET /api/v1/user-center/usage", false, s.userCenterUsage)
	s.manualActionRoutes()
	s.handle("PUT /api/v1/groups/{id}/chat-preferences", false, s.saveChatPreferences)
	s.handle("GET /api/v1/access/catalog", true, s.accessCatalog)
	s.handle("GET /api/v1/users/{id}/access", true, s.userAccess)
	s.handle("PUT /api/v1/users/{id}/access", true, s.saveUserAccess)
	s.handle("GET /api/v1/overview", false, s.overview)
	s.handle("GET /api/v1/system/monitor", true, s.systemMonitor)
	s.handle("GET /api/v1/system/monitor/events", true, s.monitorEvents)
	s.handle("GET /api/v1/system/monitor/history", true, s.monitorHistory)
	s.handle("GET /api/v1/system/monitor/incidents", true, s.monitorIncidents)
	s.handle("GET /api/v1/accounts", false, s.accounts)
	s.handle("GET /api/v1/accounts/{id}/dashboard", false, s.accountDashboard)
	s.handle("GET /api/v1/accounts/{id}/events", false, s.accountEvents)
	s.handle("GET /api/v1/accounts/{id}/receiver", true, s.receiver)
	s.handle("GET /api/v1/accounts/{id}/conversations", true, s.nativeConversations)
	s.handle("GET /api/v1/accounts/{id}/conversations/{conversation}/messages", true, s.nativeMessages)
	s.handle("GET /api/v1/accounts/{id}/conversations/{conversation}/recalls", true, s.nativeRecalls)
	s.handle("GET /api/v1/accounts/{id}/conversations/{conversation}/messages/{event}/media/{part}", true, s.nativeMessageMedia)
	s.handle("GET /api/v1/accounts/{id}/native-groups/{group}/avatar/{reference}", true, s.nativeGroupAvatar)
	s.handle("GET /api/v1/accounts/{id}/native-avatar/{reference}", true, s.nativeAccountAvatar)
	s.handle("GET /api/v1/accounts/{id}/conversations/{conversation}/sending", true, s.nativeSending)
	s.handle("GET /api/v1/accounts/{id}/conversations/{conversation}/sending-targets", true, s.chiliSendingChoices)
	s.handle("POST /api/v1/accounts/{id}/conversations/{conversation}/text-operations", true, s.stageNativeText)
	s.handle("GET /api/v1/accounts/{id}/conversations/{conversation}/text-operations", true, s.inspectNativeText)
	s.handle("GET /api/v1/accounts/{id}/conversations/{conversation}/text-operations/{operation}", true, s.inspectNativeText)
	s.handle("POST /api/v1/accounts/{id}/conversations/{conversation}/text-operations/{operation}/dispatch", true, s.dispatchNativeText)
	s.handle("POST /api/v1/accounts/{id}/conversations/{conversation}/text-operations/{operation}/reconcile", true, s.reconcileNativeText)
	s.handle("POST /api/v1/accounts/{id}/conversations/{conversation}/media-uploads", true, s.uploadConversationMedia)
	s.handle("GET /api/v1/accounts/{id}/native-media-uploads", true, s.inspectMediaUpload)
	s.handle("GET /api/v1/accounts/{id}/native-media-uploads/{upload}", true, s.inspectMediaUpload)
	s.handle("POST /api/v1/accounts/{id}/conversations/{conversation}/media-operations", true, s.stageNativeMedia)
	s.handle("POST /api/v1/accounts/{id}/conversations/{conversation}/media-operations/{operation}/dispatch", true, s.dispatchNativeText)
	s.handle("GET /api/v1/accounts/{id}/decoder-health", true, s.decoderHealth)
	s.handle("POST /api/v1/accounts/{id}/directory-refresh", true, s.refreshNativeDirectory)
	s.handle("POST /api/v1/accounts/{id}/native-members", true, s.nativeMembers)
	s.handle("GET /api/v1/accounts/{id}/native-group-bans", true, s.nativeGroupBans)
	s.handle("GET /api/v1/accounts/{id}/native-settings", true, s.nativeSettings)
	s.handle("GET /api/v1/accounts/{id}/native-social", true, s.nativeSocial)
	s.handle("POST /api/v1/accounts/{id}/native-social-operations", true, s.changeNativeSocial)
	s.handle("POST /api/v1/accounts/{id}/native-social-operations/{operation}/reconcile", true, s.reconcileNativeSocial)
	s.handle("POST /api/v1/accounts/{id}/native-setting-operations", true, s.changeNativeSetting)
	s.handle("POST /api/v1/accounts/{id}/native-security-operations", true, s.changeNativeSecurity)
	s.handle("GET /api/v1/accounts/{id}/native-security-challenge", true, s.nativeSecurityChallenge)
	s.handle("GET /api/v1/accounts/{id}/native-security-flows/{flow}", true, s.nativeSecurityFlow)
	s.handle("GET /api/v1/accounts/{id}/native-setting-operations", true, s.inspectNativeSetting)
	s.handle("GET /api/v1/accounts/{id}/native-setting-operations/{operation}", true, s.inspectNativeSetting)
	s.handle("POST /api/v1/accounts/{id}/native-setting-operations/{operation}/reconcile", true, s.reconcileNativeSetting)
	s.handle("POST /api/v1/accounts/{id}/native-role", true, s.nativeRole)
	s.handle("GET /api/v1/accounts/{id}/native-text-operations", true, s.inspectAccountNativeText)
	s.handle("GET /api/v1/accounts/{id}/native-text-operations/{operation}", true, s.inspectAccountNativeText)
	s.handle("POST /api/v1/accounts/{id}/native-text-operations/{operation}/reconcile", true, s.reconcileAccountNativeText)
	s.handle("PUT /api/v1/accounts/{id}/receiver", true, s.setReceiver)
	s.handle("GET /api/v1/native-im-profile", true, s.imProfile)
	s.handle("PUT /api/v1/native-im-profile", true, s.saveIMProfile)
	s.handle("POST /api/v1/accounts", true, s.createAccount)
	s.handle("PATCH /api/v1/accounts/{id}", true, s.updateAccount)
	s.handle("GET /api/v1/accounts/{id}/removal-preview", true, s.accountRemovalPreview)
	s.handle("DELETE /api/v1/accounts/{id}", true, s.removeAccount)
	s.handle("GET /api/v1/native-deployment", true, s.nativeDeployment)
	s.handle("PUT /api/v1/native-deployment", true, s.saveNativeDeployment)
	s.handle("POST /api/v1/native-deployment/check", true, s.checkNativeDeployment)
	s.handle("POST /api/v1/accounts/{id}/native-auth", true, s.startNativeAuth)
	s.handle("POST /api/v1/accounts/{id}/lajiaoliao-challenge", true, s.chiliChallenge)
	s.handle("POST /api/v1/accounts/{id}/lajiaoliao-login", true, s.chiliLogin)
	s.handle("POST /api/v1/accounts/{id}/lajiaoliao-signup-sms", true, s.chiliSignupSMS)
	s.handle("POST /api/v1/accounts/{id}/lajiaoliao-signup", true, s.chiliLogin)
	s.handle("GET /api/v1/accounts/{id}/lajiaoliao-signup", true, s.chiliSignupStatus)
	s.handle("GET /api/v1/accounts/{id}/native-auth/{transaction}", true, s.pollNativeAuth)
	s.handle("POST /api/v1/accounts/{id}/native-auth/{transaction}", true, s.nativeAuthAction)
	s.handle("DELETE /api/v1/accounts/{id}/native-auth/{transaction}", true, s.cancelNativeAuth)
	s.handle("POST /api/v1/accounts/{id}/forget-session", true, s.forgetNativeSession)
	s.handle("POST /api/v1/native-signup", true, s.startNativeSignup)
	s.handle("GET /api/v1/groups", false, s.groups)
	s.handle("GET /api/v1/groups/{id}/assignment", true, s.assignmentPreview)
	s.handle("POST /api/v1/groups/{id}/assignment", true, s.assignGroup)
	s.handle("GET /api/v1/features/catalog", false, s.featureCatalog)
	s.handle("GET /api/v1/groups/{id}/features", false, s.groupFeatures)
	s.handle("PUT /api/v1/groups/{id}/features", false, s.saveGroupFeatures)
	s.handle("POST /api/v1/groups/{id}/features/preview", false, s.previewGroupFeatures)
	s.handle("PATCH /api/v1/groups/{id}", false, s.updateGroup)
	s.handle("PUT /api/v1/groups/{id}/ai-service", false, s.requestGroupAI)
	s.handle("GET /api/v1/groups/{id}/chat-runtime", false, s.heatChatRuntime)
	s.handle("PUT /api/v1/heat-chat/admission", true, s.heatAdmission)
	s.handle("POST /api/v1/heat-chat/jobs/{id}/reconcile", true, s.reconcileHeatChat)
	s.handle("POST /api/v1/groups/{id}/revoke", false, s.revokeGroup)
	s.handle("GET /api/v1/claims", false, s.claims)
	s.handle("POST /api/v1/claims", false, s.createClaim)
	s.handle("POST /api/v1/claims/{id}/review", true, s.reviewClaim)
	s.handle("POST /api/v1/groups/{id}/claim-code", true, s.issueClaimCode)
	s.handle("POST /api/v1/groups/{id}/test-binding", true, s.authorizeTestBinding)
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
	s.handle("GET /api/v1/test-tools", true, s.testToolCatalog)
	s.handle("GET /api/v1/test-tools/operations", true, s.testToolOperations)
	s.handle("POST /api/v1/test-tools/call", true, s.testToolCall)
	s.handle("POST /api/v1/test-tools/recall", true, s.testRecall)
	s.handle("GET /api/v1/test-tools/messages", true, s.testMessageCandidates)
	s.handle("POST /api/v1/test-tools/text", true, s.testChiliText)
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

func extendedEndpointTimeout(pattern string) time.Duration {
	switch pattern {
	case "POST /api/v1/ai-gateway/test", "POST /api/v1/platform-assistant/sessions/{id}/turns":
		return 55 * time.Second
	case "GET /api/v1/assistant/turns/{id}/events",
		"GET /api/v1/accounts/{id}/events", "GET /api/v1/system/monitor/events":
		return 100 * time.Second
	case "POST /api/v1/accounts/{id}/lajiaoliao-login",
		"POST /api/v1/accounts/{id}/lajiaoliao-signup",
		"POST /api/v1/accounts/{id}/lajiaoliao-signup-sms":
		// Config verification, native login and IM-ticket renewal each have a
		// bounded upstream timeout. Ordinary endpoints keep their 15s budget.
		return 45 * time.Second
	default:
		return 0
	}
}

func (s *Server) handle(pattern string, admin bool, f endpoint) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		requestContext := r.Context()
		authContext, authCancel := context.WithTimeout(requestContext, 15*time.Second)
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
            u.username,u.role,u.tenant_role,se.csrf_hash FROM sessions se JOIN users u ON u.id=se.user_id
            WHERE se.token_hash=$1 AND se.expires_at>now() AND NOT u.disabled`,
			security.Digest(cookie.Value)).Scan(&p.ID, &p.TenantID, &p.Username, &p.Role, &p.TenantRole, &csrf)
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
		bindingProgress := (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			strings.HasPrefix(pattern, "GET /api/v1/group-binding/")
		if p.TenantID != "" && !bindingProgress {
			if err := (&control.BillingService{DB: s.DB}).Refresh(r.Context(), p.TenantID); err != nil {
				sendError(w, fail(503, "database_unavailable"))
				return
			}
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
		if timeout := extendedEndpointTimeout(pattern); timeout > 0 {
			endpointContext, endpointCancel := context.WithTimeout(requestContext, timeout)
			defer endpointCancel()
			r = r.WithContext(endpointContext)
			if strings.Contains(pattern, "/lajiaoliao-") || pattern == "POST /api/v1/ai-gateway/test" ||
				pattern == "POST /api/v1/platform-assistant/sessions/{id}/turns" {
				controller := http.NewResponseController(w)
				if err := controller.SetWriteDeadline(time.Now().Add(timeout + 2*time.Second)); err != nil {
					sendError(w, fail(503, "native_auth_unavailable"))
					return
				}
				defer controller.SetWriteDeadline(time.Time{})
			}
		}
		r = r.WithContext(database.WithActor(r.Context(), p.ID))
		if purpose, target := humanVerificationOperation(pattern, r); purpose != "" && !onboardingVerificationInHandler(pattern) {
			if err := s.requireHumanVerification(r, p, purpose, target); err != nil {
				sendError(w, err)
				return
			}
		}
		if p.Role != "superadmin" && strings.HasPrefix(r.URL.Path, "/api/v1/groups/") {
			if err := control.RequireGroup(r.Context(), s.DB.Platform, control.Actor{ID: p.ID, TenantID: p.TenantID, Role: p.Role}, r.PathValue("id")); err != nil {
				sendError(w, err)
				return
			}
		}
		if err := f(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, p)), p); err != nil {
			sendError(w, err)
		} else {
			s.invalidateReadProjections(r, pattern)
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
	var c *control.Failure
	var data any
	if errors.As(err, &c) {
		e = &apiError{status: c.Status, code: c.Code}
	}
	if e == nil && errors.Is(err, control.ErrFreshRole) {
		e = &apiError{status: http.StatusConflict, code: "fresh_native_role_required"}
	}
	if e == nil && !errors.As(err, &e) {
		// Database details may contain user input; never expose them in responses.
		e = &apiError{status: 500, code: "internal_error"}
		slog.Error("request failed", "error_type", "internal")
	}
	var lookup *control.GroupBindingLookupFailure
	if errors.As(err, &lookup) {
		if diagnostic, ok := control.SafeGroupBindingLookupDiagnostic(lookup.Diagnostic); ok {
			data = struct {
				Lookup control.GroupBindingLookupDiagnostic `json:"lookup_diagnostic"`
			}{diagnostic}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(e.status)
	json.NewEncoder(w).Encode(map[string]any{"code": e.status, "msg": e.code, "data": data})
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
