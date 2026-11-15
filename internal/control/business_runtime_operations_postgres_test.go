package control

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPostgresSafeBusinessOperationsProjectOnlyCurrentAuthorizedMetadata(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newBusinessFixture(t)
			account := f.account(platform, 1)
			scope := f.group(platform, account, f.tenant, 1, false)
			actor := Actor{ID: f.actor, TenantID: f.tenant, Role: "user"}
			legacy, automatic, heat := NewID(), NewID(), NewID()
			f.exec(`INSERT INTO operations(id,tenant_id,actor_id,group_id,account_id,idempotency_key,
				request_hash,kind,target_id,payload_sealed,account_epoch,status,expires_at,result_code)
				VALUES($1,$2,$3,$4,$5,$1::uuid::text,$6,'send_text','PRIVATE-TARGET',$6,1,
				'succeeded',clock_timestamp()+interval '1 minute','PRIVATE-UPSTREAM-REASON')`,
				legacy, f.tenant, f.actor, scope.Group, account, []byte("PRIVATE-SEALED-PAYLOAD"))
			f.exec(`INSERT INTO business_rule_jobs(id,platform,tenant_id,actor_id,account_id,group_id,
				event_id,rule,action,priority,scope_snapshot,payload_sealed,status,result_code,expires_at)
				VALUES($1,$2,$3,$4,$5,$6,1,'keyword_replies','send_text',1,$7,$7,
				'succeeded','PRIVATE-UPSTREAM-REASON',clock_timestamp()+interval '1 minute')`,
				automatic, platform, f.tenant, f.actor, account, scope.Group, []byte("PRIVATE-RULE-PAYLOAD"))
			chat := NewID()
			f.exec(`INSERT INTO accounts(id,name,platform,workspace,purpose)
				VALUES($1,'synthetic-chat',$2,$2,'chat')`, chat, platform)
			f.exec(`UPDATE group_bindings SET ai_account_id=$2 WHERE group_id=$1`, scope.Group, chat)
			f.exec(`INSERT INTO heat_chat_jobs(id,platform,tenant_id,actor_id,account_id,group_id,event_id,directed,
				scope_digest,scope_sealed,response_sealed,status,result_code,expires_at)
				VALUES($1,$2,$3,$4,$5,$6,1,false,$7,$8,$8,'sent','PRIVATE-UPSTREAM-REASON',
				clock_timestamp()+interval '1 minute')`,
				heat, platform, f.tenant, f.actor, chat, scope.Group, bytes.Repeat([]byte{31}, 32), []byte("PRIVATE-MODEL-TEXT"))

			read := func(actor Actor, group string) []map[string]json.RawMessage {
				t.Helper()
				raw, err := SafeBusinessOperations(f.ctx, f.db.Platform, actor, platform, group, 100)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{"PRIVATE-", "target", "payload", "scope_snapshot", "scope_sealed", "response_sealed", "prompt"} {
					if strings.Contains(string(raw), forbidden) {
						t.Fatal("private operation data reached metadata projection", forbidden)
					}
				}
				var rows []map[string]json.RawMessage
				if err = json.Unmarshal(raw, &rows); err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					for key := range row {
						switch key {
						case "id", "source", "execution_mode", "operator_demo", "kind", "status", "result_code", "group_id", "account_id", "created_at", "updated_at":
						default:
							t.Fatal("unexpected projected operation field", key)
						}
					}
				}
				return rows
			}
			rows := read(actor, scope.Group)
			if len(rows) != 3 {
				t.Fatal("durable business sources were omitted", len(rows))
			}
			sources := map[string]bool{}
			for _, row := range rows {
				var source, status, code string
				json.Unmarshal(row["source"], &source)
				json.Unmarshal(row["status"], &status)
				json.Unmarshal(row["result_code"], &code)
				sources[source] = true
				if status != "succeeded" || code != "operation_succeeded" {
					t.Fatal("result metadata was not normalized", source, status, code)
				}
			}
			if !sources["business"] || !sources["automatic"] || !sources["heatbot"] {
				t.Fatal("missing business source", sources)
			}
			otherTenant, otherActor := f.customer()
			if rows = read(Actor{ID: otherActor, TenantID: otherTenant, Role: "user"}, ""); len(rows) != 0 {
				t.Fatal("cross-tenant history leaked", len(rows))
			}
			member := NewID()
			f.exec(`INSERT INTO users(id,tenant_id,username,password_hash,role,tenant_role)
				VALUES($1::uuid,$2,$1::uuid::text,'synthetic-unused','user','viewer')`, member, f.tenant)
			f.exec(`INSERT INTO user_access(user_id,role_code,permissions) VALUES($1,'viewer',ARRAY['operations:read'])`, member)
			memberActor := Actor{ID: member, TenantID: f.tenant, Role: "user"}
			if rows = read(memberActor, ""); len(rows) != 0 {
				t.Fatal("tenant membership implied group access")
			}
			f.exec(`INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3)`, member, f.tenant, scope.Group)
			if rows = read(memberActor, ""); len(rows) != 3 {
				t.Fatal("explicit member group did not receive safe business history", len(rows))
			}
			f.exec(`DELETE FROM member_groups WHERE user_id=$1`, member)
			if rows = read(memberActor, ""); len(rows) != 0 {
				t.Fatal("revoked member group retained history", len(rows))
			}
			f.exec(`UPDATE group_bindings SET primary_account_id=$2 WHERE group_id=$1`, scope.Group, f.account(platform, 2))
			rows = read(actor, "")
			if len(rows) != 1 || string(rows[0]["source"]) != `"heatbot"` {
				t.Fatal("replaced management account retained old operations", rows)
			}
			f.exec(`UPDATE tenant_capability_ceilings SET permissions='{}' WHERE tenant_id=$1`, f.tenant)
			_, err := SafeBusinessOperations(f.ctx, f.db.Platform, actor, platform, "", 100)
			capabilityError(t, err, "permission_denied")
		})
	}
}
