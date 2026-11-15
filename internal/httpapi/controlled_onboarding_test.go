package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"wangshangliao/internal/control"
)

const controlledOnboardingScopePath = "/test-tools/onboarding/scopes"

func TestControlledOnboardingRoutesRequireAuthentication(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.groupBindingRoutes(&control.GroupBindingService{})
	id := control.NewID()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, controlledOnboardingScopePath + "?platform=wsl"},
		{http.MethodPost, controlledOnboardingScopePath},
		{http.MethodGet, controlledOnboardingScopePath + "/" + id},
		{http.MethodPost, controlledOnboardingScopePath + "/" + id + "/close"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			r := httptest.NewRequest(route.method, "/api/v1"+route.path, nil)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			var response struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil ||
				w.Code != http.StatusUnauthorized || response.Msg != "authentication_required" {
				t.Fatal("controlled scope route lost authentication", w.Code, response.Msg, err)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("controlled scope response is cacheable")
			}
		})
	}
}

func TestControlledOnboardingRejectsInvalidScopeBeforeNativeAccess(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			valid := control.ControlledOnboardingCreate{Platform: platform, AccountID: control.NewID(),
				ClaimantAccountID: control.NewID(), GroupID: control.NewID(), Minutes: 360, Key: "synthetic-scope-input"}
			for _, change := range []string{"platform", "account", "claimant", "group", "self", "negative_minutes", "long_minutes", "key"} {
				t.Run(change, func(t *testing.T) {
					in := valid
					switch change {
					case "platform":
						in.Platform = "maibot"
					case "account":
						in.AccountID = "not-an-id"
					case "claimant":
						in.ClaimantAccountID = "not-an-id"
					case "group":
						in.GroupID = "not-an-id"
					case "self":
						in.ClaimantAccountID = in.AccountID
					case "negative_minutes":
						in.Minutes = -1
					case "long_minutes":
						in.Minutes = 361
					case "key":
						in.Key = ""
					}
					s := &control.GroupBindingService{}
					_, err := s.CreateControlledScope(context.Background(), control.SessionRef{}.InPlatform(platform), in)
					friendFailure(t, err, http.StatusBadRequest, "controlled_onboarding_scope_invalid")
				})
			}
			for _, minutes := range []int{0, 1, 30, 31, 359, 360} {
				in := valid
				in.Minutes = minutes
				_, err := (&control.GroupBindingService{}).CreateControlledScope(
					context.Background(), control.SessionRef{}.InPlatform(platform), in)
				friendFailure(t, err, http.StatusServiceUnavailable, "group_binding_native_unavailable")
			}
		})
	}
}

type controlledOnboardingFixture struct {
	*onboardingFixture
	admin                       testClient
	platform, claimant, group   string
	nativeClaimant, nativeGroup string
}

func newControlledOnboardingFixture(t *testing.T, platform string) *controlledOnboardingFixture {
	t.Helper()
	return prepareControlledOnboardingFixture(t, newOnboardingFixture(t), platform)
}

func prepareControlledOnboardingFixture(t *testing.T, base *onboardingFixture, platform string) *controlledOnboardingFixture {
	t.Helper()
	f := &controlledOnboardingFixture{onboardingFixture: base, platform: platform,
		nativeGroup: "synthetic-controlled-onboarding-" + platform}
	f.admin = f.onboardingFixture.admin
	f.admin.workspace = platform
	f.nativeClaimant = f.native.owner
	ctx := context.Background()
	// integrationDB permits these synthetic account/role records only in wsl_test.
	if _, err := f.db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=true,connection_enabled=true
		WHERE id=$1`, f.accounts[platform]); err != nil {
		t.Fatal(err)
	}
	err := f.db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,workspace,purpose,native_uid,
		auth_state,credential_sealed,testing_enabled,connection_enabled)
		VALUES('Synthetic controlled claimant',$1,'maibot','chat',$2,'authenticated',$3,true,true)
		RETURNING id::text`, platform, f.nativeClaimant, []byte("synthetic-claimant-credential-"+platform)).Scan(&f.claimant)
	if err == nil {
		_, err = f.db.Platform.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
			VALUES($1,true,'synthetic-claimant-receiver',now()+interval '1 hour',now(),'{"state":"connected"}')`, f.claimant)
	}
	if err == nil {
		err = f.db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name,isolated_test)
			VALUES($1,$2,'Synthetic controlled onboarding group',true)
			ON CONFLICT(platform,native_id) DO UPDATE SET isolated_test=true RETURNING id::text`,
			platform, f.nativeGroup).Scan(&f.group)
	}
	if err == nil {
		_, err = f.db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
			VALUES($1,$3,'admin',clock_timestamp(),'native'),($2,$3,'owner',clock_timestamp(),'native')
			ON CONFLICT(account_id,group_id) DO NOTHING`,
			f.accounts[platform], f.claimant, f.group)
	}
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *controlledOnboardingFixture) input() map[string]any {
	return map[string]any{"platform": f.platform, "account_id": f.accounts[f.platform],
		"claimant_account_id": f.claimant, "group_id": f.group, "minutes": 360}
}

func controlledOnboardingScope(t *testing.T, raw []byte) control.ControlledOnboardingScope {
	t.Helper()
	var out control.ControlledOnboardingScope
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *controlledOnboardingFixture) createScope(t *testing.T, key string) control.ControlledOnboardingScope {
	t.Helper()
	return controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
		controlledOnboardingScopePath, key, f.input(), http.StatusCreated))
}

func (f *controlledOnboardingFixture) challenge(t *testing.T, scope, key string) control.GroupBindingChallenge {
	t.Helper()
	raw := onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges", key,
		map[string]any{"platform": f.platform, "controlled_scope_id": scope}, http.StatusCreated)
	var out control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *controlledOnboardingFixture) readChallenge(t *testing.T, c control.GroupBindingChallenge) control.GroupBindingChallenge {
	t.Helper()
	var out control.GroupBindingChallenge
	raw := onboardingAPI(t, f.admin, http.MethodGet, "/group-binding/challenges/"+c.ID, "", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *controlledOnboardingFixture) scan(t *testing.T, message control.OnboardingMessage) {
	t.Helper()
	f.ingress(t, f.platform, message, nil)
	if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func controlledOnboardingList(t *testing.T, client testClient, platform string) control.ControlledOnboardingList {
	t.Helper()
	var out control.ControlledOnboardingList
	raw := onboardingAPI(t, client, http.MethodGet, controlledOnboardingScopePath+"?platform="+platform,
		"", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &out); err != nil || out.Items == nil ||
		out.Candidates.Accounts == nil || out.Candidates.Groups == nil {
		t.Fatal("controlled list lost its bounded items/candidates shape", err)
	}
	return out
}

func controlledOnboardingOtherAdmin(t *testing.T, f *controlledOnboardingFixture) testClient {
	t.Helper()
	id := control.NewID()
	if _, err := f.db.Platform.Exec(context.Background(), `INSERT INTO users(id,username,password_hash,role)
		VALUES($1,$1::uuid::text,'synthetic-unused-password','superadmin')`, id); err != nil {
		t.Fatal(err)
	}
	client := f.client(t, f.admin.base, id)
	client.workspace = f.platform
	return client
}

func TestControlledOnboardingPostgresCustomerRoutesDenied(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			id := control.NewID()
			for _, client := range []testClient{f.alice, f.member, f.bob} {
				client.workspace = platform
				for _, route := range []struct {
					method, path string
					body         any
				}{
					{http.MethodGet, controlledOnboardingScopePath + "?platform=" + platform, nil},
					{http.MethodPost, controlledOnboardingScopePath, f.input()},
					{http.MethodGet, controlledOnboardingScopePath + "/" + id, nil},
					{http.MethodPost, controlledOnboardingScopePath + "/" + id + "/close", map[string]int64{"version": 1}},
				} {
					status, _, code := client.requestRaw(route.method, route.path, route.body)
					if status != http.StatusForbidden || code != "superadmin_required" {
						t.Fatal("ordinary website actor reached controlled scopes", status, code)
					}
				}
			}
			if f.native.roles.Load() != 0 || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM onboarding_test_scopes`) != 0 {
				t.Fatal("rejected customer request created authorization or native work")
			}
		})
	}
}

func TestControlledOnboardingPostgresNormalPoolProofStillDenied(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			scope := f.createScope(t, "controlled-scope-customer-isolation")
			client := f.alice
			client.workspace = platform
			// Knowing a scope ID does not authorize the ordinary customer route.
			onboardingAPI(t, client, http.MethodPost, "/group-binding/challenges", "customer-cannot-borrow-scope",
				map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, http.StatusForbidden)
			c := f.createChallenge(t, client, platform, "customer-normal-pool-proof")
			calls := f.native.roles.Load()
			for _, private := range []bool{true, false} {
				message := control.OnboardingMessage{Private: private, Sender: f.nativeClaimant, Text: c.PrivateChallenge}
				if !private {
					message.Group, message.Text = f.nativeGroup, c.GroupChallenge
				}
				f.scan(t, message)
				raw := onboardingAPI(t, client, http.MethodGet, "/group-binding/challenges/"+c.ID, "", nil, http.StatusOK)
				var current control.GroupBindingChallenge
				if err := json.Unmarshal(raw, &current); err != nil || current.IdentityProven ||
					current.State != "waiting" || current.CanConfirm || current.Version != c.Version ||
					current.BlockedReason != "group_binding_pool_identity" {
					t.Fatal("controlled scope weakened the ordinary non-pool identity gate", current, err)
				}
			}
			if f.native.roles.Load() != calls || f.native.sends.Load() != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings`) != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_ownership`) != 0 {
				t.Fatal("ordinary pool proofs created ownership or native effects")
			}
		})
	}
}

func TestControlledOnboardingPostgresScopeHTTPIsolationAndClose(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			list := controlledOnboardingList(t, f.admin, platform)
			accounts := map[string]bool{}
			for _, account := range list.Candidates.Accounts {
				if account.Platform != platform || account.ID == "" || account.NativeID == "" || account.Name == "" {
					t.Fatal("scope candidates leaked another platform or lost identity", account)
				}
				accounts[account.ID] = true
			}
			if len(list.Items) != 0 || !accounts[f.accounts[platform]] || !accounts[f.claimant] ||
				len(list.Candidates.Groups) != 1 || list.Candidates.Groups[0].ID != f.group ||
				f.native.roles.Load() != 0 {
				t.Fatal("viewing candidates created a scope or read native roles", list)
			}
			before := time.Now()
			input := f.input()
			delete(input, "minutes")
			raw := onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
				"controlled-default-six-hours", input, http.StatusCreated)
			scope := controlledOnboardingScope(t, raw)
			if scope.ID == "" || scope.State != "active" || scope.Version != 1 ||
				scope.ActorID != f.adminID || scope.TenantID == "" ||
				scope.TenantID == f.tenantA || scope.TenantID == f.tenantB ||
				scope.Platform != platform || scope.AccountID != f.accounts[platform] ||
				scope.ClaimantAccountID != f.claimant || scope.GroupID != f.group ||
				scope.Robot.ID != scope.AccountID || scope.Robot.Name == "" || scope.Robot.NativeID != "synthetic-bot-"+platform ||
				scope.Claimant.ID != f.claimant || scope.Claimant.Name == "" || scope.Claimant.NativeID != f.nativeClaimant ||
				scope.Group.ID != f.group || scope.Group.Name == "" || scope.Group.NativeID != f.nativeGroup ||
				scope.ExpiresAt.Before(before.Add(6*time.Hour)) || scope.ExpiresAt.After(time.Now().Add(6*time.Hour)) {
				t.Fatal("scope creation lost server-owned identity or the default deadline", scope)
			}
			calls := f.native.roles.Load()
			replay := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
				controlledOnboardingScopePath, "controlled-default-six-hours", f.input(), http.StatusCreated))
			if replay.ID != scope.ID || replay.Version != scope.Version || !replay.ExpiresAt.Equal(scope.ExpiresAt) ||
				f.native.roles.Load() != calls {
				t.Fatal("scope retry extended its deadline or repeated native authorization", replay)
			}
			conflict := f.input()
			conflict["minutes"] = 359
			onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
				"controlled-default-six-hours", conflict, http.StatusConflict)
			onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath, "", f.input(), http.StatusBadRequest)
			for _, name := range []string{"actor_id", "tenant_id", "claimant", "robot", "role", "key"} {
				forged := f.input()
				forged[name] = "browser-forged"
				onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
					"controlled-forged-"+name, forged, http.StatusBadRequest)
			}
			list = controlledOnboardingList(t, f.admin, platform)
			if len(list.Items) != 1 || list.Items[0].ID != scope.ID {
				t.Fatal("actor's scope disappeared from its list", list)
			}
			path := controlledOnboardingScopePath + "/" + scope.ID
			other := controlledOnboardingOtherAdmin(t, f)
			onboardingAPI(t, other, http.MethodGet, path, "", nil, http.StatusNotFound)
			onboardingAPI(t, other, http.MethodPost, path+"/close", "",
				map[string]int64{"version": scope.Version}, http.StatusNotFound)
			onboardingAPI(t, other, http.MethodPost, "/group-binding/challenges", "cross-admin-cannot-borrow-scope",
				map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, http.StatusNotFound)
			if list := controlledOnboardingList(t, other, platform); len(list.Items) != 0 {
				t.Fatal("another superadmin listed the actor's scope", list)
			}
			otherPlatform := "wsl"
			if platform == "wsl" {
				otherPlatform = "lajiaoliao"
			}
			wrong := f.admin
			wrong.workspace = otherPlatform
			onboardingAPI(t, wrong, http.MethodGet, path, "", nil, http.StatusNotFound)
			onboardingAPI(t, wrong, http.MethodPost, path+"/close", "",
				map[string]int64{"version": scope.Version}, http.StatusNotFound)
			onboardingAPI(t, f.admin, http.MethodGet, controlledOnboardingScopePath+"?platform="+otherPlatform,
				"", nil, http.StatusBadRequest)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				unknown := controlledOnboardingScopePath + "/" + control.NewID()
				body := any(nil)
				if method == http.MethodPost {
					unknown += "/close"
					body = map[string]int64{"version": 1}
				}
				onboardingAPI(t, f.admin, method, unknown, "", body, http.StatusNotFound)
			}
			onboardingAPI(t, f.admin, http.MethodGet, controlledOnboardingScopePath+"/not-an-id",
				"", nil, http.StatusBadRequest)
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges", "forged-scope-id",
				map[string]any{"platform": platform, "controlled_scope_id": control.NewID()}, http.StatusNotFound)
			if f.native.roles.Load() != calls || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("progress, rejected fields or cross-actor requests reached a native port")
			}
			challenge := f.challenge(t, scope.ID, "controlled-close-pending-challenge")
			onboardingAPI(t, f.admin, http.MethodPost, path+"/close", "",
				map[string]int64{"version": scope.Version + 1}, http.StatusConflict)
			onboardingAPI(t, f.admin, http.MethodPost, path+"/close", "",
				map[string]any{"version": scope.Version, "group_id": control.NewID()}, http.StatusBadRequest)
			closed := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
				path+"/close", "", map[string]int64{"version": scope.Version}, http.StatusOK))
			if closed.ID != scope.ID || closed.State != "closed" || closed.Version != scope.Version+1 ||
				!closed.ExpiresAt.Equal(scope.ExpiresAt) || f.readChallenge(t, challenge).State != "cancelled" {
				t.Fatal("close did not fence the scope and pending challenge", closed)
			}
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges", "closed-scope-cannot-reopen",
				map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, http.StatusConflict)
			if list := controlledOnboardingList(t, f.admin, platform); len(list.Items) != 1 ||
				list.Items[0].ID != scope.ID || list.Items[0].State != "closed" {
				t.Fatal("close erased administrative history", list)
			}
			reopened := f.createScope(t, "controlled-explicit-reopen-six-hours")
			if reopened.ID == scope.ID || reopened.State != "active" || reopened.Version != 1 ||
				reopened.NativeAttempts != 0 || reopened.ModelCalls != 0 {
				t.Fatal("explicit reopen revived an old scope or carried its usage", reopened)
			}
			newChallenge := f.challenge(t, reopened.ID, "controlled-reopen-fresh-challenge")
			if newChallenge.ID == challenge.ID || newChallenge.IdentityProven || newChallenge.CanConfirm ||
				newChallenge.State != "waiting" || f.readChallenge(t, challenge).State != "cancelled" {
				t.Fatal("explicit reopen reused an old proof or challenge", newChallenge)
			}
			prior := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodGet,
				path, "", nil, http.StatusOK))
			if prior.State != "closed" || prior.Version != closed.Version || !prior.ExpiresAt.Equal(scope.ExpiresAt) {
				t.Fatal("explicit reopen changed the old scope", prior)
			}
		})
	}
}

func TestControlledOnboardingPostgresDurationBoundaries(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			var closed control.ControlledOnboardingScope
			for _, minutes := range []int{1, 31, 360} {
				input := f.input()
				input["minutes"] = minutes
				before := time.Now()
				scope := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
					controlledOnboardingScopePath, control.NewID(), input, http.StatusCreated))
				duration := time.Duration(minutes) * time.Minute
				if scope.ExpiresAt.Before(before.Add(duration)) || scope.ExpiresAt.After(time.Now().Add(duration)) {
					t.Fatal("explicit duration did not reach the database unchanged", minutes, scope)
				}
				closed = controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
					controlledOnboardingScopePath+"/"+scope.ID+"/close", "",
					map[string]int64{"version": scope.Version}, http.StatusOK))
			}
			_, err := f.db.Platform.Exec(context.Background(), `INSERT INTO onboarding_test_scopes
				SELECT (jsonb_populate_record(NULL::onboarding_test_scopes,to_jsonb(s)||
					jsonb_build_object('id',$2::text,'create_key',$2::text,'state','active','version',1,
						'closed_at',NULL,'created_at',clock_timestamp(),
						'expires_at',clock_timestamp()+interval '6 hours 2 seconds'))).*
				FROM onboarding_test_scopes s WHERE id=$1`, closed.ID, control.NewID())
			var failure *pgconn.PgError
			if !errors.As(err, &failure) || failure.Code != "23514" ||
				failure.ConstraintName != "onboarding_test_scope_deadline" {
				t.Fatal("database did not enforce the independent six-hour deadline constraint", err)
			}
			_, err = f.db.Platform.Exec(context.Background(), `UPDATE onboarding_test_scopes
				SET expires_at=expires_at+interval '1 minute',version=version+1 WHERE id=$1`, closed.ID)
			if !errors.As(err, &failure) || failure.Code != "23514" {
				t.Fatal("closed scope deadline became mutable", err)
			}
		})
	}
}

func TestControlledOnboardingPostgresLegacyDefaultRetry(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			input := f.input()
			input["minutes"] = 30
			key := control.NewID()
			current := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
				controlledOnboardingScopePath, key, input, http.StatusCreated))
			defaultInput := f.input()
			delete(defaultInput, "minutes")
			onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
				key, defaultInput, http.StatusConflict)
			onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath+"/"+current.ID+"/close",
				"", map[string]int64{"version": current.Version}, http.StatusOK)

			// Reproduce a pre-069 row without changing its canonical request or deadline.
			legacyID, legacyKey := control.NewID(), control.NewID()
			_, err := f.db.Platform.Exec(context.Background(), `INSERT INTO onboarding_test_scopes
				SELECT (jsonb_populate_record(NULL::onboarding_test_scopes,to_jsonb(s)||
					jsonb_build_object('id',$2::text,'create_key',$3::text,'state','active',
						'version',1,'closed_at',NULL,'request_default_minutes',30))).*
				FROM onboarding_test_scopes s WHERE id=$1`, current.ID, legacyID, legacyKey)
			if err != nil {
				t.Fatal(err)
			}
			calls := f.native.roles.Load()
			for _, retry := range []map[string]any{defaultInput, input} {
				old := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
					controlledOnboardingScopePath, legacyKey, retry, http.StatusCreated))
				if old.ID != legacyID || old.Version != 1 || !old.ExpiresAt.Equal(current.ExpiresAt) ||
					f.native.roles.Load() != calls {
					t.Fatal("legacy retry changed the scope or repeated native authorization", old)
				}
			}
			onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
				legacyKey, f.input(), http.StatusConflict)
			_, err = f.db.Platform.Exec(context.Background(), `UPDATE onboarding_test_scopes
				SET request_default_minutes=360,version=version+1 WHERE id=$1`, legacyID)
			var failure *pgconn.PgError
			if !errors.As(err, &failure) || failure.Code != "23514" {
				t.Fatal("legacy canonical request default became mutable", err)
			}
			if f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("legacy retry caused a native effect")
			}
		})
	}
}

func TestControlledOnboardingPostgresSelectionPlatformAndRoleMismatch(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			ctx := context.Background()
			other := "wsl"
			if platform == "wsl" {
				other = "lajiaoliao"
			}
			var otherGroup string
			if err := f.db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name,isolated_test)
				VALUES($1,'synthetic-other-platform-controlled','Synthetic other platform group',true) RETURNING id::text`,
				other).Scan(&otherGroup); err != nil {
				t.Fatal(err)
			}
			// Enable the other synthetic account so platform is the rejecting fence.
			if _, err := f.db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=true WHERE id=$1`,
				f.accounts[other]); err != nil {
				t.Fatal(err)
			}
			for _, mismatch := range []struct {
				field string
				value any
				want  int
			}{
				{"platform", other, http.StatusBadRequest},
				{"account_id", f.accounts[other], http.StatusConflict},
				{"claimant_account_id", f.accounts[other], http.StatusConflict},
				{"group_id", otherGroup, http.StatusConflict},
				{"account_id", control.NewID(), http.StatusConflict},
				{"claimant_account_id", control.NewID(), http.StatusConflict},
				{"group_id", control.NewID(), http.StatusConflict},
				{"claimant_account_id", f.accounts[platform], http.StatusBadRequest},
				{"minutes", -1, http.StatusBadRequest},
				{"minutes", 361, http.StatusBadRequest},
			} {
				in := f.input()
				in[mismatch.field] = mismatch.value
				onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
					"controlled-selection-mismatch", in, mismatch.want)
			}
			if f.native.roles.Load() != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM onboarding_test_scopes`) != 0 {
				t.Fatal("a cross-platform or unknown selection reached the native role reader")
			}
			for _, unavailable := range []struct {
				id, invalidate, restore string
			}{
				{f.accounts[platform], `UPDATE accounts SET testing_enabled=false WHERE id=$1`,
					`UPDATE accounts SET testing_enabled=true WHERE id=$1`},
				{f.claimant, `UPDATE accounts SET testing_enabled=false WHERE id=$1`,
					`UPDATE accounts SET testing_enabled=true WHERE id=$1`},
				{f.accounts[platform], `UPDATE account_receivers SET observed_at=clock_timestamp()-interval '1 minute' WHERE account_id=$1`,
					`UPDATE account_receivers SET observed_at=clock_timestamp() WHERE account_id=$1`},
				{f.group, `UPDATE native_groups SET isolated_test=false WHERE id=$1`,
					`UPDATE native_groups SET isolated_test=true WHERE id=$1`},
			} {
				if _, err := f.db.Platform.Exec(ctx, unavailable.invalidate, unavailable.id); err != nil {
					t.Fatal(err)
				}
				onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
					"controlled-selection-unavailable", f.input(), http.StatusConflict)
				if unavailable.id == f.group {
					if list := controlledOnboardingList(t, f.admin, platform); len(list.Candidates.Groups) != 0 {
						t.Fatal("unisolated group remained a controlled scope candidate", list)
					}
				}
				if _, err := f.db.Platform.Exec(ctx, unavailable.restore, unavailable.id); err != nil {
					t.Fatal(err)
				}
			}
			if f.native.roles.Load() != 0 {
				t.Fatal("disabled account, stale receiver or unisolated group reached the native port")
			}
			f.native.owner = "synthetic-different-native-owner"
			onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath,
				"controlled-member-cannot-claim", f.input(), http.StatusConflict)
			if f.native.roles.Load() != 1 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM onboarding_test_scopes`) != 0 {
				t.Fatal("a claimant without native owner/admin role created a scope")
			}
		})
	}
}

func TestControlledOnboardingPostgresBoundProofAndChallengeLifetime(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			scope := f.createScope(t, "controlled-six-hour-scope")
			before := time.Now()
			c := f.challenge(t, scope.ID, "controlled-ten-minute-challenge")
			if c.State != "waiting" || c.IdentityProven || c.CanConfirm || c.Mode != "operator_demo" ||
				c.ProofKind != "controlled_test" || c.ControlledScope == nil || c.ControlledScope.ID != scope.ID ||
				c.Scope.ActorID != f.adminID || c.Scope.TenantID != scope.TenantID ||
				c.Robot.ID != scope.AccountID || c.Group == nil || c.Group.ID != scope.GroupID ||
				c.PrivateChallenge == "" || c.GroupChallenge == "" ||
				c.ExpiresAt.Before(before.Add(9*time.Minute)) || c.ExpiresAt.After(time.Now().Add(10*time.Minute)) ||
				scope.ExpiresAt.Sub(c.ExpiresAt) < 5*time.Hour+49*time.Minute {
				t.Fatal("scope silently extended the proof deadline or supplied a proof", c)
			}
			f.scan(t, control.OnboardingMessage{Private: true, Sender: "synthetic-unscoped-human", Text: c.PrivateChallenge})
			current := f.readChallenge(t, c)
			if current.IdentityProven || current.CanConfirm || current.State != "waiting" ||
				current.BlockedReason != "controlled_onboarding_claimant_mismatch" {
				t.Fatal("unscoped claimant supplied private identity", current)
			}
			f.scan(t, control.OnboardingMessage{Private: true, Sender: f.nativeClaimant, Text: c.PrivateChallenge})
			current = f.readChallenge(t, c)
			if !current.IdentityProven || current.CanConfirm || current.State != "waiting" {
				t.Fatal("scoped private proof replaced the independent group proof", current)
			}
			for _, message := range []control.OnboardingMessage{
				{Group: "synthetic-unscoped-group", Sender: f.nativeClaimant, Text: c.GroupChallenge},
				{Group: f.nativeGroup, Sender: "synthetic-native-admin", Text: c.GroupChallenge},
			} {
				f.scan(t, message)
				current = f.readChallenge(t, c)
				if current.State != "waiting" || current.CanConfirm || current.Group == nil || current.Group.ID != f.group {
					t.Fatal("another group or claimant widened controlled proof", current)
				}
			}
			f.scan(t, control.OnboardingMessage{Group: f.nativeGroup, Sender: f.nativeClaimant, Text: c.GroupChallenge})
			proven := f.readChallenge(t, c)
			if proven.State != "proven" || !proven.IdentityProven || !proven.CanConfirm || proven.ProofRole != "owner" {
				t.Fatal("fresh scoped private and group proof did not become confirmable", proven)
			}
			calls := f.native.roles.Load()
			f.readChallenge(t, c)
			onboardingAPI(t, f.admin, http.MethodGet, controlledOnboardingScopePath+"/"+scope.ID, "", nil, http.StatusOK)
			controlledOnboardingList(t, f.admin, platform)
			if f.native.roles.Load() != calls {
				t.Fatal("GET progress consumed proof or refreshed native evidence")
			}
			raw := onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/confirm",
				"controlled-explicit-confirm", map[string]int64{"version": proven.Version}, http.StatusOK)
			if field(t, raw, "state") != "bound" {
				t.Fatal("controlled proof could not bind its own operator demo group")
			}
			var isolated bool
			err := f.db.Platform.QueryRow(context.Background(), `SELECT o.proof_kind='controlled_test'
				AND o.controlled_scope_id=$2 AND c.controlled_scope_id=$2 AND c.applicant_mode='operator_demo'
				AND b.tenant_id=$3 AND t.space_kind='operator_demo' AND t.operator_owner_id=$4
				AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT r.enabled
				AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=b.tenant_id)
				AND NOT EXISTS(SELECT 1 FROM business_capability_gates WHERE state='open')
				FROM group_binding_ownership o JOIN group_bindings b ON b.group_id=o.group_id
				JOIN group_business_runtime r ON r.group_id=b.group_id JOIN tenants t ON t.id=b.tenant_id
				JOIN group_binding_challenges c ON c.id=o.proof_challenge_id WHERE o.group_id=$1`,
				f.group, scope.ID, scope.TenantID, f.adminID).Scan(&isolated)
			if err != nil || !isolated || f.native.sends.Load() != 0 || f.native.invites.Load() != 0 {
				t.Fatal("controlled binding became customer proof or activated runtime/source gates", err)
			}
			if _, err = f.db.Platform.Exec(context.Background(), `UPDATE group_bindings
				SET config=jsonb_set(config,'{blocked_words}','["retain-controlled"]') WHERE group_id=$1`, f.group); err != nil {
				t.Fatal(err)
			}
			onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath+"/"+scope.ID+"/close", "",
				map[string]int64{"version": scope.Version}, http.StatusOK)
			err = f.db.Platform.QueryRow(context.Background(), `SELECT o.state='paused'
				AND o.proof_kind='controlled_test' AND o.controlled_scope_id=$2
				AND b.config->'blocked_words'='["retain-controlled"]'::jsonb
				AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT r.enabled
				AND c.state='bound' AND c.proof_sealed IS NOT NULL AND c.identity_sealed IS NOT NULL
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id
				JOIN group_business_runtime r ON r.group_id=b.group_id
				JOIN group_binding_challenges c ON c.id=o.proof_challenge_id WHERE b.group_id=$1`,
				f.group, scope.ID).Scan(&isolated)
			if err != nil || !isolated {
				t.Fatal("scope close erased saved configuration or proof history", err)
			}
		})
	}
}

func TestControlledOnboardingPostgresAccountVersionsFenceConfirmation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			for _, change := range []string{"robot_epoch", "robot_version", "claimant_epoch", "claimant_version", "claimant_during_roles"} {
				t.Run(change, func(t *testing.T) {
					f := newControlledOnboardingFixture(t, platform)
					scope := f.createScope(t, "controlled-version-scope")
					c := f.challenge(t, scope.ID, "controlled-version-challenge")
					proven := f.prove(t, f.admin, platform, c, f.nativeClaimant, f.nativeGroup)
					invalidate := func() {
						t.Helper()
						account, query := f.accounts[platform], `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`
						switch change {
						case "robot_version":
							query = `UPDATE accounts SET version=version+1 WHERE id=$1`
						case "claimant_epoch":
							account = f.claimant
						case "claimant_version", "claimant_during_roles":
							account, query = f.claimant, `UPDATE accounts SET version=version+1 WHERE id=$1`
						}
						if _, err := f.db.Platform.Exec(context.Background(), query, account); err != nil {
							t.Fatal(err)
						}
					}
					if change == "claimant_during_roles" {
						f.native.beforeRoles = func() {
							f.native.beforeRoles = nil
							invalidate()
						}
					} else {
						invalidate()
					}
					onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/confirm",
						"controlled-stale-confirm", map[string]int64{"version": proven.Version}, http.StatusConflict)
					current := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodGet,
						controlledOnboardingScopePath+"/"+scope.ID, "", nil, http.StatusOK))
					if current.State != "closed" || current.BlockedReason != "controlled_onboarding_scope_changed" ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings`) != 0 ||
						friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_ownership`) != 0 ||
						f.native.sends.Load() != 0 {
						t.Fatal("stale receiving/claimant connection retained scope or committed ownership", current)
					}
					onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges", "controlled-stale-create",
						map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, http.StatusConflict)
				})
			}
		})
	}
}

func TestControlledOnboardingPostgresExistingGroupTransferNeedsReview(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			ctx := context.Background()
			source := f.alice
			source.workspace = platform
			f.native.owner = "synthetic-original-customer-owner-" + platform
			first := f.createChallenge(t, source, platform, "controlled-transfer-original-customer")
			first = f.prove(t, source, platform, first, f.native.owner, f.nativeGroup)
			onboardingAPI(t, source, http.MethodPost, "/group-binding/challenges/"+first.ID+"/confirm",
				"controlled-transfer-original-confirm", map[string]int64{"version": first.Version}, http.StatusOK)
			if first.Group == nil || first.Group.ID != f.group {
				t.Fatal("original customer did not bind the existing observed group", first)
			}
			if _, err := f.db.Platform.Exec(ctx, `UPDATE group_bindings SET config='{"blocked_words":["source-only"]}'
				WHERE group_id=$1`, f.group); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Platform.Exec(ctx, `INSERT INTO ledger_entries(tenant_id,group_id,member_id,kind,
				amount_cents,external_key,evidence,actor_id)
				VALUES($1,$2,'synthetic-source-member','adjustment',100,'controlled-transfer-source-history','{}',$3)`,
				f.tenantA, f.group, f.aliceID); err != nil {
				t.Fatal(err)
			}
			// The synthetic native owner is now the explicitly selected pooled
			// claimant. Its new proof must still pass the dedicated tenant transfer.
			f.native.owner = f.nativeClaimant
			scope := f.createScope(t, "controlled-transfer-scope")
			c := f.challenge(t, scope.ID, "controlled-transfer-demo-challenge")
			c = f.prove(t, f.admin, platform, c, f.nativeClaimant, f.nativeGroup)
			if !c.TransferRequired || c.Transfer == nil || c.Transfer.SourceTenantID != f.tenantA ||
				c.ProofKind != "controlled_test" || c.ControlledScope == nil || c.ControlledScope.ID != scope.ID {
				t.Fatal("controlled challenge hid its previous tenant or lost proof origin", c)
			}
			raw := onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/confirm",
				"controlled-transfer-request-review", map[string]int64{"version": c.Version}, http.StatusOK)
			var review control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &review); err != nil || review.State != "review" ||
				review.Transfer == nil || review.ProofKind != "controlled_test" {
				t.Fatal("controlled proof bypassed explicit transfer review", review, err)
			}
			var retained bool
			err := f.db.Platform.QueryRow(ctx, `SELECT b.tenant_id=$2 AND o.proof_kind='authenticated_message'
				AND o.controlled_scope_id IS NULL AND b.config->'blocked_words'='["source-only"]'::jsonb
				AND EXISTS(SELECT 1 FROM group_claims WHERE onboarding_challenge_id=$3 AND proof_method='controlled_test')
				AND NOT EXISTS(SELECT 1 FROM customer_group_transfers WHERE group_id=b.group_id)
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id WHERE b.group_id=$1`,
				f.group, f.tenantA, c.ID).Scan(&retained)
			if err != nil || !retained {
				t.Fatal("ordinary confirmation replaced the source tenant or relabeled historical proof", err)
			}
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/review", "",
				map[string]any{"version": review.Version, "approve": true}, http.StatusConflict)
			body := map[string]any{"version": review.Version, "approve": true,
				"source_tenant_id": f.tenantA, "binding_version": review.Transfer.BindingVersion}
			raw = onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/transfer-review",
				"controlled-transfer-explicit-approval", body, http.StatusOK)
			var bound control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &bound); err != nil || bound.State != "bound" ||
				bound.ProofKind != "controlled_test" || bound.ControlledScope == nil || bound.ControlledScope.ID != scope.ID {
				t.Fatal("approved transfer lost controlled proof in HTTP progress", bound, err)
			}
			calls := f.native.roles.Load()
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/transfer-review",
				"controlled-transfer-explicit-approval", body, http.StatusOK)
			err = f.db.Platform.QueryRow(ctx, `SELECT b.tenant_id=$2 AND b.primary_account_id=$5
				AND b.version=2 AND b.epoch=2 AND b.config='{}'::jsonb
				AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT r.enabled
				AND o.tenant_id=b.tenant_id AND o.proof_kind='controlled_test' AND o.controlled_scope_id=$3
				AND o.proof_challenge_id=$4 AND o.state='active'
				AND (SELECT count(*) FROM customer_group_transfers tr WHERE tr.group_id=b.group_id
					AND tr.source_tenant_id=$6 AND tr.target_tenant_id=$2 AND tr.archived_settings_sealed IS NOT NULL)=1
				AND (SELECT count(*) FROM ledger_entries l WHERE l.group_id=b.group_id AND l.tenant_id=$6)=1
				AND NOT EXISTS(SELECT 1 FROM ledger_entries l WHERE l.group_id=b.group_id AND l.tenant_id=$2)
				AND EXISTS(SELECT 1 FROM group_claims WHERE onboarding_challenge_id=$4
					AND proof_method='controlled_test' AND state='approved')
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id
				JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`,
				f.group, scope.TenantID, scope.ID, c.ID, scope.AccountID, f.tenantA).Scan(&retained)
			if err != nil || !retained || f.native.roles.Load() != calls || f.native.invites.Load() != 0 ||
				f.native.sends.Load() != 0 {
				t.Fatal("controlled transfer lost source history, repeated native work or activated runtime", err)
			}
		})
	}
}

func TestControlledOnboardingPostgresLegacyProofCannotBorrowScope(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			base := newOnboardingFixture(t)
			admin := base.admin
			admin.workspace = platform
			legacy := base.createChallenge(t, admin, platform, "legacy-authenticated-create")
			legacy = base.prove(t, admin, platform, legacy, base.native.owner,
				"synthetic-controlled-onboarding-"+platform)
			if legacy.ProofKind != "authenticated_message" || legacy.ControlledScope != nil || !legacy.CanConfirm {
				t.Fatal("legacy fixture was not an independently authenticated proof", legacy)
			}
			// Add this identity to the synthetic pool only after the ordinary
			// proof. A later test scope cannot rewrite that proof's origin.
			f := prepareControlledOnboardingFixture(t, base, platform)
			scope := f.createScope(t, "later-scope-is-not-legacy-authority")
			path := "/group-binding/challenges/" + legacy.ID
			for _, suffix := range []string{"/confirm", "/review"} {
				onboardingAPI(t, f.admin, http.MethodPost, path+suffix, "forged-legacy-confirm-scope",
					map[string]any{"version": legacy.Version, "controlled_scope_id": scope.ID}, http.StatusBadRequest)
			}
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges", "legacy-authenticated-create",
				map[string]any{"platform": platform, "controlled_scope_id": scope.ID}, http.StatusConflict)
			onboardingAPI(t, f.admin, http.MethodPost, path+"/confirm", "legacy-still-needs-non-pool-proof",
				map[string]int64{"version": legacy.Version}, http.StatusConflict)
			ctx := context.Background()
			_, err := f.db.Platform.Exec(ctx, `UPDATE group_binding_challenges SET controlled_scope_id=$2 WHERE id=$1`,
				legacy.ID, scope.ID)
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != "23514" {
				t.Fatal("immutable challenge accepted a late controlled scope reference", err)
			}
			_, err = f.db.Platform.Exec(ctx, `INSERT INTO group_claims(tenant_id,group_id,account_id,applicant_id,
				native_identity,onboarding_challenge_id,proof_method)
				VALUES($1,$2,$3,$4,'synthetic-forged-controlled-proof',$5,'controlled_test')`,
				legacy.Scope.TenantID, f.group, legacy.Robot.ID, f.adminID, legacy.ID)
			constraint = nil
			if !errors.As(err, &constraint) || constraint.Code != "23514" {
				t.Fatal("claim trigger relabeled ordinary proof as controlled_test", err)
			}
			current := f.readChallenge(t, legacy)
			if current.ProofKind != "authenticated_message" || current.ControlledScope != nil ||
				current.State != "proven" || current.Version != legacy.Version ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
					WHERE id=$1 AND controlled_scope_id IS NULL AND identity_sealed IS NOT NULL
					AND proof_sealed IS NOT NULL`, legacy.ID) != 1 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_claims`) != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings`) != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_ownership`) != 0 ||
				f.native.sends.Load() != 0 {
				t.Fatal("forged scope upgraded legacy ownership, changed history or performed native work", current)
			}
		})
	}
}

func TestControlledOnboardingPostgresSameSpaceClosedScopeRestore(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			ctx := context.Background()
			firstScope := f.createScope(t, "controlled-same-space-first-scope")
			first := f.challenge(t, firstScope.ID, "controlled-same-space-first-challenge")
			first = f.prove(t, f.admin, platform, first, f.nativeClaimant, f.nativeGroup)
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+first.ID+"/confirm",
				"controlled-same-space-first-confirm", map[string]int64{"version": first.Version}, http.StatusOK)
			if _, err := f.db.Platform.Exec(ctx, `UPDATE group_bindings SET config='{"blocked_words":["same-space-retain"]}'
				WHERE group_id=$1`, f.group); err != nil {
				t.Fatal(err)
			}
			closed := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
				controlledOnboardingScopePath+"/"+firstScope.ID+"/close", "",
				map[string]int64{"version": firstScope.Version}, http.StatusOK))
			var closedEpoch, closedVersion int64
			var paused bool
			err := f.db.Platform.QueryRow(ctx, `SELECT b.epoch,b.version,b.active AND o.state='paused'
				AND o.controlled_scope_id=$2 AND b.config->'blocked_words'='["same-space-retain"]'::jsonb
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id WHERE b.group_id=$1`,
				f.group, firstScope.ID).Scan(&closedEpoch, &closedVersion, &paused)
			if err != nil || !paused || closed.State != "closed" {
				t.Fatal("scope closure did not retain a paused same-space binding", err)
			}
			secondScope := f.createScope(t, "controlled-same-space-new-scope")
			second := f.challenge(t, secondScope.ID, "controlled-same-space-fresh-challenge")
			if secondScope.ID == firstScope.ID || secondScope.TenantID != firstScope.TenantID ||
				second.Group == nil || second.Group.ID != f.group || second.State != "waiting" ||
				second.IdentityProven || second.CanConfirm || second.TransferRequired {
				t.Fatal("same-space restore copied the old scope or proof", secondScope, second)
			}
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+second.ID+"/confirm",
				"controlled-same-space-no-old-proof", map[string]int64{"version": second.Version}, http.StatusConflict)
			second = f.prove(t, f.admin, platform, second, f.nativeClaimant, f.nativeGroup)
			raw := onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+second.ID+"/confirm",
				"controlled-same-space-fresh-confirm", map[string]int64{"version": second.Version}, http.StatusOK)
			if field(t, raw, "state") != "bound" {
				t.Fatal("fresh controlled proof could not restore its own paused binding")
			}
			var restored bool
			err = f.db.Platform.QueryRow(ctx, `SELECT b.active AND b.tenant_id=$2
				AND b.epoch>$5 AND b.version>$6 AND b.config->'blocked_words'='["same-space-retain"]'::jsonb
				AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT r.enabled
				AND o.state='active' AND o.proof_kind='controlled_test'
				AND o.controlled_scope_id=$3 AND o.proof_challenge_id=$4
				AND NOT EXISTS(SELECT 1 FROM customer_group_transfers WHERE group_id=b.group_id)
				AND (SELECT count(*) FROM onboarding_test_scopes WHERE group_id=b.group_id)=2
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id
				JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`,
				f.group, firstScope.TenantID, secondScope.ID, second.ID, closedEpoch, closedVersion).Scan(&restored)
			if err != nil || !restored || f.native.sends.Load() != 0 || f.native.invites.Load() != 0 {
				t.Fatal("same-space restore lost configuration, proof origin or binding fences", err)
			}
			old := f.readChallenge(t, first)
			if old.State != "bound" || old.ControlledScope == nil || old.ControlledScope.ID != firstScope.ID ||
				old.ControlledScope.State != "closed" || old.ProofKind != "controlled_test" {
				t.Fatal("fresh scope relabeled the previous challenge's retained history", old)
			}
		})
	}
}

type controlledOnboardingContactNative struct {
	*bindingFriendNativeFixture
	verifyAbsence bool
}

func (n *controlledOnboardingContactNative) BindingRoles(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingRoleEvidence, error) {
	out, err := n.onboardingNativeFixture.BindingRoles(ctx, scope)
	if err == nil && !out.Present {
		out.AbsenceVerified = n.verifyAbsence
		out.SenderRole, out.RobotRole, out.NativeOwnerID = "", "", ""
	}
	return out, err
}

func newControlledOnboardingContactFixture(t *testing.T, platform string) (*controlledOnboardingFixture, *controlledOnboardingContactNative) {
	t.Helper()
	base, friend, _, _ := friendFixture(t, platform)
	f := prepareControlledOnboardingFixture(t, base, platform)
	n := &controlledOnboardingContactNative{bindingFriendNativeFixture: friend, verifyAbsence: true}
	f.app.GroupBinding.Native = n
	return f, n
}

func controlledOnboardingContactSelect(t *testing.T, f *controlledOnboardingFixture, c control.GroupBindingChallenge) control.GroupBindingChallenge {
	t.Helper()
	raw := onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/friend-candidate",
		"controlled-contact-select", map[string]any{"version": c.Version, "nickname": "Synthetic applicant"}, http.StatusOK)
	var out control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &out); err != nil || out.FriendContact == nil ||
		out.FriendContact.Status != "prepared" || out.IdentityProven || out.CanConfirm || out.Version != c.Version {
		t.Fatal("controlled contact selection established proof or lost its prepared operation", out, err)
	}
	return out
}

func controlledOnboardingContactUsage(t *testing.T, f *controlledOnboardingFixture, id string, native int) control.ControlledOnboardingScope {
	t.Helper()
	out := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodGet,
		controlledOnboardingScopePath+"/"+id, "", nil, http.StatusOK))
	if out.NativeAttempts != native || out.ModelCalls != 0 || out.ReservedUSDT != "0.000000" {
		t.Fatal("contact flow spent an unexpected controlled budget", out)
	}
	return out
}

func TestControlledOnboardingContactPostgresApproveRequiresStrictIdentity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n := newControlledOnboardingContactFixture(t, platform)
			scope := f.createScope(t, "controlled-contact-scope")
			created := f.challenge(t, scope.ID, "controlled-contact-challenge")
			path := "/group-binding/challenges/" + created.ID
			onboardingAPI(t, f.admin, http.MethodPost, path+"/friend-candidate", "controlled-contact-closed-gate",
				map[string]any{"version": created.Version, "nickname": "Synthetic applicant"}, http.StatusConflict)
			if n.lookups.Load() != 0 || n.submits.Load() != 0 {
				t.Fatal("closed friend gate called the native contact port")
			}
			friendOpenGate(t, f.onboardingFixture, platform)
			n.owner = "synthetic-unselected-contact"
			onboardingAPI(t, f.admin, http.MethodPost, path+"/friend-candidate", "controlled-contact-other-pool-sender",
				map[string]any{"version": created.Version, "nickname": "Synthetic applicant"}, http.StatusConflict)
			n.owner = f.nativeClaimant
			if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_friend_attempts`) != 0 {
				t.Fatal("contact selection admitted an unselected sender")
			}
			c := contactPrepare(t, f.onboardingFixture, f.admin, created)
			controlledOnboardingContactUsage(t, f, scope.ID, 0)
			c = contactApprove(t, f.onboardingFixture, f.admin, c)
			if c.FriendContact == nil || c.FriendContact.Status != "succeeded" ||
				c.IdentityProven || c.CanConfirm || c.CanAcceptInvitation || n.submits.Load() != 1 ||
				n.approvals.Load() != 1 || n.rejections.Load() != 0 {
				t.Fatal("pooled contact approval supplied private or group identity", c)
			}
			before := customerBindingNativeCalls(n.bindingFriendNativeFixture)
			contactApprove(t, f.onboardingFixture, f.admin, c)
			for _, suffix := range []string{"/invitation-prepare", "/invitation-accept"} {
				onboardingAPI(t, f.admin, http.MethodPost, path+suffix, "controlled-contact-without-private-proof",
					map[string]any{"version": c.Version, "native_group_id": f.nativeGroup}, http.StatusConflict)
			}
			if customerBindingNativeCalls(n.bindingFriendNativeFixture) != before ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM customer_group_accept_attempts`) != 0 {
				t.Fatal("approval replay or an unproven invitation reached native work")
			}
			controlledOnboardingContactUsage(t, f, scope.ID, 1)
			f.scan(t, control.OnboardingMessage{Private: true, Sender: "synthetic-other-human",
				Text: created.PrivateChallenge})
			current := f.readChallenge(t, created)
			if current.IdentityProven || current.Version != created.Version ||
				current.BlockedReason != "controlled_onboarding_claimant_mismatch" {
				t.Fatal("contact approval admitted another private sender", current)
			}
			f.scan(t, control.OnboardingMessage{Private: true, Sender: f.nativeClaimant, Text: created.PrivateChallenge})
			current = f.readChallenge(t, created)
			if !current.IdentityProven || current.State != "waiting" || current.CanConfirm {
				t.Fatal("selected pooled sender did not supply independent private proof", current)
			}
			for _, message := range []control.OnboardingMessage{
				{Group: f.nativeGroup, Sender: "synthetic-other-human", Text: created.GroupChallenge},
				{Group: "synthetic-other-group", Sender: f.nativeClaimant, Text: created.GroupChallenge},
			} {
				f.scan(t, message)
				rejected := f.readChallenge(t, created)
				if rejected.State != "waiting" || rejected.CanConfirm || rejected.Version != current.Version ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
						WHERE id=$1 AND proof_event_id IS NOT NULL`, created.ID) != 0 {
					t.Fatal("contact approval widened group or claimant proof", rejected)
				}
			}
			proven := f.prove(t, f.admin, platform, created, f.nativeClaimant, f.nativeGroup)
			raw := onboardingAPI(t, f.admin, http.MethodPost, path+"/confirm", "controlled-contact-bind",
				map[string]int64{"version": proven.Version}, http.StatusOK)
			if field(t, raw, "state") != "bound" ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_ownership
					WHERE group_id=$1 AND proof_kind='controlled_test' AND controlled_scope_id=$2
					AND proof_challenge_id=$3`, f.group, scope.ID, created.ID) != 1 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
					WHERE id=$1 AND identity_event_id IS NOT NULL AND identity_friend_proof_id IS NULL
					AND proof_event_id IS NOT NULL`, created.ID) != 1 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
				n.submits.Load() != 1 || n.sends.Load() != 0 {
				t.Fatal("controlled contact flow lost independent ingress proof or repeated an effect")
			}
			controlledOnboardingContactUsage(t, f, scope.ID, 1)
		})
	}
}

func TestControlledOnboardingContactPostgresExpiredReject(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n := newControlledOnboardingContactFixture(t, platform)
			friendOpenGate(t, f.onboardingFixture, platform)
			n.expiredContact = true
			scope := f.createScope(t, "controlled-old-contact-scope")
			created := f.challenge(t, scope.ID, "controlled-old-contact-challenge")
			n.owner = "synthetic-unselected-expired-applicant"
			status, _, code := f.admin.requestWithCaptcha(http.MethodPost,
				"/group-binding/challenges/"+created.ID+"/friend-candidate",
				map[string]any{"version": created.Version, "nickname": "Synthetic applicant"},
				http.Header{"Idempotency-Key": {"controlled-old-contact-other-sender"}})
			if status != http.StatusConflict || code != "controlled_onboarding_claimant_mismatch" ||
				n.submits.Load() != 0 || n.rejections.Load() != 0 || n.approvals.Load() != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_friend_attempts`) != 0 {
				t.Fatal("expired controlled application admitted another sender or spent a native attempt", status, code)
			}
			controlledOnboardingContactUsage(t, f, scope.ID, 0)
			n.owner = f.nativeClaimant
			c := controlledOnboardingContactSelect(t, f, created)
			if c.FriendContact.Action != "reject_expired" || !c.FriendContact.CanReject || c.FriendContact.CanApprove {
				t.Fatal("old pooled application was not a rejection-only plan", c)
			}
			path := "/group-binding/challenges/" + c.ID
			body := map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID}
			onboardingAPI(t, f.admin, http.MethodPost, path+"/friend-approve", "controlled-old-contact-cannot-approve",
				body, http.StatusConflict)
			status, _, code = f.admin.requestRaw(http.MethodPost, path+"/friend-reject", body,
				http.Header{"Idempotency-Key": {"controlled-old-contact-reject"}})
			if status != http.StatusOK || code != "ok" || n.submits.Load() != 1 {
				t.Fatal("live controlled contact did not perform its single confirmed rejection", status, code)
			}
			controlledOnboardingContactUsage(t, f, scope.ID, 1)
			onboardingAPI(t, f.admin, http.MethodPost, path+"/friend-reject", "controlled-old-contact-reject",
				body, http.StatusOK)
			current := f.readChallenge(t, created)
			if current.State != "cancelled" || current.Version != created.Version+1 ||
				current.BlockedReason != "group_binding_friend_reapply_required" || current.IdentityProven ||
				current.FriendContact == nil || current.FriendContact.ID != c.FriendContact.ID ||
				current.FriendContact.Status != "succeeded" || current.CanConfirm ||
				n.rejections.Load() != 1 || n.approvals.Load() != 0 || n.submits.Load() != 1 {
				t.Fatal("old pooled rejection granted identity or failed to require a new application", current)
			}
			before := customerBindingNativeCalls(n.bindingFriendNativeFixture)
			onboardingAPI(t, f.admin, http.MethodPost, path+"/friend-reject", "controlled-old-contact-reject",
				body, http.StatusOK)
			if customerBindingNativeCalls(n.bindingFriendNativeFixture) != before ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
					WHERE identity_sealed IS NOT NULL OR proof_sealed IS NOT NULL`) != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings`) != 0 {
				t.Fatal("old rejection replay created proof or repeated the native decision")
			}
			next := f.challenge(t, scope.ID, "controlled-old-contact-fresh-challenge")
			if next.State != "waiting" || next.IdentityProven || next.FriendContact != nil || next.CanConfirm {
				t.Fatal("fresh challenge inherited the rejected application's evidence", next)
			}
			controlledOnboardingContactUsage(t, f, scope.ID, 1)
		})
	}
}

func TestControlledOnboardingContactPostgresUnknownAfterClose(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, decision := range []string{"approve", "reject"} {
			t.Run(platform+"/"+decision, func(t *testing.T) {
				f, n := newControlledOnboardingContactFixture(t, platform)
				friendOpenGate(t, f.onboardingFixture, platform)
				n.expiredContact, n.unknown = decision == "reject", true
				scope := f.createScope(t, "controlled-contact-unknown-scope")
				created := f.challenge(t, scope.ID, "controlled-contact-unknown-challenge")
				c := controlledOnboardingContactSelect(t, f, created)
				path := "/group-binding/challenges/" + c.ID
				body := map[string]any{"version": c.Version, "operation_id": c.FriendContact.ID}
				action := path + "/friend-" + decision
				onboardingAPI(t, f.admin, http.MethodPost, action, "controlled-contact-unknown-decision", body, http.StatusOK)
				current := f.readChallenge(t, created)
				if current.FriendContact == nil || current.FriendContact.Status != "unknown" ||
					!current.FriendContact.CanReconcile || current.IdentityProven || current.CanCancel ||
					current.CanConfirm || current.CanAcceptInvitation || n.submits.Load() != 1 {
					t.Fatal("ambiguous pooled contact released its operation or granted proof", current)
				}
				ctx := context.Background()
				var sealed []byte
				if err := f.db.Platform.QueryRow(ctx, `SELECT payload_sealed FROM group_binding_friend_attempts
					WHERE id=$1`, c.FriendContact.ID).Scan(&sealed); err != nil {
					t.Fatal(err)
				}
				before := customerBindingNativeCalls(n.bindingFriendNativeFixture)
				closed := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
					controlledOnboardingScopePath+"/"+scope.ID+"/close", "",
					map[string]int64{"version": scope.Version}, http.StatusOK))
				if closed.State != "unknown" || closed.Version != scope.Version+1 ||
					closed.BlockedReason != "group_binding_operation_unresolved" ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM onboarding_test_scopes
						WHERE id=$1 AND state='closed' AND NOT onboarding_test_scope_active(id)`, scope.ID) != 1 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges c
						JOIN group_binding_friend_attempts a ON a.challenge_id=c.id
						WHERE c.id=$1 AND c.state=$2 AND c.version=$3 AND c.blocked_reason<>''
						AND group_binding_friend_pending(c.id)
						AND a.id=$4 AND a.status='unknown' AND a.attempts_used=1`,
						c.ID, created.State, created.Version+1, c.FriendContact.ID) != 1 {
					t.Fatal("closing an unknown contact discarded its history or kept execution live", closed)
				}
				for _, key := range []string{"controlled-contact-unknown-decision", "controlled-contact-no-new-attempt"} {
					onboardingAPI(t, f.admin, http.MethodPost, action, key, body, http.StatusOK)
				}
				onboardingAPI(t, f.admin, http.MethodPost, path+"/friend-candidate", "controlled-contact-after-close",
					map[string]any{"version": created.Version + 1, "nickname": "Synthetic applicant"}, http.StatusConflict)
				f.scan(t, control.OnboardingMessage{Private: true, Sender: f.nativeClaimant, Text: created.PrivateChallenge})
				f.scan(t, control.OnboardingMessage{Group: f.nativeGroup, Sender: f.nativeClaimant, Text: created.GroupChallenge})
				if err := f.app.GroupBinding.RecoverFriends(ctx); err != nil {
					t.Fatal(err)
				}
				current = f.readChallenge(t, created)
				// Scope closure blocks execution without claiming that the
				// original unknown operation has been cancelled.
				if current.State != created.State ||
					current.IdentityProven || current.CanConfirm || current.CanCancel || current.CanAcceptInvitation ||
					current.ControlledScope == nil || current.ControlledScope.State != "unknown" ||
					current.FriendContact == nil || current.FriendContact.ID != c.FriendContact.ID ||
					current.FriendContact.Status != "unknown" || current.FriendContact.CanApprove ||
					current.FriendContact.CanReject || customerBindingNativeCalls(n.bindingFriendNativeFixture) != before {
					t.Fatal("closed pooled contact replayed or consumed late proof", current)
				}
				reconcile := path + "/friend-attempts/" + c.FriendContact.ID + "/reconcile"
				n.reconcileState = "pending"
				raw := onboardingAPI(t, f.admin, http.MethodPost, reconcile, "", nil, http.StatusOK)
				if field(t, raw, "id") != c.FriendContact.ID || field(t, raw, "status") != "unknown" {
					t.Fatal("read-only reconciliation guessed a contact result")
				}
				n.reconcileState = "approved"
				if decision == "reject" {
					n.reconcileState = "rejected"
				}
				raw = onboardingAPI(t, f.admin, http.MethodPost, reconcile, "", nil, http.StatusOK)
				if field(t, raw, "id") != c.FriendContact.ID || field(t, raw, "status") != "succeeded" ||
					n.submits.Load() != 1 || n.reconciles.Load() != 2 {
					t.Fatal("closed contact reconciliation replaced or replayed its original decision")
				}
				before = customerBindingNativeCalls(n.bindingFriendNativeFixture)
				onboardingAPI(t, f.admin, http.MethodPost, reconcile, "", nil, http.StatusOK)
				var retained []byte
				if err := f.db.Platform.QueryRow(ctx, `SELECT payload_sealed FROM group_binding_friend_attempts
					WHERE id=$1 AND attempts_used=1`, c.FriendContact.ID).Scan(&retained); err != nil {
					t.Fatal(err)
				}
				wantState := created.State
				if decision == "reject" {
					wantState = "cancelled"
				}
				current = f.readChallenge(t, created)
				if string(retained) != string(sealed) || customerBindingNativeCalls(n.bindingFriendNativeFixture) != before ||
					current.State != wantState ||
					current.IdentityProven || current.CanConfirm || current.CanCancel || current.CanAcceptInvitation ||
					current.FriendContact == nil || current.FriendContact.ID != c.FriendContact.ID ||
					current.FriendContact.Status != "succeeded" || current.FriendContact.CanApprove || current.FriendContact.CanReject ||
					current.ControlledScope == nil || current.ControlledScope.State != "closed" ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
						WHERE id=$1 AND identity_sealed IS NULL AND proof_sealed IS NULL`, created.ID) != 1 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings`) != 0 {
					t.Fatal("contact reconciliation rewrote the sealed request or established ownership", current)
				}
				if out := controlledOnboardingContactUsage(t, f, scope.ID, 1); out.State != "closed" {
					t.Fatal("resolved contact reactivated its closed scope", out)
				}
			})
		}
	}
}

func controlledOnboardingContactInvitation(t *testing.T, platform string) (*controlledOnboardingFixture, *controlledOnboardingContactNative, control.ControlledOnboardingScope, control.GroupBindingChallenge) {
	t.Helper()
	f, n := newControlledOnboardingContactFixture(t, platform)
	n.absent, n.verifyAbsence = true, false
	onboardingAPI(t, f.admin, http.MethodPost, controlledOnboardingScopePath, "controlled-unverified-absence",
		f.input(), http.StatusConflict)
	if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM onboarding_test_scopes`) != 0 {
		t.Fatal("unverified robot absence created controlled authority")
	}
	n.verifyAbsence = true
	scope := f.createScope(t, "controlled-contact-invitation-scope")
	if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM onboarding_test_scopes
		WHERE id=$1 AND claimant_role='' AND robot_role='' AND owner_identity_hash IS NULL`, scope.ID) != 1 {
		t.Fatal("verified absence manufactured initial native roles")
	}
	friendOpenGate(t, f.onboardingFixture, platform)
	created := f.challenge(t, scope.ID, "controlled-contact-invitation-challenge")
	c := contactPrepare(t, f.onboardingFixture, f.admin, created)
	c = contactApprove(t, f.onboardingFixture, f.admin, c)
	if c.IdentityProven || n.submits.Load() != 1 || c.FriendContact.Status != "succeeded" {
		t.Fatal("invitation prerequisite treated friendship as private identity", c)
	}
	path := "/group-binding/challenges/" + c.ID
	before := customerBindingNativeCalls(n.bindingFriendNativeFixture)
	for _, suffix := range []string{"/invitation-prepare", "/invitation-accept"} {
		onboardingAPI(t, f.admin, http.MethodPost, path+suffix, "controlled-absence-no-private-proof",
			map[string]any{"version": c.Version, "native_group_id": f.nativeGroup}, http.StatusConflict)
	}
	if customerBindingNativeCalls(n.bindingFriendNativeFixture) != before ||
		friendCount(t, f.onboardingFixture, `SELECT count(*) FROM customer_group_accept_attempts`) != 0 {
		t.Fatal("verified absence spent an invitation without private proof")
	}
	controlledOnboardingContactUsage(t, f, scope.ID, 1)
	f.scan(t, control.OnboardingMessage{Private: true, Sender: f.nativeClaimant, Text: created.PrivateChallenge})
	c = f.readChallenge(t, created)
	if !c.IdentityProven || !c.CanAcceptInvitation || c.CanConfirm || c.State != "waiting" {
		t.Fatal("verified absence blocked the selected claimant's privately proven invitation", c)
	}
	onboardingAPI(t, f.admin, http.MethodPost, path+"/invitation-prepare", "",
		map[string]any{"version": c.Version, "native_group_id": "synthetic-unselected-invitation-group"}, http.StatusConflict)
	body := map[string]any{"version": c.Version, "native_group_id": f.nativeGroup}
	onboardingAPI(t, f.admin, http.MethodPost, path+"/invitation-accept", "controlled-contact-closed-invitation-gate",
		body, http.StatusConflict)
	if _, err := f.db.Platform.Exec(context.Background(), `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
		VALUES($1,'group_accept','onboarding','open',ARRAY['synthetic-controlled-contact-invitation'])
		ON CONFLICT(platform,action,source) DO UPDATE SET state='open',version=business_capability_gates.version+1`,
		platform); err != nil {
		t.Fatal(err)
	}
	raw := onboardingAPI(t, f.admin, http.MethodPost, path+"/invitation-prepare", "", body, http.StatusOK)
	if field(t, raw, "native_group_id") != f.nativeGroup || n.sends.Load() != 0 ||
		friendCount(t, f.onboardingFixture, `SELECT count(*) FROM customer_group_accept_attempts`) != 0 {
		t.Fatal("invitation preview changed scope or submitted acceptance")
	}
	controlledOnboardingContactUsage(t, f, scope.ID, 1)
	c.PrivateChallenge, c.GroupChallenge = created.PrivateChallenge, created.GroupChallenge
	return f, n, scope, c
}

func TestControlledOnboardingContactPostgresInvitationRequiresFreshRoles(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, scope, c := controlledOnboardingContactInvitation(t, platform)
			path := "/group-binding/challenges/" + c.ID
			body := map[string]any{"version": c.Version, "native_group_id": f.nativeGroup}
			raw := onboardingAPI(t, f.admin, http.MethodPost, path+"/invitation-accept", "controlled-contact-invitation-once",
				body, http.StatusOK)
			if field(t, raw, "status") != "succeeded" || n.sends.Load() != 1 || n.submits.Load() != 1 {
				t.Fatal("selected pooled claimant's invitation was not accepted once")
			}
			current := f.readChallenge(t, c)
			if current.State != "waiting" || !current.IdentityProven || current.CanConfirm ||
				current.AcceptAttempt == nil || current.AcceptAttempt.ID != field(t, raw, "id") ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings`) != 0 {
				t.Fatal("invitation acknowledgement supplied native group ownership", current)
			}
			before := customerBindingNativeCalls(n.bindingFriendNativeFixture)
			onboardingAPI(t, f.admin, http.MethodPost, path+"/invitation-accept", "controlled-contact-invitation-once",
				body, http.StatusOK)
			if customerBindingNativeCalls(n.bindingFriendNativeFixture) != before {
				t.Fatal("controlled invitation replay repeated native work")
			}
			f.scan(t, control.OnboardingMessage{Group: f.nativeGroup, Sender: f.nativeClaimant, Text: c.GroupChallenge})
			current = f.readChallenge(t, c)
			if current.State != "waiting" || current.CanConfirm || current.BlockedReason != "group_binding_native_role_required" {
				t.Fatal("verified absence substituted for fresh in-group role proof", current)
			}
			n.absent, n.owner = false, "synthetic-current-group-owner"
			f.scan(t, control.OnboardingMessage{Group: f.nativeGroup, Sender: f.nativeClaimant, Text: c.GroupChallenge})
			current = f.readChallenge(t, c)
			if current.State != "waiting" || current.CanConfirm || current.BlockedReason != "group_binding_native_role_required" {
				t.Fatal("ordinary member claimant inherited the initial absence authorization", current)
			}
			n.owner = f.nativeClaimant
			f.scan(t, control.OnboardingMessage{Group: f.nativeGroup, Sender: f.nativeClaimant, Text: c.GroupChallenge})
			current = f.readChallenge(t, c)
			if current.State != "proven" || !current.CanConfirm || current.ProofRole != "owner" {
				t.Fatal("fresh selected owner proof could not complete invitation onboarding", current)
			}
			roles := n.roles.Load()
			raw = onboardingAPI(t, f.admin, http.MethodPost, path+"/confirm", "controlled-contact-invitation-bind",
				map[string]int64{"version": current.Version}, http.StatusOK)
			if field(t, raw, "state") != "bound" || n.roles.Load() != roles+1 ||
				n.sends.Load() != 1 || n.submits.Load() != 1 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_ownership
					WHERE group_id=$1 AND controlled_scope_id=$2 AND proof_kind='controlled_test' AND state='active'`,
					f.group, scope.ID) != 1 {
				t.Fatal("final invitation binding skipped a fresh role read or changed proof origin")
			}
			controlledOnboardingContactUsage(t, f, scope.ID, 2)
		})
	}
}

func TestControlledOnboardingContactPostgresInvitationFinalScopeClosure(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, scope, c := controlledOnboardingContactInvitation(t, platform)
			ref := onboardingRef(t, f.admin, platform)
			n.beforeSubmit = func(ctx context.Context, request control.GroupBindingAcceptRequest) error {
				if request.NativeGroupID != f.nativeGroup || request.NativeSenderID != f.nativeClaimant {
					return errors.New("synthetic invitation escaped its selected scope")
				}
				_, err := f.app.GroupBinding.CloseControlledScope(ctx, ref, scope.ID, scope.Version)
				return err
			}
			path := "/group-binding/challenges/" + c.ID
			body := map[string]any{"version": c.Version, "native_group_id": f.nativeGroup}
			raw := onboardingAPI(t, f.admin, http.MethodPost, path+"/invitation-accept", "controlled-contact-final-close",
				body, http.StatusOK)
			id := field(t, raw, "id")
			if !control.ValidID(id) || field(t, raw, "status") != "failed" ||
				field(t, raw, "result_code") != "controlled_onboarding_scope_closed" || n.sends.Load() != 0 ||
				n.submits.Load() != 1 {
				t.Fatal("invitation crossed scope closure at the final native fence")
			}
			before := customerBindingNativeCalls(n.bindingFriendNativeFixture)
			raw = onboardingAPI(t, f.admin, http.MethodPost, path+"/invitation-accept", "controlled-contact-final-close",
				body, http.StatusOK)
			onboardingAPI(t, f.admin, http.MethodPost, path+"/accept-attempts/"+id+"/reconcile", "", nil, http.StatusOK)
			f.scan(t, control.OnboardingMessage{Group: f.nativeGroup, Sender: f.nativeClaimant, Text: c.GroupChallenge})
			current := f.readChallenge(t, c)
			if field(t, raw, "id") != id || customerBindingNativeCalls(n.bindingFriendNativeFixture) != before ||
				current.State != "cancelled" || current.CanConfirm || current.CanAcceptInvitation ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM customer_group_accept_attempts
					WHERE id=$1 AND status='failed' AND payload_sealed IS NOT NULL`, id) != 1 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_challenges
					WHERE id=$1 AND proof_event_id IS NOT NULL`, c.ID) != 0 ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings`) != 0 {
				t.Fatal("closed invitation replayed, discarded its ID or established ownership", current)
			}
			if out := controlledOnboardingContactUsage(t, f, scope.ID, 2); out.State != "closed" {
				t.Fatal("failed final invitation reopened or refunded its closed scope", out)
			}
		})
	}
}

func TestControlledOnboardingContactPostgresGroupCollectionsIsolation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledOnboardingFixture(t, platform)
			scope := f.createScope(t, "controlled-group-collections-scope")
			c := f.challenge(t, scope.ID, "controlled-group-collections-challenge")
			c = f.prove(t, f.admin, platform, c, f.nativeClaimant, f.nativeGroup)
			onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+c.ID+"/confirm",
				"controlled-group-collections-bind", map[string]int64{"version": c.Version}, http.StatusOK)
			other := controlledOnboardingOtherAdmin(t, f)
			calls := f.native.roles.Load()
			type listedGroup struct {
				ID       string `json:"group_id"`
				Tenant   string `json:"tenant_id"`
				Platform string `json:"platform"`
			}
			assertCollections := func(client testClient, want int) {
				t.Helper()
				assertRows := func(rows []listedGroup) {
					t.Helper()
					if len(rows) != want {
						t.Fatal("group collection leaked or hid a controlled binding", len(rows), want)
					}
					for _, row := range rows {
						if row.ID != f.group || row.Tenant != scope.TenantID || row.Platform != platform {
							t.Fatal("group collection changed controlled identity", row)
						}
					}
				}
				for _, query := range []string{
					"?platform=" + platform,
					"?platform=" + platform + "&workspace=all",
					"?platform=" + platform + "&workspace=all&tenant_id=" + scope.TenantID,
				} {
					var page struct {
						Items, Groups []listedGroup
						Total         int
					}
					raw := onboardingAPI(t, client, http.MethodGet, "/user-center/groups"+query, "", nil, http.StatusOK)
					if err := json.Unmarshal(raw, &page); err != nil || page.Total != want {
						t.Fatal("user-center collection count leaked controlled scope", page.Total, want, err)
					}
					assertRows(page.Items)
					assertRows(page.Groups)
				}
				var rows []listedGroup
				raw := onboardingAPI(t, client, http.MethodGet, "/groups", "", nil, http.StatusOK)
				if err := json.Unmarshal(raw, &rows); err != nil {
					t.Fatal(err)
				}
				assertRows(rows)
			}
			assertCollections(f.admin, 1)
			assertCollections(other, 0)
			onboardingAPI(t, other, http.MethodPost, controlledOnboardingScopePath+"/"+scope.ID+"/close", "",
				map[string]int64{"version": scope.Version}, http.StatusNotFound)
			assertCollections(f.admin, 1)
			closed := controlledOnboardingScope(t, onboardingAPI(t, f.admin, http.MethodPost,
				controlledOnboardingScopePath+"/"+scope.ID+"/close", "",
				map[string]int64{"version": scope.Version}, http.StatusOK))
			if closed.State != "closed" {
				t.Fatal("collection isolation fixture did not close its scope", closed)
			}
			assertCollections(f.admin, 0)
			assertCollections(other, 0)
			if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings b
				JOIN group_binding_ownership o ON o.group_id=b.group_id
				WHERE b.group_id=$1 AND b.active AND b.tenant_id=$2 AND o.state='paused'
				AND o.proof_kind='controlled_test' AND o.controlled_scope_id=$3`,
				f.group, scope.TenantID, scope.ID) != 1 ||
				f.native.roles.Load() != calls || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("collection filtering erased controlled history or invoked native work")
			}
			if old := f.readChallenge(t, c); old.State != "bound" || old.ControlledScope == nil ||
				old.ControlledScope.State != "closed" {
				t.Fatal("hidden group lost its owner's historical proof", old)
			}
		})
	}
}
