package nativeauth

import (
	"context"
	"encoding/json"
)

type DeploymentCheck struct {
	Verified        bool `json:"verified"`
	CaptchaRequired bool `json:"captcha_required"`
}

// CheckDeployment reads one fixed anonymous settings endpoint. It never logs in,
// sends SMS, registers an identity or retries an ambiguous upstream request.
func CheckDeployment(ctx context.Context, d Deployment, ids RequestIDSource,
	factory func(Deployment) (AuthClient, error)) (DeploymentCheck, error) {
	if !supportedProtocol(d.ProtocolVersion) || !supportedDesktop(d.DesktopPlatform) {
		return DeploymentCheck{}, rejection("native_desktop_required")
	}
	if factory == nil {
		factory = NewClient
	}
	client, err := factory(d)
	if err != nil {
		return DeploymentCheck{}, err
	}
	defer client.Close()
	if concrete, ok := client.(*Client); ok {
		concrete.requestIDs = ids
	}
	data, err := client.Request(ctx, "/v1/settings/get-sys-cfg", map[string]any{})
	if err != nil {
		return DeploymentCheck{}, err
	}
	raw := data["enableYidunVerify"]
	var required bool
	if string(raw) == "null" || json.Unmarshal(raw, &required) != nil {
		return DeploymentCheck{}, rejection("native_response_invalid")
	}
	return DeploymentCheck{Verified: true, CaptchaRequired: required}, nil
}
