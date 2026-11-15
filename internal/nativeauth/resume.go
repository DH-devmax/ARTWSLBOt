package nativeauth

import (
	"context"
	"encoding/json"
)

// Resume creates no login, signup or verification request. An ephemeral IM token
// may be renewed through the saved authenticated business session only.
func Resume(d Deployment, saved Session, ids RequestIDSource) (*Client, error) {
	if err := validateDeploymentMode(d, false); err != nil {
		return nil, err
	}
	if saved.Origin != d.Origin || saved.ProtocolVersion != d.ProtocolVersion ||
		desktopClient(saved.DesktopPlatform) != desktopClient(d.DesktopPlatform) ||
		!supportedDesktop(saved.DesktopPlatform) ||
		saved.UID == 0 || saved.UID >= 1<<31 || !validToken(saved.JWT) ||
		!validToken(saved.NIMToken) || saved.NIMID == "" || len(saved.NIMID) > 128 ||
		saved.NIMAuthType < 0 || saved.NIMAuthType > 2 {
		return nil, rejection("native_reauth_required")
	}
	base, err := NewClient(d)
	if err != nil {
		return nil, err
	}
	c := base.(*Client)
	if c.fields[0] != saved.Device || len(saved.MAC) != 16 {
		c.Close()
		return nil, rejection("native_reauth_required")
	}
	c.fields[3], c.fields[4] = saved.UID, saved.Kind
	c.jwt, c.mac, c.requestIDs = saved.JWT, append([]byte(nil), saved.MAC...), ids
	return c, nil
}

func (c *Client) RenewIM(ctx context.Context, saved Session) (Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	params, err := desktopRenewalParams(c.config)
	if err != nil {
		return Session{}, err
	}
	if c.mobile || saved.NIMAuthType != 2 {
		return Session{}, rejection("native_nim_token_invalid")
	}
	result, err := c.request(ctx, "/v1/user/get-nim-token", params, nil)
	if err != nil {
		return Session{}, err
	}
	token, err := decodeRenewedCallback(result)
	if err != nil {
		return Session{}, err
	}
	saved.NIMToken, saved.NIMAuthType, saved.JWT = token, 2, c.jwt
	saved.Kind = c.fields[4]
	return saved, nil
}

func decodeIMToken(result map[string]json.RawMessage) (string, int, error) {
	var token string
	if json.Unmarshal(result["nimToken"], &token) != nil || !validToken(token) {
		return "", 0, rejection("native_nim_token_invalid")
	}
	kind := "NIM_LOGIN_KIND_STATIC"
	if raw, ok := result["kind"]; ok {
		kind = ""
		if json.Unmarshal(raw, &kind) != nil {
			return "", 0, rejection("native_nim_token_invalid")
		}
	}
	// DYNC is the official protobuf spelling; DYNAMIC is kept for the
	// previously reviewed adapter/fixture contract.
	mode, ok := map[string]int{
		"NIM_LOGIN_KIND_STATIC": 0, "NIM_LOGIN_KIND_DYNC": 1,
		"NIM_LOGIN_KIND_DYNAMIC": 1, "NIM_LOGIN_KIND_CALLBACK": 2,
	}[kind]
	if !ok {
		return "", 0, rejection("native_nim_token_invalid")
	}
	return token, mode, nil
}
