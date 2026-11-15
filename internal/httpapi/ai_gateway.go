package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
)

func (s *Server) gatewayRoutes() {
	s.handle("GET /api/v1/ai-gateway", true, s.gatewaySettings)
	s.handle("PUT /api/v1/ai-gateway", true, s.saveGatewaySettings)
	s.handle("POST /api/v1/ai-gateway/inspect", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			RateID string `json:"rate_id"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := s.Gateway.Inspect(r.Context(), assistantRef(r), in.RateID)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/ai-gateway/test", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GatewayTestInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := s.Gateway.Test(r.Context(), assistantRef(r), in)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/ai-gateway/calls/{id}/reconcile", true, s.reconcileGatewayCall)
	s.handle("POST /api/v1/ai-gateway/calls/{id}/adjust", true, s.adjustGatewayCall)
	s.mux.HandleFunc("POST /internal/v1/ai/generate", func(w http.ResponseWriter, r *http.Request) {
		// Only this private, bounded generation route can outlive the normal API timeout.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(100 * time.Second))
		ctx, stop := context.WithTimeout(r.Context(), 90*time.Second)
		defer stop()
		r = r.WithContext(ctx)
		var in control.HeatGatewayInput
		if err := decode(w, r, &in); err != nil {
			sendError(w, err)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		result, err := s.Gateway.HeatGenerate(ctx, token, in)
		if err != nil {
			sendError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": result.Message.Content, "usage": result.Usage})
	})
}

func (s *Server) adjustGatewayCall(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in control.CreditCorrection
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Gateway.CorrectCharge(r.Context(), principalActor(p), r.PathValue("id"), in); err != nil {
		return err
	}
	send(w, 200, map[string]bool{"recorded": true})
	return nil
}
func (s *Server) gatewaySettings(w http.ResponseWriter, r *http.Request, p Principal) error {
	var settings control.GatewaySettings
	if err := s.DB.Platform.QueryRow(r.Context(), `SELECT enabled,platform_limit_micro,test_limit_micro,version FROM ai_gateway_settings`).Scan(
		&settings.Enabled, &settings.PlatformLimit, &settings.TestLimit, &settings.Version); err != nil {
		return err
	}
	var data []byte
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT jsonb_build_object(
        'routes',(SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
            SELECT r.alias,r.version,r.enabled,m.id rate_id,m.version rate_version,m.verified,
            jsonb_build_object('input',m.input_rate,'output',m.output_rate,'cache_read',m.cache_read_rate,'cache_write',m.cache_write_rate) rates
            FROM ai_model_routes r JOIN ai_model_rates m ON m.id=r.rate_id) x),
        'calls',(SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
            SELECT id,tenant_id,actor_id,group_id,purpose,execution_mode,billing_source,rate_id,
                reserved_micro,charged_micro,effective_charged_micro,status,created_at,finished_at
            FROM ai_call_costs ORDER BY created_at DESC LIMIT 100) x),
        'upstream',(SELECT snapshot FROM ai_upstream_snapshots WHERE rate_id=(SELECT rate_id FROM ai_model_routes WHERE alias='default')),
        'native_ready',false,'ai_ready',false)`).Scan(&data)
	if err != nil {
		return err
	}
	// Only the superadmin projection exposes non-secret provider coordinates.
	var provider map[string]any
	var id string
	var sealed []byte
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT m.id::text,m.provider_sealed FROM ai_model_routes route
        JOIN ai_model_rates m ON m.id=route.rate_id WHERE route.alias='default'`).Scan(&id, &sealed)
	if err == nil {
		raw, e := s.Vault.Open("ai-gateway:"+id, sealed)
		if e != nil {
			return e
		}
		var model control.GatewayProviderConfig
		e = json.Unmarshal(raw, &model)
		clear(raw)
		if e != nil {
			return fail(503, "model_unavailable")
		}
		provider = map[string]any{"base_url": model.BaseURL, "model": model.Model, "has_key": model.APIKey != "",
			"usd_usdt_parity": model.USDUSDTParity}
	} else if err != pgx.ErrNoRows {
		return err
	}
	usage, err := s.Gateway.Usage(r.Context(), assistantRef(r))
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"settings": settings, "runtime": json.RawMessage(data), "provider": provider, "usage": usage})
	return nil
}
func (s *Server) saveGatewaySettings(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Settings control.GatewaySettings `json:"settings"`
		Route    control.ModelRoute      `json:"route"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	// Empty key means retain the current independently configured business key.
	if in.Route.Model.APIKey == "" {
		var id string
		var sealed []byte
		err := s.DB.Platform.QueryRow(r.Context(), `SELECT m.id::text,m.provider_sealed FROM ai_model_routes r
            JOIN ai_model_rates m ON m.id=r.rate_id WHERE r.alias=$1`, in.Route.Alias).Scan(&id, &sealed)
		if err == nil {
			raw, e := s.Vault.Open("ai-gateway:"+id, sealed)
			if e != nil {
				return e
			}
			var old assistant.ModelConfig
			e = json.Unmarshal(raw, &old)
			clear(raw)
			if e != nil {
				return fail(503, "model_unavailable")
			}
			if in.Route.Model.BaseURL != "" && strings.TrimRight(in.Route.Model.BaseURL, "/") != strings.TrimRight(old.BaseURL, "/") &&
				strings.TrimRight(in.Route.Model.BaseURL, "/")+"/v1" != strings.TrimRight(old.BaseURL, "/") {
				return fail(409, "new_origin_requires_business_key")
			}
			in.Route.Model.APIKey = old.APIKey
			if in.Route.Model.BaseURL == "" {
				in.Route.Model.BaseURL = old.BaseURL
			}
			if in.Route.Model.Model == "" {
				in.Route.Model.Model = old.Model
			}
		}
	}
	if err := s.Gateway.Configure(r.Context(), principalActor(p), in.Settings, in.Route); err != nil {
		return err
	}
	return s.gatewaySettings(w, r, p)
}
func (s *Server) reconcileGatewayCall(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Usage  assistant.Usage `json:"usage"`
		Reason string          `json:"reason"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Gateway.Reconcile(r.Context(), principalActor(p), r.PathValue("id"), in.Usage, in.Reason); err != nil {
		return err
	}
	send(w, 200, map[string]bool{"reconciled": true})
	return nil
}
