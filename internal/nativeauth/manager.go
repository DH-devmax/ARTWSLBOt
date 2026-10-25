package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"wangshangliao/internal/security"
)

type Input struct {
	Action   string `json:"action"`
	Account  string `json:"account"`
	Password string `json:"password"`
	Captcha  string `json:"captcha"`
	Code     string `json:"code"`
	Nickname string `json:"nickname"`
	Agreed   bool   `json:"agreed"`
}

type View struct {
	ID                string    `json:"transaction_id"`
	AccountID         string    `json:"account_id"`
	Status            string    `json:"status"`
	Error             string    `json:"error"`
	ProviderCode      int       `json:"provider_code,omitempty"`
	Diagnostic        string    `json:"diagnostic,omitempty"`
	CaptchaID         string    `json:"captcha_id"`
	ExpiresAt         time.Time `json:"expires_at"`
	RetryAt           time.Time `json:"retry_at"`
	ResendAt          time.Time `json:"resend_at"`
	NativeUID         string    `json:"native_uid,omitempty"`
	Nickname          string    `json:"nickname,omitempty"`
	Purpose           string    `json:"purpose"`
	RegisteredAccount string    `json:"registered_account,omitempty"`
}

type SignupHooks struct {
	BeforeSubmit func(context.Context, string) error
	Complete     func(context.Context, string, string, error) error
}

type transaction struct {
	view        View
	owner       string
	client      AuthClient
	cancel      context.CancelFunc
	ctx         context.Context
	timer       *time.Timer
	busy        bool
	attempts    int
	phone       map[string]any
	key         string
	originalKey string
	commit      func(context.Context, Session, string) error
	signup      *SignupHooks
}

type startBudget struct {
	until time.Time
	count int
}

type Manager struct {
	mu      sync.Mutex
	items   map[string]*transaction
	starts  map[string]startBudget
	Factory func(Deployment) (AuthClient, error)
}

func NewManager() *Manager {
	return &Manager{items: make(map[string]*transaction), starts: make(map[string]startBudget), Factory: NewClient}
}

func (m *Manager) Start(owner, account string, d Deployment, commit func(context.Context, Session, string) error, sources ...RequestIDSource) (View, error) {
	return m.start(owner, account, d, commit, nil, sources...)
}

func (m *Manager) StartSignup(owner, account string, d Deployment, hooks SignupHooks, source RequestIDSource) (View, error) {
	if hooks.BeforeSubmit == nil || hooks.Complete == nil {
		return View{}, rejection("native_signup_unavailable")
	}
	return m.start(owner, account, d, nil, &hooks, source)
}

func (m *Manager) start(owner, account string, d Deployment, commit func(context.Context, Session, string) error, signup *SignupHooks, sources ...RequestIDSource) (View, error) {
	m.mu.Lock()
	var retired AuthClient
	defer func() {
		m.mu.Unlock()
		if retired != nil {
			retired.Close()
		}
	}()
	var previous *transaction
	for _, item := range m.items {
		if item.view.AccountID == account && time.Now().Before(item.view.ExpiresAt) {
			if item.owner != owner {
				return View{}, rejection("native_auth_account_busy")
			}
			if (item.signup == nil) == (signup == nil) {
				return item.view, nil
			}
			if item.busy {
				return View{}, rejection("native_auth_busy")
			}
			if item.view.Status == "unknown" {
				return View{}, rejection("native_auth_review_required")
			}
			previous = item
			break
		}
	}
	if previous == nil && len(m.items) >= 10 {
		return View{}, rejection("native_auth_limit")
	}
	if previous == nil {
		budget := m.starts[account]
		if time.Now().After(budget.until) {
			budget = startBudget{until: time.Now().Add(10 * time.Minute)}
		}
		if budget.count >= 4 {
			return View{}, rejection("native_auth_rate_limited")
		}
		budget.count++
		m.starts[account] = budget
	}
	for key, value := range m.starts {
		if time.Now().After(value.until) {
			delete(m.starts, key)
		}
	}
	client, err := m.Factory(d)
	if err != nil {
		return View{}, err
	}
	if concrete, ok := client.(*Client); ok && len(sources) == 1 {
		concrete.requestIDs = sources[0]
	}
	id, err := security.Token()
	if err != nil {
		client.Close()
		return View{}, err
	}
	expires := time.Now().Add(10 * time.Minute)
	if previous != nil {
		expires = previous.view.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(context.Background(), expires)
	purpose := "login"
	if signup != nil {
		purpose = "signup"
	}
	tx := &transaction{owner: owner, client: client, ctx: ctx, cancel: cancel, commit: commit, signup: signup,
		view: View{ID: id, AccountID: account, Purpose: purpose, Status: "pending", CaptchaID: d.CaptchaID, ExpiresAt: expires}}
	if previous != nil {
		// Changing tabs replaces only an idle flow, without resetting its safety budget.
		tx.attempts = previous.attempts
		tx.view.RetryAt, tx.view.ResendAt = previous.view.RetryAt, previous.view.ResendAt
		delete(m.items, previous.view.ID)
		previous.timer.Stop()
		previous.cancel()
		previous.key, previous.originalKey, previous.phone = "", "", nil
		retired = previous.client
	}
	m.items[id] = tx
	tx.timer = time.AfterFunc(time.Until(tx.view.ExpiresAt), func() { _, _ = m.Cancel(owner, account, id) })
	return tx.view, nil
}

func (m *Manager) current(owner, account, id string) (*transaction, error) {
	tx := m.items[id]
	if tx == nil || tx.owner != owner || tx.view.AccountID != account {
		return nil, rejection("native_auth_not_found")
	}
	if time.Now().After(tx.view.ExpiresAt) {
		return nil, rejection("native_auth_expired")
	}
	return tx, nil
}

func (m *Manager) Poll(owner, account, id string) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.current(owner, account, id)
	if err != nil {
		return View{}, err
	}
	return tx.view, nil
}

func (m *Manager) Cancel(owner, account, id string) (View, error) {
	m.mu.Lock()
	tx := m.items[id]
	if tx == nil || tx.owner != owner || tx.view.AccountID != account {
		m.mu.Unlock()
		return View{}, rejection("native_auth_not_found")
	}
	delete(m.items, id)
	tx.cancel()
	tx.timer.Stop()
	tx.view.Status = "cancelled"
	view, idle := tx.view, !tx.busy
	tx.key, tx.originalKey, tx.phone = "", "", nil
	m.mu.Unlock()
	if idle {
		tx.client.Close()
	}
	return view, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	type target struct{ owner, account, id string }
	targets := make([]target, 0, len(m.items))
	for id, tx := range m.items {
		targets = append(targets, target{tx.owner, tx.view.AccountID, id})
	}
	m.mu.Unlock()
	for _, t := range targets {
		_, _ = m.Cancel(t.owner, t.account, t.id)
	}
}

func (m *Manager) CancelAccount(account string) {
	m.mu.Lock()
	targets := make(map[string]string)
	for id, tx := range m.items {
		if tx.view.AccountID == account {
			targets[id] = tx.owner
		}
	}
	m.mu.Unlock()
	for id, owner := range targets {
		_, _ = m.Cancel(owner, account, id)
	}
}

// ForgetAccount is called after durable removal, while new starts are excluded
// by the control API. Running requests still have to pass the database fence.
func (m *Manager) ForgetAccount(account string) {
	m.CancelAccount(account)
	m.mu.Lock()
	delete(m.starts, account)
	m.mu.Unlock()
}

func (m *Manager) Action(owner, account, id string, in Input) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.current(owner, account, id)
	if err != nil {
		return View{}, err
	}
	if tx.busy {
		return View{}, rejection("native_auth_busy")
	}
	if time.Now().Before(tx.view.RetryAt) {
		return View{}, rejection("native_auth_cooldown")
	}
	if (in.Action == "sms_request" || in.Action == "sms_resend") && time.Now().Before(tx.view.ResendAt) {
		return View{}, rejection("native_auth_cooldown")
	}
	if tx.attempts >= 12 {
		return View{}, rejection("native_auth_rate_limited")
	}
	if len(in.Account) > 128 || len(in.Password) > 1024 || len(in.Captcha) > 8192 || len(in.Code) > 6 ||
		len(in.Nickname) > 256 {
		return View{}, rejection("native_invalid_input")
	}
	state := tx.view.Status
	if tx.signup != nil {
		if !in.Agreed {
			return View{}, rejection("native_signup_consent_required")
		}
		switch in.Action {
		case "signup_sms":
			if (state != "pending" && state != "failed") || !phoneDigits(in.Account) || in.Account[0] != '1' {
				return View{}, rejection("native_auth_state_conflict")
			}
			if !signupNickname(in.Nickname) {
				return View{}, rejection("native_invalid_input")
			}
		case "signup_resend":
			if state != "signup_sms_required" || tx.phone == nil || tx.key == "" {
				return View{}, rejection("native_auth_state_conflict")
			}
		case "signup_submit":
			if state != "signup_sms_required" || tx.phone == nil || tx.key == "" {
				return View{}, rejection("native_auth_state_conflict")
			}
			if !sixDigits(in.Code) || !signupPassword(in.Password) || !signupNickname(in.Nickname) {
				return View{}, rejection("native_invalid_input")
			}
		default:
			return View{}, rejection("native_auth_state_conflict")
		}
		if (in.Action == "signup_sms" || in.Action == "signup_resend") && time.Now().Before(tx.view.ResendAt) {
			return View{}, rejection("native_auth_cooldown")
		}
	} else {
		switch in.Action {
		case "password", "sms_request":
			if state != "pending" && state != "failed" {
				return View{}, rejection("native_auth_state_conflict")
			}
			if in.Account == "" || (in.Action == "password" && in.Password == "") ||
				(in.Action == "sms_request" && !phoneDigits(in.Account)) {
				return View{}, rejection("native_invalid_input")
			}
		case "sms_verify", "sms_resend":
			if state != "sms_required" || tx.phone == nil || tx.key == "" {
				return View{}, rejection("native_auth_state_conflict")
			}
			if in.Action == "sms_verify" && !sixDigits(in.Code) {
				return View{}, rejection("native_invalid_input")
			}
		case "security_verify":
			if state != "security_required" || !sixDigits(in.Code) {
				return View{}, rejection("native_auth_state_conflict")
			}
		case "retry_finish":
			if state != "connection_pending" {
				return View{}, rejection("native_auth_state_conflict")
			}
		default:
			return View{}, rejection("native_invalid_input")
		}
	}
	if in.Action != "security_verify" && in.Action != "retry_finish" && in.Captcha == "" {
		return View{}, rejection("native_captcha_required")
	}
	tx.attempts++
	tx.busy, tx.view.Status, tx.view.Error, tx.view.ProviderCode = true, "working", "", 0
	tx.view.Diagnostic = ""
	view := tx.view
	phone, key, original := tx.phone, tx.key, tx.originalKey
	go m.run(tx, in, phone, key, original)
	return view, nil
}

// Apply the official mixed-password length rule and reject blank/control input.
func signupPassword(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\u0085\u2028\u2029") {
		return false
	}
	size := len(utf16.Encode([]rune(value)))
	if size < 6 || size > 16 {
		return false
	}
	letters, digits, special := true, true, true
	const allowed = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ\u00d7\u00f7`\u00ac!\u00a3$%^*()~=#{}[];':,./?-_+<>@&"
	for _, r := range value {
		if !strings.ContainsRune(allowed, r) {
			return false
		}
		letters = letters && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		digits = digits && r >= '0' && r <= '9'
		special = special && !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || unicode.IsSpace(r))
		if unicode.IsControl(r) {
			return false
		}
	}
	return !letters && !digits && !special
}

func signupNickname(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(utf16.Encode([]rune(value))) > 20 {
		return false
	}
	return strings.IndexFunc(value, unicode.IsControl) < 0
}

func phoneDigits(account string) bool {
	if len(account) != 11 {
		return false
	}
	for _, b := range []byte(account) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

func (m *Manager) run(tx *transaction, in Input, phone map[string]any, key, original string) {
	ctx, cancel := context.WithTimeout(tx.ctx, 40*time.Second)
	defer cancel()
	state, retry, uid, nickname := "failed", time.Time{}, "", ""
	var registeredAccount string
	prepared := false
	var resend time.Time
	var err error
	var nextPhone map[string]any
	var nextKey, nextOriginal string
	defer func() {
		if recover() != nil {
			err, state = rejection("native_auth_failed"), "failed"
			if prepared {
				err, state = &Error{Code: "native_signup_review_required", Submitted: true}, "unknown"
			}
		}
		m.mu.Lock()
		if m.items[tx.view.ID] != tx || tx.ctx.Err() != nil {
			m.mu.Unlock()
			tx.client.Close()
			return
		}
		tx.busy = false
		if err != nil {
			var native *Error
			if errors.As(err, &native) {
				tx.view.Error, tx.view.ProviderCode = native.Code, native.Number
				tx.view.Diagnostic = native.Diagnostic
				if native.RetryAfter > 0 {
					retry = time.Now().Add(native.RetryAfter)
				}
				if native.Code == "native_transport_unknown" || native.Code == "native_response_invalid" {
					state = "unknown"
				}
				if native.Code == "native_security_required" {
					state = "security_required"
				}
				if native.Code == "native_security_setup_required" || native.Code == "native_security_phone_required" {
					state = "security_setup_required"
				}
				if native.Code == "native_signup_identity_mismatch" {
					state = "failed"
				}
			} else {
				tx.view.Error = "native_auth_failed"
			}
		}
		tx.view.Status, tx.view.NativeUID, tx.view.Nickname = state, uid, nickname
		tx.view.RegisteredAccount = registeredAccount
		tx.view.RetryAt = retry
		if !resend.IsZero() {
			tx.view.ResendAt = resend
		}
		if nextPhone != nil {
			tx.phone, tx.key, tx.originalKey = nextPhone, nextKey, nextOriginal
		}
		if state == "authenticated" || state == "registered" {
			tx.phone, tx.key, tx.originalKey = nil, "", ""
		}
		m.mu.Unlock()
		if state == "authenticated" || state == "registered" {
			tx.client.Close()
		}
	}()
	switch in.Action {
	case "signup_sms", "signup_resend":
		resend = time.Now().Add(60 * time.Second)
		if in.Action == "signup_sms" {
			number, _ := strconv.ParseInt(in.Account, 10, 64)
			phone = map[string]any{"countryCode": 86, "nationalNumber": number, "maskedNationalNumber": ""}
		} else {
			state = "signup_sms_required"
		}
		var result map[string]json.RawMessage
		result, err = tx.client.Request(ctx, "/v1/verify/sms-anon", map[string]any{
			"ty": "VERIFY_FOR_REGISTER", "phone": phone, "validateStr": in.Captcha, "Key": "",
		})
		if err != nil {
			return
		}
		if json.Unmarshal(result["key"], &nextKey) != nil || nextKey == "" || len(nextKey) > 8192 {
			err = &Error{Code: "native_response_invalid", Submitted: true}
			return
		}
		nextPhone, state = phone, "signup_sms_required"
		return
	case "signup_submit":
		state = "signup_sms_required"
		if err = tx.signup.BeforeSubmit(ctx, tx.view.ID); err != nil {
			return
		}
		prepared = true
		var result map[string]json.RawMessage
		result, err = tx.client.Request(ctx, "/v1/user/register", map[string]any{
			"key": key, "verificationCode": in.Code, "password": in.Password,
			"validateStr": in.Captcha, "phone": phone, "nickName": strings.TrimSpace(in.Nickname),
		})
		if err == nil {
			var accountID uint64
			if json.Unmarshal(result["accountId"], &accountID) != nil || accountID == 0 || accountID >= 1<<31 {
				err = &Error{Code: "native_response_invalid", Submitted: true}
			} else {
				registeredAccount = strconv.FormatUint(accountID, 10)
			}
		}
		// Preserve a known remote outcome even if the initiating browser cancelled.
		recordCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		recordErr := tx.signup.Complete(recordCtx, tx.view.ID, registeredAccount, err)
		done()
		if recordErr != nil {
			err = &Error{Code: "native_signup_record_pending", Submitted: true}
			state = "unknown"
			return
		}
		if err == nil {
			state = "registered"
		}
		return
	case "password":
		kind := "LOGIN_TYPE_ACCOUNT_PWD"
		if phoneDigits(in.Account) {
			kind = "LOGIN_TYPE_PHONE_PWD"
		}
		params := map[string]any{"account": in.Account, "passwd": in.Password, "validateStr": in.Captcha, "type": kind}
		err = tx.client.Login(ctx, params)
		var native *Error
		if errors.As(err, &native) && native.Number == 1069 {
			resend = time.Now().Add(60 * time.Second)
			var result map[string]json.RawMessage
			result, err = tx.client.Request(ctx, "/v1/user/get-change-device-verify",
				map[string]any{"account": in.Account, "passwd": in.Password, "type": kind})
			if err != nil {
				return
			}
			var sms struct {
				Key string `json:"key"`
			}
			if json.Unmarshal(result["phone"], &nextPhone) != nil || nextPhone == nil ||
				json.Unmarshal(result["sms"], &sms) != nil || sms.Key == "" || len(sms.Key) > 8192 {
				err = rejection("native_sms_challenge_invalid")
				return
			}
			nextKey, nextOriginal, state = sms.Key, sms.Key, "sms_required"
			return
		}
	case "sms_request", "sms_resend":
		resend = time.Now().Add(60 * time.Second)
		if in.Action == "sms_request" {
			number, _ := strconv.ParseInt(in.Account, 10, 64)
			phone = map[string]any{"countryCode": 86, "nationalNumber": number, "maskedNationalNumber": ""}
		}
		params := map[string]any{"ty": "VERIFY_FOR_LOGIN", "phone": phone, "validateStr": in.Captcha}
		if original != "" {
			params["Key"] = original
		}
		var result map[string]json.RawMessage
		result, err = tx.client.Request(ctx, "/v1/verify/sms-anon", params)
		if in.Action == "sms_resend" {
			state = "sms_required"
		}
		if err != nil {
			return
		}
		if json.Unmarshal(result["key"], &nextKey) != nil || nextKey == "" || len(nextKey) > 8192 {
			err = rejection("native_sms_challenge_invalid")
			return
		}
		nextPhone, nextOriginal, state = phone, original, "sms_required"
		return
	case "sms_verify":
		state = "sms_required"
		err = tx.client.Login(ctx, map[string]any{"phone": phone, "key": key, "verificationCode": in.Code,
			"validateStr": in.Captcha, "type": "LOGIN_TYPE_SMS"})
	case "security_verify":
		state, retry = "security_required", time.Now().Add(time.Second)
		err = tx.client.VerifySecurity(ctx, in.Code)
	case "retry_finish":
		state = "connection_pending"
	}
	if err != nil {
		return
	}
	if tx.client.SecurityRequired() {
		state = "security_required"
		return
	}
	state = "connection_pending"
	var saved Session
	saved, nickname, err = tx.client.Finish(ctx)
	if err != nil {
		return
	}
	if ctx.Err() != nil {
		err = rejection("native_auth_expired")
		return
	}
	err = tx.commit(ctx, saved, nickname)
	if err != nil {
		return
	}
	uid, state = strconv.FormatUint(saved.UID, 10), "authenticated"
	retry = time.Time{}
}
