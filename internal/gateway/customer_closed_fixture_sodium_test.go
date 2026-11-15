//go:build sodium && cgo

package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/features"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func customerClosedQuantile(input []float64, q float64) float64 {
	if len(input) == 0 {
		return 0
	}
	values := slices.Clone(input)
	slices.Sort(values)
	position := q * float64(len(values)-1)
	low, high := int(position), int(math.Ceil(position))
	return values[low] + (values[high]-values[low])*(position-float64(low))
}

func customerClosedGroup(position, groups int) int {
	if position%2 == 0 {
		return 0
	}
	return 1 + (position/2)%(groups-1)
}

func customerClosedNativeGroup(platform string, group int) string {
	if platform == "wsl" {
		return strconv.Itoa(456 + group)
	}
	return fmt.Sprintf("group-%d", group)
}

// All eight classes are valid in the frozen v54 parsers on both platforms.
// This fixture constructs inputs; it does not copy a parser or an IO optimizer.
func customerClosedSources(t testing.TB, platform string, groups int) [][]byte {
	t.Helper()
	key := bytes.Repeat([]byte{17}, 32)
	out := make([][]byte, 0, 8*groups)
	for group := range groups {
		for class := range 8 {
			text := fmt.Sprintf("synthetic text class %d", class)
			if class == 1 {
				// Stay below the production 4096-byte text limit while
				// exercising a materially larger payload than the short case.
				text = strings.Repeat("synthetic long text ", 180)
			}
			var raw []byte
			var err error
			// Keep the fixture inside the same UUID contract as production
			// outbound messages while remaining deterministic across runs.
			client := fmt.Sprintf("00000000-0000-4000-8000-%012x", group*8+class+1)
			if platform == "lajiaoliao" {
				content, e := json.Marshal(struct {
					Content string `json:"content"`
				}{text})
				if e != nil {
					t.Fatal(e)
				}
				raw, err = proto.Marshal(&sdkws.MsgData{SendID: "human", RecvID: "group-recipient",
					GroupID: customerClosedNativeGroup(platform, group), ClientMsgID: client,
					ServerMsgID:    fmt.Sprintf("synthetic-server-%d-%d", group, class),
					SenderNickname: "synthetic", SessionType: 3, MsgFrom: 100, ContentType: 101,
					Content: content, SendTime: 1700000000123})
			} else {
				message := nativewire.GroupText{SenderID: 123, GroupID: uint64(456 + group),
					ClientID: client, CreatedAt: time.UnixMilli(1700000000123), Text: text}
				encoded, e := nativewire.SealGroupText(key, message, uint64(1000+group*8+class), 3)
				if e != nil {
					t.Fatal(e)
				}
				raw, err = json.Marshal(nativewire.IMMessage{Scene: 1,
					Target: fmt.Sprintf("team-%d", group), Sender: "peer",
					Fields: map[uint32]string{8: "100", 10: encoded}})
			}
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, raw)
		}
	}
	return out
}

func customerClosedFixture(t *testing.T, db *pgxpool.Pool, platform string, groups int) (*security.Vault, []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := json.Marshal(nativeauth.Deployment{ProtocolVersion: nativeauth.DesktopProtocolVersion,
		MessageKey: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := vault.Seal("native-deployment:v1", deployment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,1)
		ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=1`, sealed); err != nil {
		t.Fatal(err)
	}
	accounts := make([]string, (groups+29)/30)
	for i := range accounts {
		self := strconv.Itoa(10000 + i)
		accounts[i] = fmt.Sprintf("00000000-0000-4000-8100-%012x", i+1)
		_, err = db.Exec(ctx, `INSERT INTO accounts(id,name,platform,auth_state,native_uid)
			VALUES($1,$2,$3,'authenticated',$4)`,
			accounts[i], fmt.Sprintf("customer-closed-%d", i), platform, self)
		if err != nil {
			t.Fatal(err)
		}
		var session []byte
		aad := "native-session:" + accounts[i]
		if platform == "wsl" {
			session, err = json.Marshal(savedNativeSession{DeploymentVersion: 1,
				Session: nativeauth.Session{UID: uint64(10000 + i), NIMID: self}})
		} else {
			aad = "lajiaoliao-session:" + accounts[i]
			session, err = json.Marshal(lajiaoliao.Session{UserID: self, IMToken: "synthetic", ChatToken: "synthetic"})
		}
		if err != nil {
			t.Fatal(err)
		}
		sealed, err = vault.Seal(aad, session)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `UPDATE accounts SET credential_sealed=$1 WHERE id=$2`, sealed, accounts[i]); err != nil {
			t.Fatal(err)
		}
	}
	tenant := "00000000-0000-4000-8300-000000000001"
	if _, err = db.Exec(ctx, `INSERT INTO tenants(id,name) VALUES($1,'synthetic closed consumer')`, tenant); err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(features.Config{BlockedWords: []string{"synthetic"},
		KeywordReplies: []features.KeywordRule{{Keyword: "synthetic", Reply: "must never send"}},
		FeatureOptions: map[string]features.Options{"blocked_words": {Enabled: true}, "keyword_replies": {Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range groups {
		group := fmt.Sprintf("00000000-0000-4000-8200-%012x", i+1)
		if _, err = db.Exec(ctx, `INSERT INTO native_groups(id,platform,native_id,name)
			VALUES($1,$2,$3,'synthetic closed group')`,
			group, platform, customerClosedNativeGroup(platform, i)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,active,management_enabled,config)
			VALUES($1,$2,$3,true,true,$4::jsonb)`, group, tenant, accounts[i/30], config); err != nil {
			t.Fatal(err)
		}
	}
	var maximum int
	if err = db.QueryRow(ctx, `SELECT max(n) FROM (SELECT count(*) n FROM group_bindings
		GROUP BY primary_account_id) counts`).Scan(&maximum); err != nil || maximum > 30 {
		t.Fatal("synthetic binding capacity mismatch", maximum, err)
	}
	customerClosedAssertIdle(t, db, groups)
	return vault, accounts
}

func customerClosedInsert(t *testing.T, db *pgxpool.Pool, vault *security.Vault, accounts []string, sources [][]byte, count, offset int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	values := make([][]any, count)
	now := time.Now()
	for i := range count {
		position := i + offset
		group := customerClosedGroup(position, len(sources)/8)
		account := accounts[group/30]
		key := fmt.Sprintf("customer-closed-%d", position)
		sealed, err := vault.Seal("native-event:"+account+":"+key, sources[group*8+position%8])
		if err != nil {
			t.Fatal(err)
		}
		values[i] = []any{account, key, "group", now, sealed}
	}
	written, err := db.CopyFrom(ctx, pgx.Identifier{"native_received_events"},
		[]string{"account_id", "event_key", "kind", "received_at", "payload_sealed"}, pgx.CopyFromRows(values))
	if err != nil || written != int64(count) {
		t.Fatal("insert synthetic sealed input", written, err)
	}
}

func customerClosedAssertIdle(t *testing.T, db *pgxpool.Pool, groups int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var total, enabled, work int
	err := db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM group_business_runtime),
		(SELECT count(*) FROM group_business_runtime WHERE enabled OR enabled_at IS NOT NULL),
		(SELECT count(*) FROM business_rule_consumers)+(SELECT count(*) FROM business_rule_receipts)
			+(SELECT count(*) FROM business_rule_jobs)+(SELECT count(*) FROM business_rule_attempts)
			+(SELECT count(*) FROM group_binding_challenges)+(SELECT count(*) FROM group_binding_observations)`).
		Scan(&total, &enabled, &work)
	if err != nil || total != groups || enabled != 0 || work != 0 {
		t.Fatalf("closed fixture/runtime performed work: groups=%d enabled=%d work=%d err=%v", total, enabled, work, err)
	}
}

func customerClosedTestURL(raw string, container bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "127.0.0.1" || u.User == nil ||
		u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", errors.New("explicit localhost isolated database URL required")
	}
	u.Path, u.RawPath = "/wsl_test", ""
	if container {
		u.Host = "postgres:5432"
	}
	return u.String(), nil
}

func customerClosedMaintenance(t *testing.T) *pgx.Conn {
	t.Helper()
	if os.Getenv("WSL_CUSTOMER_CLOSED_PERFORMANCE") != "1" || os.Getenv("WSL_GATEWAY_INTEGRATION") != "1" {
		t.Fatal("isolated performance and gateway opt-ins required before maintenance")
	}
	raw, err := os.ReadFile("../../.env")
	if err != nil {
		t.Fatal("isolated maintenance configuration unavailable")
	}
	var dsn string
	for _, line := range strings.Split(string(raw), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok && key == "MIGRATION_DATABASE_URL" {
			dsn = value
		}
	}
	dsn, err = customerClosedTestURL(dsn, os.Getenv("WSL_CONTAINER_TEST") == "1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("isolated maintenance connection unavailable")
	}
	t.Cleanup(func() { owner.Close(context.Background()) })
	customerClosedCheckDatabase(t, owner)
	return owner
}

func customerClosedCheckDatabase(t *testing.T, owner *pgx.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var name string
	if err := owner.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "wsl_test" {
		t.Fatal("refusing non-wsl_test maintenance")
	}
}

func customerClosedMaintain(t *testing.T, owner *pgx.Conn, analyze bool) {
	t.Helper()
	customerClosedCheckDatabase(t, owner)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	queries := []string{`TRUNCATE accounts,native_groups,tenants RESTART IDENTITY CASCADE`}
	if analyze {
		queries = []string{
			`VACUUM (ANALYZE) native_received_events`,
			`VACUUM (ANALYZE) native_decoded_events`,
			`ANALYZE accounts,group_bindings,group_business_runtime`,
		}
	}
	for _, query := range queries {
		if _, err := owner.Exec(ctx, query); err != nil {
			t.Fatal("isolated fixture maintenance", err)
		}
	}
}

// Background ANALYZE can invalidate a prepared claim plan halfway through one
// variant. Keep statistics fixed for both variants without changing JIT or the
// production query, then restore these two wsl_test table options.
func customerClosedFixedStatistics(t *testing.T, owner *pgx.Conn) {
	t.Helper()
	customerClosedCheckDatabase(t, owner)
	for _, table := range []string{"native_received_events", "native_decoded_events"} {
		var previous string
		err := owner.QueryRow(context.Background(), `SELECT coalesce((
			SELECT option_value FROM pg_options_to_table(c.reloptions) WHERE option_name='autovacuum_enabled'),'')
			FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname='public' AND c.relname=$1`, table).Scan(&previous)
		if err != nil || previous != "" && previous != "true" && previous != "false" {
			t.Fatal("read isolated table maintenance option", err)
		}
		restore := `ALTER TABLE ` + table + ` RESET(autovacuum_enabled)`
		if previous != "" {
			restore = `ALTER TABLE ` + table + ` SET(autovacuum_enabled=` + previous + `)`
		}
		t.Cleanup(func() {
			customerClosedCheckDatabase(t, owner)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := owner.Exec(ctx, restore); err != nil {
				t.Error("restore isolated table maintenance option", err)
			}
		})
		if _, err = owner.Exec(context.Background(), `ALTER TABLE `+table+` SET(autovacuum_enabled=false)`); err != nil {
			t.Fatal("freeze isolated table statistics", err)
		}
	}
}

func customerClosedPreserveDeployment(t *testing.T, owner *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	var original []byte
	var version int64
	err := owner.QueryRow(ctx, `SELECT sealed,version FROM native_deployment`).Scan(&original, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		customerClosedCheckDatabase(t, owner)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var restoreErr error
		if errors.Is(err, pgx.ErrNoRows) {
			_, restoreErr = owner.Exec(ctx, `DELETE FROM native_deployment`)
		} else {
			_, restoreErr = owner.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,$2)
				ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=excluded.version`, original, version)
		}
		if restoreErr != nil {
			t.Error("restore isolated deployment", restoreErr)
		}
	})
}

func TestCustomerClosedPerformanceSources(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		sources := customerClosedSources(t, platform, 3)
		for i, source := range sources {
			var scene, conversation, state, reason string
			if platform == "wsl" {
				var outer nativewire.IMMessage
				if err := json.Unmarshal(source, &outer); err != nil {
					t.Fatal(err)
				}
				_, scene, conversation, state, reason = decodeApplication(
					base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32)),
					nativeauth.Session{UID: 10000, NIMID: "10000"}, outer)
			} else {
				_, scene, conversation, state, reason = decodeChiliMessage(source, "10000")
			}
			if state != "decoded" || scene != "group" || conversation == "" {
				t.Fatalf("%s fixture %d incompatible with frozen parser: %s/%s", platform, i, state, reason)
			}
		}
	}
}

func TestCustomerClosedPerformanceQuantile(t *testing.T) {
	input := []float64{4, 1, 3, 2}
	if got := customerClosedQuantile(input, .95); math.Abs(got-3.85) > 1e-9 ||
		!slices.Equal(input, []float64{4, 1, 3, 2}) || customerClosedQuantile(nil, .95) != 0 {
		t.Fatal("percentile or input ownership changed", got, input)
	}
}

func TestCustomerClosedPerformanceDistribution(t *testing.T) {
	for _, groups := range []int{100, 300} {
		counts := make([]int, groups)
		for i := range customerClosedSamples {
			group := customerClosedGroup(i, groups)
			if group < 0 || group >= groups || group/30 >= (groups+29)/30 {
				t.Fatal("group/account scope escaped fixture")
			}
			counts[group]++
		}
		if counts[0] != customerClosedSamples/2 || slices.Contains(counts, 0) {
			t.Fatal("hotspot or complete group coverage missing")
		}
	}
}

func TestCustomerClosedPerformanceMaintenanceURL(t *testing.T) {
	for _, raw := range []string{"", "postgres://test:synthetic@example.test/wsl",
		"file://test@127.0.0.1/wsl", "postgres://127.0.0.1/wsl"} {
		if _, err := customerClosedTestURL(raw, false); err == nil {
			t.Fatal("unsafe maintenance URL accepted")
		}
	}
	for _, container := range []bool{false, true} {
		dsn, err := customerClosedTestURL("postgres://test:synthetic@127.0.0.1:5432/wsl?sslmode=disable", container)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(dsn)
		if err != nil || u.Path != "/wsl_test" ||
			!container && u.Hostname() != "127.0.0.1" || container && u.Host != "postgres:5432" {
			t.Fatal("maintenance escaped isolated database")
		}
	}
}
