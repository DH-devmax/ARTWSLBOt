package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wangshangliao/internal/security"
)

const fixtureID = "c1640000-0000-4000-8000-000000000001"
const fixtureTenant = "c1640000-0000-4000-8000-000000000002"

type syntheticRow struct {
	values []any
	err    error
}

func (r syntheticRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected synthetic row")
	}
	for index, value := range r.values {
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

type syntheticStore struct {
	database  string
	user      fixtureIdentity
	queries   []string
	writes    int
	params    []any
	insertErr error
}

func (s *syntheticStore) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	s.queries = append(s.queries, query)
	if query == "SELECT current_database()" {
		return syntheticRow{values: []any{s.database}}
	}
	if !strings.Contains(query, "FROM users WHERE id=$1 AND username=$2 FOR SHARE") ||
		len(args) != 2 || args[0] != fixtureID || args[1] != s.user.Username {
		return syntheticRow{err: errors.New("unexpected identity query")}
	}
	u := s.user
	return syntheticRow{values: []any{u.ID, u.Username, u.Role, u.TenantID, u.TenantRole, u.Disabled}}
}

func (s *syntheticStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	if query != "INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,$4)" {
		return pgconn.CommandTag{}, errors.New("only session inserts are permitted")
	}
	s.params = args
	return pgconn.NewCommandTag("INSERT 0 1"), s.insertErr
}

func TestSessionFixtureRejectsOtherDatabasesBeforeIdentityReadsOrWrites(t *testing.T) {
	for _, name := range []string{"wsl", "postgres", "", "wsl_test_other"} {
		store := &syntheticStore{database: name}
		_, err := seedSession(context.Background(), store, "synthetic-smoke-admin", fixtureID)
		if err == nil || store.writes != 0 || len(store.queries) != 1 {
			t.Fatalf("database %q was not rejected before identity access", name)
		}
	}
}

func TestSessionFixtureOnlyAdmitsExistingSyntheticIdentities(t *testing.T) {
	admin := fixtureIdentity{ID: fixtureID, Username: "synthetic-smoke-admin", Role: "superadmin", TenantRole: "legacy"}
	for _, mutate := range []func(*fixtureIdentity){
		func(u *fixtureIdentity) { u.Username = "admin" },
		func(u *fixtureIdentity) { u.ID = fixtureTenant },
		func(u *fixtureIdentity) { u.Role = "user" },
		func(u *fixtureIdentity) { u.TenantID = fixtureTenant },
		func(u *fixtureIdentity) { u.Disabled = true },
		func(u *fixtureIdentity) { u.TenantRole = "owner" },
	} {
		user := admin
		mutate(&user)
		store := &syntheticStore{database: "wsl_test", user: user}
		if _, err := seedSession(context.Background(), store, admin.Username, fixtureID); err == nil || store.writes != 0 {
			t.Fatal("invalid fixture identity admitted")
		}
	}
	for _, request := range [][2]string{
		{"admin", fixtureID}, {"synthetic-arbitrary", fixtureID},
		{admin.Username, ""}, {admin.Username, "00000000-0000-0000-0000-000000000000"},
	} {
		store := &syntheticStore{}
		if _, err := seedSession(context.Background(), store, request[0], request[1]); err == nil || len(store.queries) != 0 {
			t.Fatal("unsupported fixture request reached the database")
		}
	}
	customer := fixtureIdentity{ID: fixtureID, Username: "synthetic-customer", Role: "user",
		TenantID: fixtureTenant, TenantRole: "owner"}
	if !validFixtureIdentity(customer, customer.Username, fixtureID) {
		t.Fatal("activated synthetic customer was rejected")
	}
	for _, mutate := range []func(*fixtureIdentity){
		func(u *fixtureIdentity) { u.Role = "superadmin" },
		func(u *fixtureIdentity) { u.TenantID = "" },
		func(u *fixtureIdentity) { u.TenantRole = "member" },
		func(u *fixtureIdentity) { u.Disabled = true },
	} {
		user := customer
		mutate(&user)
		if validFixtureIdentity(user, user.Username, fixtureID) {
			t.Fatal("invalid customer fixture was admitted")
		}
	}
}

func TestSessionFixtureUsesProductionTokensAndDigestsWithoutRoleWrites(t *testing.T) {
	for _, user := range []fixtureIdentity{
		{ID: fixtureID, Username: "synthetic-smoke-admin", Role: "superadmin", TenantRole: "legacy"},
		{ID: fixtureID, Username: "synthetic-customer", Role: "user", TenantID: fixtureTenant, TenantRole: "owner"},
	} {
		store := &syntheticStore{database: "wsl_test", user: user}
		before := time.Now()
		session, err := seedSession(context.Background(), store, user.Username, fixtureID)
		if err != nil {
			t.Fatal(err)
		}
		if len(session.SessionToken) != 43 || len(session.CSRFToken) != 43 || session.SessionToken == session.CSRFToken ||
			session.UserID != fixtureID || session.Username != user.Username {
			t.Fatal("session fixture format differs from authentication")
		}
		if store.writes != 1 || !bytes.Equal(store.params[0].([]byte), security.Digest(session.SessionToken)) ||
			!bytes.Equal(store.params[1].([]byte), security.Digest(session.CSRFToken)) ||
			store.params[2] != fixtureID || store.params[3] != session.ExpiresAt {
			t.Fatal("session insertion changed the production digest contract")
		}
		if session.ExpiresAt.Before(before.Add(12*time.Hour)) || session.ExpiresAt.After(time.Now().Add(12*time.Hour)) {
			t.Fatal("unexpected session expiry")
		}
	}
}

func TestSessionFixtureInsertionFailureReturnsNoTokens(t *testing.T) {
	store := &syntheticStore{database: "wsl_test",
		user:      fixtureIdentity{ID: fixtureID, Username: "synthetic-smoke-admin", Role: "superadmin", TenantRole: "legacy"},
		insertErr: errors.New("synthetic failure")}
	session, err := seedSession(context.Background(), store, store.user.Username, fixtureID)
	if err == nil || session.SessionToken != "" || session.CSRFToken != "" {
		t.Fatal("failed insertion returned session material")
	}
}

func TestSessionFixtureDSNIsLimitedToTheIsolatedAliasAndDatabase(t *testing.T) {
	for _, dsn := range []string{"", "postgres://postgres/wsl", "postgres://localhost/wsl_test",
		"postgres://postgres:5433/wsl_test", "postgres://postgres/wsl_test?dbname=wsl"} {
		if _, err := fixtureConfig(dsn); err == nil {
			t.Fatal("non-fixture DSN admitted")
		}
	}
	if _, err := fixtureConfig("postgres://synthetic:synthetic@postgres:5432/wsl_test?sslmode=disable"); err != nil {
		t.Fatal("explicit isolated fixture DSN rejected")
	}
}
