package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/httpapi"
	"wangshangliao/internal/metricsproc"
	"wangshangliao/internal/monitor"
	"wangshangliao/internal/security"
)

func main() {
	if err := run(); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return errors.New("usage: wsl init|migrate|bootstrap|serve|reconcile-avatar-upload")
	}
	if os.Args[1] == "init" || os.Args[1] == "dev-init" {
		return devInit()
	}
	if err := loadEnv(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Args[1] == "migrate" {
		return database.Migrate(ctx, os.Getenv("MIGRATION_DATABASE_URL"))
	}
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"), os.Getenv("TENANT_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	if os.Args[1] == "reconcile-avatar-upload" {
		return reconcileAvatarUpload(ctx, db, os.Stdin, os.Stdout)
	}
	if os.Args[1] == "bootstrap" {
		username, password := os.Getenv("BOOTSTRAP_USERNAME"), os.Getenv("BOOTSTRAP_PASSWORD")
		if len(username) < 3 || len(username) > 80 {
			return errors.New("BOOTSTRAP_USERNAME must contain 3 to 80 bytes")
		}
		hash, err := security.Password(password)
		if err != nil {
			return err
		}
		tx, err := db.Platform.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798425)`); err != nil {
			return err
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role='superadmin')`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return errors.New("a superadmin already exists; bootstrap is disabled")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES($1,$2,'superadmin')`, username, hash); err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		slog.Info("initial superadmin created", "username", username)
		return nil
	}
	if os.Args[1] != "serve" {
		return errors.New("unknown command")
	}
	if os.Getenv("ENABLE_NATIVE") == "true" {
		return errors.New("native transport is not release-verified; ENABLE_NATIVE must remain false")
	}
	origin := os.Getenv("PUBLIC_ORIGIN")
	if origin == "" {
		return errors.New("PUBLIC_ORIGIN is required")
	}
	vault, err := security.NewVault(os.Getenv("MASTER_KEY"))
	if err != nil {
		return err
	}
	server := httpapi.New(db, vault, origin, os.Getenv("COOKIE_SECURE") == "true")
	if err = server.Gateway.Recover(ctx); err != nil {
		return err
	}
	billingDone := make(chan struct{})
	go func() { defer close(billingDone); _ = server.RunBillingMaintenance(ctx) }()
	defer func() { stop(); <-billingDone }()
	if err = server.StartAssistant(ctx); err != nil {
		return err
	}
	defer server.StopAssistant()
	defer server.Native.Close()
	defer server.NativeMobile.Close()
	receiverDone := server.RunReceiver(ctx)
	defer func() { stop(); <-receiverDone }()
	if err = server.GroupBinding.Recover(ctx); err != nil {
		return err
	}
	bindingDone := server.GroupBinding.Run(ctx)
	defer func() { stop(); <-bindingDone }()
	businessDone, err := server.RunBusinessRules(ctx)
	if err != nil {
		return err
	}
	defer func() { stop(); <-businessDone }()
	heatDone, err := server.RunHeatChat(ctx)
	if err != nil {
		return err
	}
	defer func() { stop(); <-heatDone }()
	server.ResourceSampler = monitor.NewSampler("api_self", metricsproc.Reader{})
	monitorDone := server.RunMonitoring(ctx)
	defer func() { stop(); <-monitorDone }()
	nativeMaintenanceDone := server.RunNativeMaintenance(ctx)
	defer func() { stop(); <-nativeMaintenanceDone }()
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	httpServer := &http.Server{
		Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024,
	}
	// A process restart never silently replays an operation that may have reached upstream.
	if _, err = db.Platform.Exec(ctx, `UPDATE native_setting_operations SET
        status=CASE WHEN status='submitting' THEN 'unknown' ELSE 'failed' END,
        result_code='process_restarted',updated_at=now() WHERE status IN ('preparing','submitting')`); err != nil {
		return err
	}
	if err = (&control.NativeTextService{DB: db}).RecoverInterrupted(ctx); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdown)
	}()
	go retention(ctx, db, server)
	slog.Info("control plane listening", "address", addr, "native_ready", false, "ai_ready", false)
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func reconcileAvatarUpload(ctx context.Context, db *database.DB, input io.Reader, output io.Writer) error {
	var in struct {
		Session   string `json:"session"`
		Account   string `json:"account"`
		Operation string `json:"operation"`
	}
	reader := json.NewDecoder(io.LimitReader(input, 4097))
	reader.DisallowUnknownFields()
	if reader.Decode(&in) != nil || len(in.Session) > 256 || !control.ValidID(in.Account) || !control.ValidID(in.Operation) {
		return errors.New("invalid avatar reconciliation input")
	}
	var extra any
	if reader.Decode(&extra) != io.EOF {
		return errors.New("invalid avatar reconciliation input")
	}
	ref, err := control.WebsiteSession(in.Session)
	if err != nil {
		return errors.New("website session required")
	}
	vault, err := security.NewVault(os.Getenv("MASTER_KEY"))
	if err != nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := (&control.NativeSettingsService{DB: db, Vault: vault}).ResolveAvatarUpload(
		work, ref.InPlatform("lajiaoliao"), in.Account, in.Operation)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func retention(ctx context.Context, db *database.DB, server *httpapi.Server) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err := db.Platform.Exec(work, `DELETE FROM sessions WHERE expires_at<now();
                DELETE FROM claim_codes WHERE expires_at<now()-interval '1 day';
                UPDATE group_claims SET state='expired' WHERE state='pending' AND created_at<now()-interval '24 hours';
                UPDATE operations SET status='expired',result_code='deadline_elapsed',updated_at=now()
                    WHERE status='queued' AND expires_at<now()
                    AND NOT EXISTS(SELECT 1 FROM native_text_attempts t WHERE t.operation_id=operations.id);
                UPDATE operations SET payload_sealed=NULL WHERE created_at<now()-interval '7 days' AND payload_sealed IS NOT NULL;
                UPDATE native_setting_operations SET payload_sealed=NULL WHERE created_at<now()-interval '7 days' AND payload_sealed IS NOT NULL;
                DELETE FROM native_security_flows WHERE expires_at<now();
                UPDATE lajiaoliao_auth_state SET sms_phone_sealed=NULL,sms_session_hash=NULL,
                    sms_actor_id=NULL,sms_expires_at=NULL
                    WHERE sms_phone_sealed IS NOT NULL AND
                    (sms_expires_at<=now() OR sms_expires_at IS NULL AND attempted_at<now()-interval '10 minutes');
                DELETE FROM native_setting_operations WHERE created_at<now()-interval '90 days' AND status IN ('succeeded','failed');
                UPDATE inbox SET payload_sealed=NULL,state=CASE WHEN state IN('pending','unresolved') THEN 'quarantined' ELSE state END
                    WHERE received_at<now()-interval '7 days' AND payload_sealed IS NOT NULL;
                DELETE FROM native_received_events WHERE received_at<now()-interval '7 days';`)
			if err == nil {
				err = server.Assistant.Retain(work)
			}
			cancel()
			if err != nil {
				slog.Error("retention failed", "code", "database_error")
			}
		}
	}
}

func loadEnv() error {
	data, err := os.ReadFile(".env")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return errors.New("invalid .env line")
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err = os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func devInit() error {
	values := make([]string, 5)
	for i := range values {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		values[i] = hex.EncodeToString(b)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	content := fmt.Sprintf(`POSTGRES_OWNER_PASSWORD=%s
WSL_PLATFORM_PASSWORD=%s
WSL_TENANT_PASSWORD=%s
MIGRATION_DATABASE_URL=postgres://wsl_owner:%s@127.0.0.1:55439/wsl?sslmode=disable
DATABASE_URL=postgres://wsl_platform:%s@127.0.0.1:55439/wsl?sslmode=disable
TENANT_DATABASE_URL=postgres://wsl_tenant:%s@127.0.0.1:55439/wsl?sslmode=disable
MASTER_KEY=%s
HTTP_ADDR=127.0.0.1:8080
PUBLIC_ORIGIN=http://127.0.0.1:5173
COOKIE_SECURE=false
BOOTSTRAP_USERNAME=admin
BOOTSTRAP_PASSWORD=%s
AI_BRIDGE_TOKEN=%s
ENABLE_NATIVE=false
`, values[0], values[1], values[2], values[0], values[1], values[2], base64.StdEncoding.EncodeToString(key), values[3], values[4])
	f, err := os.OpenFile(".env", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("refusing to overwrite .env: %w", err)
	}
	if _, err = f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	slog.Info("local development secrets generated in .env; never use this HTTP configuration in production")
	return nil
}
