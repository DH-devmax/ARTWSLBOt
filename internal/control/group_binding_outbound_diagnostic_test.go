package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

func TestGroupBindingOutboundSubmitDiagnosticIsBoundedAndNonAuthoritative(t *testing.T) {
	observed := time.Now()
	for _, tc := range []struct {
		code     string
		number   int
		submit   bool
		attempt  bool
		expected int
	}{
		{"native_business_rejected", 1001, true, true, 1001},
		{"native_rate_limited", 429, true, true, 429},
		{"native_transport_unknown", 502, true, true, 502},
		{"native_response_invalid", 500, true, true, 500},
		{"native_reauth_required", 401, true, true, 401},
		{"native_http_rejected", 403, true, true, 403},
		{"native_business_rejected", 1001, true, false, 0},
		{"native_business_rejected", 1001, false, true, 0},
		{"synthetic-private-body", 1001, true, true, 0},
		{"native_business_rejected", -1, true, true, 0},
		{"native_business_rejected", 10000, true, true, 0},
	} {
		cause := fmt.Errorf("synthetic-private-token: %w", &nativeauth.Error{
			Code: tc.code, Number: tc.number, Submitted: tc.submit, Diagnostic: "synthetic-private-body",
		})
		if got := outboundSubmitProviderCode(cause, tc.attempt); got != tc.expected {
			t.Fatalf("provider number escaped the submission boundary: %d", got)
		}
		record := bindingOutboundRecord{Platform: "wsl", Code: tc.code, Used: 1,
			ProviderCode: &tc.number, ProviderCategory: outboundSubmitCategory(tc.code), ProviderObservedAt: &observed}
		diagnostic := outboundSubmitDiagnostic(record)
		valid := outboundSubmitCategory(tc.code) != "" && tc.number >= 1 && tc.number <= 9999
		if valid != (diagnostic != nil) {
			t.Fatal("stored diagnosis accepted an unknown code or invalid numeric value")
		}
		encoded, err := json.Marshal(diagnostic)
		if err != nil || strings.Contains(string(encoded), "synthetic-private") {
			t.Fatal("provider text entered the public diagnosis")
		}
	}
	for _, cause := range []error{nil, errors.New("synthetic-private-body"), Denied(409, "synthetic-private-code")} {
		if outboundSubmitProviderCode(cause, true) != 0 {
			t.Fatal("untyped error became provider evidence")
		}
	}
	number := 1001
	base := bindingOutboundRecord{Platform: "wsl", Code: "native_business_rejected", Used: 1,
		ProviderCode: &number, ProviderCategory: "upstream_rejected", ProviderObservedAt: &observed}
	for _, change := range []func(*bindingOutboundRecord){
		func(r *bindingOutboundRecord) { r.Platform = "lajiaoliao" },
		func(r *bindingOutboundRecord) { r.Platform = "" },
		func(r *bindingOutboundRecord) { r.Used = 0 },
		func(r *bindingOutboundRecord) { r.ProviderCode = nil },
		func(r *bindingOutboundRecord) { r.ProviderObservedAt = nil },
		func(r *bindingOutboundRecord) { r.ProviderObservedAt = &time.Time{} },
		func(r *bindingOutboundRecord) { r.ProviderCategory = "" },
		func(r *bindingOutboundRecord) { r.ProviderCategory = "synthetic-private-body" },
	} {
		record := base
		change(&record)
		if outboundSubmitDiagnostic(record) != nil {
			t.Fatal("missing, foreign or successful record manufactured a failure diagnosis")
		}
	}
	for _, code := range []string{"native_friend_request_observed", "native_friend_request_rejected",
		"native_friend_relationship_observed"} {
		record := base
		record.Code = code
		if diagnostic := outboundSubmitDiagnostic(record); diagnostic == nil || diagnostic.ProviderCode != number ||
			diagnostic.Category != "upstream_rejected" {
			t.Fatal("read-only reconciliation lost the immutable submission diagnosis")
		}
	}
}
