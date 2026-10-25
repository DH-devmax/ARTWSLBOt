package nativeauth

import (
	"context"
	"encoding/json"
)

// Resume creates no login, signup or verification request. A dynamic IM token
// may be renewed through the saved authenticated business session only.
func Resume(d Deployment, saved Session, ids RequestIDSource) (*Client, error) {
	if saved.Origin != d.Origin || saved.ProtocolVersion != d.ProtocolVersion ||
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
	result, err := c.request(ctx, "/v1/user/get-nim-token", map[string]any{
		"nimDeviceId": c.config.DeviceID, "uid": c.fields[3], "loginToken": c.jwt}, nil)
	if err != nil {
		return Session{}, err
	}
	var token, kind string
	if json.Unmarshal(result["nimToken"], &token) != nil || !validToken(token) {
		return Session{}, rejection("native_nim_token_invalid")
	}
	mode := 0
	if value, ok := result["kind"]; ok {
		if json.Unmarshal(value, &kind) != nil {
			return Session{}, rejection("native_nim_token_invalid")
		}
		var valid bool
		mode, valid = map[string]int{"NIM_LOGIN_KIND_STATIC": 0, "NIM_LOGIN_KIND_DYNAMIC": 1, "NIM_LOGIN_KIND_CALLBACK": 2}[kind]
		if !valid {
			return Session{}, rejection("native_nim_token_invalid")
		}
	}
	saved.NIMToken, saved.NIMAuthType, saved.JWT = token, mode, c.jwt
	saved.Kind = c.fields[4]
	return saved, nil
}
