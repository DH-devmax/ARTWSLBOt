package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
	"wangshangliao/internal/heatbot"
	"wangshangliao/internal/nativewire"
)

// All native ports and model responses are synthetic. Only wsl_test is reset.
func TestPostgresHeatChatDurableDualPlatform(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"confirmed", "unknown_send", "unknown_usage", "revoked", "connection_changed", "sampled_out", "pool_robot", "deployment_changed"} {
			if platform == "lajiaoliao" && mode == "deployment_changed" {
				continue
			}
			t.Run(platform+"/"+mode, func(t *testing.T) {
				f := newCommercialFixture(t)
				owner := f.owner("chat-owner", "PRO")
				owner.client.workspace = platform
				group, account := heatFixtureGroup(t, f, owner, platform)
				if err := f.s.Gateway.Configure(f.ctx, f.adminActor, control.GatewaySettings{Enabled: true, PlatformLimit: 100_000_000_000, TestLimit: 1_000_000_000},
					control.ModelRoute{Alias: "default", Enabled: true, Verified: true, Rates: aigateway.Rates{Input: 1_000_000, Output: 1_000_000},
						Model: assistant.ModelConfig{BaseURL: "https://fixture.example/v1", Model: "synthetic", APIKey: "synthetic-key"}}); err != nil {
					t.Fatal(err)
				}
				sealed, err := f.s.Vault.Seal("maibot:"+account+":chat", []byte(`{"enabled":true,"reply_probability":0,"cooldown_seconds":30,"start_hour":0,"end_hour":24}`))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.db.Platform.Exec(f.ctx, `INSERT INTO maibot_settings(account_id,section,sealed,version,updated_by)
				VALUES($1,'chat',$2,1,$3)`, account, sealed, f.adminActor.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = f.db.Platform.Exec(f.ctx, `INSERT INTO account_receivers(account_id,enabled,owner,revision,lease_until,observed_at,snapshot)
				VALUES($1,true,'synthetic-owner',1,clock_timestamp()+interval '10 minutes',clock_timestamp(),'{"state":"connected"}')`, account); err != nil {
					t.Fatal(err)
				}
				if _, err = f.db.Platform.Exec(f.ctx, `UPDATE heat_chat_admission SET enabled=true WHERE platform=$1`, platform); err != nil {
					t.Fatal(err)
				}
				if err = control.RequireBusinessAdmission(f.ctx, f.db.Platform, platform, group, "send_text", "heatbot"); err != nil {
					t.Fatal("synthetic heat fixture lacks explicit business admission", err)
				}
				service := f.s.HeatChat
				service.Conversation = func(context.Context, control.HeatScope) (string, error) { return "synthetic-conversation", nil }
				service.Decode = func(scope control.HeatScope, _ string, id int64, _, _ []byte) (control.HeatMessage, error) {
					out := control.HeatMessage{Group: scope.NativeGroup, Sender: "human", Text: "synthetic trigger",
						ClientID: fmt.Sprintf("source-%d", id), Mentions: []string{scope.NativeSelf}}
					if mode == "sampled_out" {
						out.Mentions = nil
					}
					if mode == "pool_robot" {
						out.Sender = "management-self"
					}
					return out, nil
				}
				var calls, sends atomic.Int32
				f.s.Gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
					return fixtureModel(func(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error) {
						calls.Add(1)
						if mode == "revoked" {
							_, _ = f.db.Platform.Exec(f.ctx, `UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, group)
						}
						if mode == "connection_changed" {
							_, _ = f.db.Platform.Exec(f.ctx, `UPDATE account_receivers SET owner='synthetic-new-owner' WHERE account_id=$1`, account)
						}
						if mode == "unknown_usage" {
							return assistant.Response{Message: assistant.Message{Role: "assistant", Content: "synthetic response"}}, nil
						}
						return assistant.Response{Message: assistant.Message{Role: "assistant", Content: "synthetic response"},
							Usage: assistant.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}}, nil
					}), nil
				}
				service.FreshRole = func(context.Context, control.HeatScope) error { return nil }
				service.Generate = func(ctx context.Context, in heatbot.Request) (heatbot.Proposal, error) {
					out, e := f.s.Gateway.HeatGenerate(ctx, in.GatewayToken, control.HeatGatewayInput{ModelAlias: in.ModelAlias,
						ModelVersion: in.ModelVersion, Messages: []assistant.Message{{Role: "user", Content: "synthetic"}}})
					return heatbot.Proposal{ID: in.ID, Scope: in.Scope, BindingVersion: in.BindingVersion, AccountEpoch: in.AccountEpoch,
						PersonaVersion: in.PersonaVersion, SettingsVersion: in.Settings.Version, Text: out.Message.Content}, e
				}
				service.Send = func(ctx context.Context, in control.HeatSend, fence func(context.Context, func(context.Context) error) error) (bool, error) {
					if platform == "wsl" {
						if e := in.RecordRoute(ctx, syntheticHeatRoute()); e != nil {
							return false, e
						}
						if mode == "deployment_changed" {
							if _, e := f.db.Platform.Exec(ctx, `UPDATE native_deployment SET version=version+1`); e != nil {
								t.Fatal(e)
							}
						}
					}
					err := fence(ctx, func(context.Context) error { sends.Add(1); return nil })
					if again := fence(ctx, func(context.Context) error { sends.Add(1); return nil }); again == nil {
						t.Error("second native write allowed")
					}
					if mode == "unknown_send" {
						return false, context.DeadlineExceeded
					}
					return err == nil, err
				}
				ctx, cancel := context.WithCancel(f.ctx)
				done := service.Run(ctx)
				defer func() { cancel(); <-done }()
				// Let the consumer establish its start watermark before committing a
				// source with a lower allocated ID: discovery scans time, not max ID.
				waitHeat(t, f, `SELECT count(*) FROM heat_chat_consumers WHERE group_id=$1`, group, 1)
				var event int64
				err = f.db.Platform.QueryRow(f.ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
				VALUES($1,'synthetic-heat-source','group',clock_timestamp(),$2) RETURNING id`, account, []byte("sealed-synthetic")).Scan(&event)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.db.Platform.Exec(f.ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,payload_sealed,codec_version,deployment_version,received_at)
				VALUES($1,$2,'decoded','group','synthetic-conversation',$3,7,1,clock_timestamp())`, event, account, []byte("sealed-synthetic"))
				if err != nil {
					t.Fatal(err)
				}
				if mode == "sampled_out" || mode == "pool_robot" {
					decision := "sampled_out"
					if mode == "pool_robot" {
						decision = "ignored"
					}
					waitHeat(t, f, `SELECT count(*) FROM heat_chat_receipts WHERE group_id=$1 AND decision='`+decision+`'`, group, 1)
					cancel()
					<-done
					var jobs int
					if err = f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM heat_chat_jobs WHERE group_id=$1`, group).Scan(&jobs); err != nil {
						t.Fatal(err)
					}
					if jobs != 0 || calls.Load() != 0 || sends.Load() != 0 {
						t.Fatal("ineligible candidate created work", jobs, calls.Load(), sends.Load())
					}
					return
				}
				status := "sent"
				wantSends := int32(1)
				if mode != "confirmed" {
					status = "unknown"
				}
				if mode == "deployment_changed" {
					status = "failed"
				}
				if mode == "unknown_usage" || mode == "revoked" || mode == "connection_changed" || mode == "deployment_changed" {
					wantSends = 0
				}
				waitHeat(t, f, `SELECT count(*) FROM heat_chat_jobs WHERE group_id=$1 AND status='`+status+`'`, group, 1)
				if calls.Load() != 1 || sends.Load() != wantSends {
					t.Fatal("duplicate model/send", calls.Load(), sends.Load())
				}
				var decision string
				if err = f.db.Platform.QueryRow(f.ctx, `SELECT decision FROM heat_chat_receipts WHERE group_id=$1`, group).Scan(&decision); err != nil || decision != "queued" {
					t.Fatal(decision, err)
				}
				cancel()
				<-done
				if err = service.Recover(f.ctx); err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 1 || sends.Load() != wantSends {
					t.Fatal("restart replayed effects")
				}
				raw := expect(t, owner.client, "GET", "/groups/"+group+"/chat-runtime", nil, 200)
				var view control.HeatRuntimeView
				if json.Unmarshal(raw, &view) != nil || view.State != "running" || view.Effective.ReplyProbability != 0 {
					t.Fatal(string(raw))
				}
			})
		}
	}
}

func heatFixtureGroup(t *testing.T, f *commercialFixture, owner commercialOwner, platform string) (string, string) {
	t.Helper()
	if platform == "wsl" {
		if _, err := f.db.Platform.Exec(f.ctx, `INSERT INTO native_deployment(singleton,sealed,version)
			VALUES(true,$1,1)`, []byte("synthetic-deployment")); err != nil {
			t.Fatal(err)
		}
	}
	var manager, chat, group string
	for _, row := range []struct {
		purpose, workspace string
		out                *string
	}{
		{"management", platform, &manager}, {"chat", "maibot", &chat},
	} {
		if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO accounts(name,platform,purpose,workspace,
			connection_enabled,auth_state,native_uid,native_nickname,credential_sealed)
			VALUES($1,$2,$3,$4,false,'authenticated',$1,$1,$5) RETURNING id::text`,
			row.purpose+"-self", platform, row.purpose, row.workspace, []byte("synthetic-sealed-credential")).Scan(row.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO native_groups(platform,native_id,name)
		VALUES($1,'native-test-group','chat-group') RETURNING id::text`, platform).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(f.ctx, `INSERT INTO personas(account_id,nickname,personality)
		VALUES($1,'synthetic','group scoped persona')`, chat); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(f.ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
		VALUES($1,$2,'member',clock_timestamp(),'native')`, chat, group); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(f.ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,ai_account_id,
		management_enabled,ai_requested,ai_enabled)VALUES($1,$2,$3,$4,false,true,true)`, group, owner.tenant, manager, chat); err != nil {
		t.Fatal(err)
	}
	if err := control.RequireTenantCapability(f.ctx, f.db.Platform, owner.tenant, "groups:ai"); err != nil {
		t.Fatal("synthetic heat owner lacks an explicit tenant ceiling", err)
	}
	if _, err := f.db.Platform.Exec(f.ctx, `UPDATE group_business_runtime SET enabled=true,version=version+1,updated_by=$2
		WHERE group_id=$1`, group, f.adminActor.ID); err != nil {
		t.Fatal(err)
	}
	admin := f.admin
	admin.workspace = platform
	expect(t, admin, "PUT", "/capabilities/gates/send_text/heatbot", map[string]any{
		"state": "pilot", "version": 0, "pilot_group_ids": []string{group},
		"evidence_refs": []string{"acceptance:SYNTHETIC-HEAT-DURABLE"},
	}, 200)
	return group, chat
}

func syntheticHeatRoute() nativewire.TextRoute {
	return nativewire.TextRoute{Digest: make([]byte, 32), DeploymentVersion: 1, ObservedAt: time.Now()}
}

func waitHeat(t *testing.T, f *commercialFixture, query, arg string, want int) {
	t.Helper()
	until := time.Now().Add(12 * time.Second)
	for time.Now().Before(until) {
		var count int
		if err := f.db.Platform.QueryRow(f.ctx, query, arg).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	var states string
	_ = f.db.Platform.QueryRow(f.ctx, `SELECT coalesce(string_agg(status||':'||result_code,','),'none') FROM heat_chat_jobs`).Scan(&states)
	t.Fatal(errors.New("heat chat wait expired"), states)
}
