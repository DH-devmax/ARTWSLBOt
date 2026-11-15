package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
)

type GatewayTestInput struct {
	RateID  string `json:"rate_id"`
	Key     string `json:"idempotency_key"`
	MaxCost int64  `json:"max_cost_micro"`
}

type GatewayTestResult struct {
	ID       string           `json:"id"`
	Status   string           `json:"status"`
	Reserved int64            `json:"reserved_micro"`
	Charged  int64            `json:"charged_micro"`
	Usage    *assistant.Usage `json:"usage"`
	Result   string           `json:"result"`
}

func (s *GatewayService) Test(ctx context.Context, ref SessionRef, in GatewayTestInput) (GatewayTestResult, error) {
	var out GatewayTestResult
	if !ValidID(in.RateID) || !ValidID(in.Key) || in.MaxCost <= 0 || in.MaxCost > 100_000_000 {
		return out, Denied(400, "invalid_gateway_test")
	}
	check := func(ctx context.Context) error {
		a, err := sessionActor(ctx, s.DB.Platform, ref, false)
		if err != nil {
			return err
		}
		if a.Role != "superadmin" || a.TenantID != "" {
			return Denied(403, "superadmin_required")
		}
		return nil
	}
	if err := check(ctx); err != nil {
		return out, err
	}
	a, err := sessionActor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return out, err
	}
	key := "gateway-test:" + a.ID + ":" + in.Key
	var recordedRate string
	read := func(ctx context.Context) error {
		return s.DB.Platform.QueryRow(ctx, `SELECT id::text,status,reserved_micro,charged_micro,usage,rate_id::text
			FROM ai_calls WHERE external_key=$1 AND actor_id=$2 AND purpose='test'`, key, a.ID).
			Scan(&out.ID, &out.Status, &out.Reserved, &out.Charged, &out.Usage, &recordedRate)
	}
	if err = read(ctx); err == nil {
		if recordedRate != in.RateID || out.Reserved > in.MaxCost {
			return out, Denied(409, "idempotency_conflict")
		}
		out.Result = out.Status
		return out, check(ctx)
	} else if err != pgx.ErrNoRows {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	_, runErr := s.generate(ctx, gatewayRequest{Actor: a, Session: ref, Purpose: "test", Key: key,
		ExpectedRateID: in.RateID, MaxCost: in.MaxCost, CallID: &out.ID, Check: check},
		[]assistant.Message{{Role: "user", Content: "Reply with OK only."}}, nil, func(string) error { return nil })
	if out.ID == "" {
		if runErr != nil {
			return out, runErr
		}
		return out, Denied(409, "ai_call_already_recorded")
	}
	work, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	if err = check(work); err != nil {
		return GatewayTestResult{}, err
	}
	if err = read(work); err != nil {
		return out, err
	}
	out.Result = out.Status
	if runErr != nil {
		out.Result = "generation_failed"
	}
	// No provider error body or generated text is projected.
	return out, nil
}
