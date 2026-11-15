// This test tool is built separately from the production API and CLI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wangshangliao/internal/security"
)

type fixtureIdentity struct {
	ID, Username, Role, TenantID, TenantRole string
	Disabled                                 bool
}

type fixtureSession struct {
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	SessionToken string    `json:"session_token"`
	CSRFToken    string    `json:"csrf_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type sessionStore interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

var fixtureUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validFixtureRequest(username, id string) bool {
	return (username == "synthetic-smoke-admin" || username == "synthetic-customer") &&
		fixtureUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

func validFixtureIdentity(user fixtureIdentity, username, id string) bool {
	if !validFixtureRequest(username, id) || user.ID != id || user.Username != username || user.Disabled {
		return false
	}
	if username == "synthetic-smoke-admin" {
		return user.Role == "superadmin" && user.TenantID == "" && user.TenantRole == "legacy"
	}
	return user.Role == "user" && fixtureUUID.MatchString(user.TenantID) && user.TenantRole == "owner"
}

func fixtureConfig(dsn string) (*pgx.ConnConfig, error) {
	if dsn == "" {
		return nil, errors.New("explicit synthetic database DSN required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Database != "wsl_test" || config.Host != "postgres" || config.Port != 5432 {
		return nil, errors.New("only the isolated postgres/wsl_test fixture is supported")
	}
	return config, nil
}

func seedSession(ctx context.Context, tx sessionStore, username, id string) (fixtureSession, error) {
	if !validFixtureRequest(username, id) {
		return fixtureSession{}, errors.New("unsupported synthetic identity")
	}
	var database string
	if err := tx.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil || database != "wsl_test" {
		return fixtureSession{}, errors.New("session fixture requires current_database() = wsl_test")
	}
	var user fixtureIdentity
	if err := tx.QueryRow(ctx, `SELECT id::text,username,role,coalesce(tenant_id::text,''),tenant_role,disabled
		FROM users WHERE id=$1 AND username=$2 FOR SHARE`, id, username).
		Scan(&user.ID, &user.Username, &user.Role, &user.TenantID, &user.TenantRole, &user.Disabled); err != nil {
		return fixtureSession{}, errors.New("synthetic user must already exist")
	}
	if !validFixtureIdentity(user, username, id) {
		return fixtureSession{}, errors.New("existing user is not the expected synthetic fixture")
	}
	token, err := security.Token()
	if err != nil {
		return fixtureSession{}, errors.New("session token generation failed")
	}
	csrf, err := security.Token()
	if err != nil {
		return fixtureSession{}, errors.New("CSRF token generation failed")
	}
	result := fixtureSession{UserID: id, Username: username, SessionToken: token, CSRFToken: csrf,
		ExpiresAt: time.Now().UTC().Add(12 * time.Hour)}
	tag, err := tx.Exec(ctx, `INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,$4)`,
		security.Digest(token), security.Digest(csrf), id, result.ExpiresAt)
	if err != nil || tag.RowsAffected() != 1 {
		return fixtureSession{}, errors.New("synthetic session insertion failed")
	}
	return result, nil
}

func run() (err error) {
	if len(os.Args) != 2 {
		return errors.New("one private output file is required")
	}
	username, id := os.Getenv("WSL_SMOKE_SESSION_USERNAME"), os.Getenv("WSL_SMOKE_SESSION_USER_ID")
	if !validFixtureRequest(username, id) {
		return errors.New("unsupported synthetic identity")
	}
	config, err := fixtureConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	// O_EXCL rejects existing files and symlinks. Tokens never go to stdout/logs.
	output, err := os.OpenFile(os.Args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("private session output could not be created")
	}
	defer func() {
		_ = output.Close()
		if err != nil {
			_ = os.Remove(os.Args[1])
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return errors.New("synthetic database connection failed")
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return errors.New("synthetic transaction failed")
	}
	defer tx.Rollback(ctx)
	session, err := seedSession(ctx, tx, username, id)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(output).Encode(session); err != nil {
		return errors.New("private session output failed")
	}
	if err = output.Close(); err != nil {
		return errors.New("private session output failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("synthetic session commit failed")
	}
	return nil
}

func main() {
	if run() != nil {
		// Database/driver errors can contain connection material.
		fmt.Fprintln(os.Stderr, "Synthetic session helper failed")
		os.Exit(1)
	}
}
