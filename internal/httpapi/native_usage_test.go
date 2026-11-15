package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"wangshangliao/internal/features"
)

func TestCatalogExposesConsumerBoundaryWithoutRemovingManualContracts(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		request := httptest.NewRequest("GET", "/api/v1/features/catalog", nil)
		request.Header.Set("X-Workspace", platform)
		response := httptest.NewRecorder()
		if err := (&Server{}).featureCatalog(response, request, Principal{Role: "superadmin"}); err != nil {
			t.Fatal(err)
		}
		var body struct {
			Data struct {
				Usage   map[string]string                        `json:"native_usage"`
				Native  []features.NativeCapability              `json:"native_capabilities"`
				Modules []features.Definition                    `json:"items"`
				Ready   bool                                     `json:"native_ready"`
				Samples map[string]features.ControlledAcceptance `json:"native_demonstrations"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Ready || len(body.Data.Native) != len(features.NativeCatalogForPlatform(platform)) {
			t.Fatal("consumer classification altered support or readiness")
		}
		if body.Data.Usage["signup_sms"] != features.NativeManual ||
			body.Data.Usage["accept_friend"] != features.NativeManual ||
			body.Data.Usage["read_voice"] != features.NativeManual ||
			body.Data.Usage["mute_member"] != features.NativeFoundation {
			t.Fatal("manual/foundation boundary missing from HTTP catalog")
		}
		for _, item := range body.Data.Modules {
			if item.ID == "friend_approval" {
				t.Fatal("automatic approval exposed as a business module")
			}
		}
		if platform == "wsl" && len(body.Data.Samples) != 9 ||
			platform == "lajiaoliao" && len(body.Data.Samples) != 4 {
			t.Fatal("mobile evidence platform boundary missing from HTTP catalog")
		}
	}
}
