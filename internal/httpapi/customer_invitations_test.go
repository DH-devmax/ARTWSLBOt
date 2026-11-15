package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

type onboardingFixture struct {
	db                        *database.DB
	app                       *Server
	vault                     *security.Vault
	admin, alice, bob, member testClient
	adminID, aliceID, bobID   string
	tenantA, tenantB, plan    string
	accounts                  map[string]string
	native                    *onboardingNativeFixture
}

func newOnboardingFixture(t *testing.T) *onboardingFixture {
	t.Helper()
	f := &onboardingFixture{db: integrationDB(t), accounts: map[string]string{}}
	ctx := context.Background()
	f.vault, _ = security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)))
	seedPassword, err := security.Password("synthetic-onboarding-password")
	if err != nil {
		t.Fatal(err)
	}
	err = f.db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role)
		VALUES('onboarding-admin',$1,'superadmin') RETURNING id::text`, seedPassword).Scan(&f.adminID)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"customer-a", "customer-b"} {
		tx, err := f.db.Platform.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var tenant, actor string
		if err = tx.QueryRow(ctx, `INSERT INTO tenants(name) VALUES($1) RETURNING id::text`, name).Scan(&tenant); err == nil {
			err = tx.QueryRow(ctx, `INSERT INTO users(username,password_hash,role,tenant_id,tenant_role)
				VALUES($1,$2,'user',$3,'owner') RETURNING id::text`, name, seedPassword, tenant).Scan(&actor)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE tenants SET owner_id=$2 WHERE id=$1`, tenant, actor)
		}
		if err == nil {
			err = control.SeedTenantRoles(ctx, tx, tenant, tenantPermissions)
		}
		grants := []string{}
		for _, spec := range tenantPermissions {
			grants = append(grants, spec.Code)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO tenant_capability_ceilings(tenant_id,permissions) VALUES($1,$2)`, tenant, grants)
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			f.tenantA, f.aliceID = tenant, actor
		} else {
			f.tenantB, f.bobID = tenant, actor
		}
	}
	if err = f.db.Platform.QueryRow(ctx, `INSERT INTO plans(name,group_limit,ai_group_limit,modules,allowed_platforms)
		VALUES('synthetic-onboarding',10,0,ARRAY['moderation','replies'],ARRAY['wsl','lajiaoliao']) RETURNING id::text`).Scan(&f.plan); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{f.tenantA, f.tenantB} {
		if _, err = f.db.Platform.Exec(ctx, `INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenant, f.plan); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.db.Platform.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed) VALUES(true,$1)`, []byte("synthetic-deployment")); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		var account string
		err = f.db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,workspace,purpose,native_uid,native_nickname,
			auth_state,credential_sealed,connection_enabled) VALUES($1,$1,$1,'management',$2,$3,'authenticated',$4,false) RETURNING id::text`,
			platform, "synthetic-bot-"+platform, "Synthetic "+platform, []byte("synthetic-credential-"+platform)).Scan(&account)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.db.Platform.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
			VALUES($1,true,'synthetic-receiver',now()+interval '1 hour',now(),'{"state":"connected"}')`, account)
		if err != nil {
			t.Fatal(err)
		}
		f.accounts[platform] = account
	}
	f.app = newCaptchaTestServer(f.db, f.vault, "http://127.0.0.1:5173", false)
	f.native = &onboardingNativeFixture{db: f.db, owner: "synthetic-native-owner", robotRole: "admin"}
	f.app.GroupBinding.Native = f.native
	f.app.GroupBinding.Decode = f.decode
	server := httptest.NewServer(f.app.Handler())
	t.Cleanup(server.Close)
	f.admin = f.client(t, server.URL, f.adminID)
	f.alice = f.client(t, server.URL, f.aliceID)
	f.bob = f.client(t, server.URL, f.bobID)
	var member string
	if err = f.db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role,tenant_id,tenant_role)
		VALUES('customer-member',$1,'user',$2,'manager') RETURNING id::text`, seedPassword, f.tenantA).Scan(&member); err != nil {
		t.Fatal(err)
	}
	f.member = f.client(t, server.URL, member)
	return f
}

func (f *onboardingFixture) client(t *testing.T, base, actor string) testClient {
	t.Helper()
	token, _ := security.Token()
	csrf, _ := security.Token()
	if _, err := f.db.Platform.Exec(context.Background(), `INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest(csrf), actor); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(base)
	jar.SetCookies(u, []*http.Cookie{{Name: "wsl_session", Value: token, Path: "/"}, {Name: "wsl_csrf", Value: csrf, Path: "/"}})
	return testClient{base: base, client: &http.Client{Jar: jar, Timeout: 20 * time.Second}}
}

func onboardingAPI(t *testing.T, c testClient, method, path, key string, body any, want int) json.RawMessage {
	t.Helper()
	status, data, msg := c.requestWithCaptcha(method, path, body, http.Header{"Idempotency-Key": {key}})
	if status != want {
		t.Fatalf("%s %s got %d (%s), want %d", method, path, status, msg, want)
	}
	return data
}

func TestCustomerInvitationsPostgresOneTimeAndExistingLogin(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	create := map[string]any{"mode": "new", "tenant_name": "Invited customer"}
	first := onboardingAPI(t, f.admin, "POST", "/customer-invitations", "customer-invitation-once", create, 201)
	token := field(t, first, "token")
	id := field(t, first, "id")
	again := onboardingAPI(t, f.admin, "POST", "/customer-invitations", "customer-invitation-once", create, 201)
	if bytes.Contains(again, []byte(token)) || field(t, again, "id") != id {
		t.Fatal("create replay must not disclose or regenerate the invitation token")
	}
	var valid bool
	if err := f.db.Platform.QueryRow(ctx, `SELECT token_hash=$2 AND octet_length(token_hash)=32
		AND expires_at-created_at<=interval '24 hours 1 second' FROM customer_invitations WHERE id=$1`,
		id, security.Digest(token)).Scan(&valid); err != nil || !valid {
		t.Fatal("digest-only 24-hour invitation missing", err)
	}
	jar, _ := cookiejar.New(nil)
	anonymous := testClient{base: f.admin.base, client: &http.Client{Jar: jar}}
	onboardingAPI(t, anonymous, "POST", "/customer-invitations/preview", "", map[string]string{"token": token}, 200)
	onboardingAPI(t, f.alice, "POST", "/customer-invitations", "not-an-admin-key", create, 403)
	onboardingAPI(t, anonymous, "POST", "/customer-invitations/activate", "",
		map[string]string{"token": token, "username": "customer-a", "password": "a-different-secret-password"}, 409)
	newUser := onboardingAPI(t, anonymous, "POST", "/customer-invitations/activate", "",
		map[string]string{"token": token, "username": "new-invited-customer", "password": "synthetic-new-customer-password"}, 201)
	var passwordHash string
	if err := f.db.Platform.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, field(t, newUser, "user_id")).Scan(&passwordHash); err != nil ||
		!security.VerifyPassword(passwordHash, "synthetic-new-customer-password") || strings.Contains(passwordHash, "synthetic-new-customer-password") {
		t.Fatal("new customer password not stored with the website password scheme")
	}
	onboardingAPI(t, anonymous, "POST", "/customer-invitations/activate", "",
		map[string]string{"token": token, "username": "another-customer", "password": "synthetic-new-customer-password"}, 409)
	existing := onboardingAPI(t, f.admin, "POST", "/customer-invitations", "existing-invitation-key",
		map[string]string{"mode": "existing", "existing_user_id": f.bobID}, 201)
	existingToken := field(t, existing, "token")
	onboardingAPI(t, anonymous, "POST", "/customer-invitations/activate", "", map[string]string{
		"token": existingToken, "username": "customer-b", "password": "synthetic-attempted-reset"}, 401)
	onboardingAPI(t, anonymous, "POST", "/customer-invitations/accept", "", map[string]string{"token": existingToken}, 401)
	onboardingAPI(t, f.alice, "POST", "/customer-invitations/accept", "", map[string]string{"token": existingToken}, 409)
	onboardingAPI(t, f.bob, "POST", "/customer-invitations/accept", "", map[string]string{"token": existingToken}, 200)
	onboardingAPI(t, f.bob, "POST", "/customer-invitations/accept", "", map[string]string{"token": existingToken}, 409)
	var oldHash string
	if err := f.db.Platform.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, f.bobID).Scan(&oldHash); err != nil ||
		!security.VerifyPassword(oldHash, "synthetic-onboarding-password") {
		t.Fatal("existing account password changed")
	}
	var leaked bool
	if err := f.db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE detail::text LIKE '%'||$1||'%'
		OR detail::text LIKE '%synthetic-new-customer-password%')`, token).Scan(&leaked); err != nil || leaked {
		t.Fatal("secret appeared in onboarding audit", err)
	}
}
