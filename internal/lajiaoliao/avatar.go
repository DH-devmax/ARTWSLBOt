package lajiaoliao

import (
	"bytes"
	"context"
	"crypto/md5" // OpenIM multipart content identifier.
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func avatarURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.User == nil &&
		(u.Host == "lj-im-chat-01.lajiaoliao.com" || u.Host == "im-api.lajiaoliao.com" ||
			u.Host == objectStorageHost || u.Host == objectReadHost) &&
		u.Fragment == "" && u.Path != ""
}

type avatarValues struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}
type avatarPart struct {
	Number        int `json:"partNumber"`
	URL           string
	Query, Header []avatarValues
}
type avatarSign struct {
	URL           string
	Query, Header []avatarValues
	Parts         []avatarPart
}

func (s avatarSign) request() (string, http.Header, error) {
	if len(s.Parts) != 1 || s.Parts[0].Number != 1 ||
		len(s.Query)+len(s.Header)+len(s.Parts[0].Query)+len(s.Parts[0].Header) > 64 {
		return "", nil, &Failure{Code: "lajiaoliao_avatar_contract", Submitted: true}
	}
	part := s.Parts[0]
	target := s.URL
	if part.URL != "" {
		target = part.URL
	}
	if !avatarURL(target) {
		return "", nil, uploadOriginFailure("avatar-signed", target)
	}
	u, _ := url.Parse(target)
	query, headers := u.Query(), http.Header{}
	for _, fields := range [][]avatarValues{s.Query, part.Query} {
		for _, field := range fields {
			if len(field.Key) > 256 || len(field.Values) > 8 {
				return "", nil, &Failure{Code: "lajiaoliao_avatar_contract", Submitted: true}
			}
			query[field.Key] = field.Values
		}
	}
	for _, fields := range [][]avatarValues{s.Header, part.Header} {
		for _, field := range fields {
			key := http.CanonicalHeaderKey(field.Key)
			if key != "Content-Type" && key != "Content-Md5" && !strings.HasPrefix(key, "X-Amz-") &&
				!strings.HasPrefix(key, "X-Oss-") || len(field.Values) != 1 ||
				len(field.Values[0]) > 8192 || strings.ContainsAny(field.Values[0], "\r\n") {
				return "", nil, &Failure{Code: "lajiaoliao_avatar_contract", Submitted: true}
			}
			headers[key] = field.Values
		}
	}
	u.RawQuery = query.Encode()
	if !avatarURL(u.String()) {
		return "", nil, &Failure{Code: "lajiaoliao_avatar_contract", Submitted: true}
	}
	return u.String(), headers, nil
}

// ChangeAvatar accepts a normalized JPEG, uploads once, then fences the account
// update separately. Uncertain uploads are never resumed or retried.
func (c *Client) ChangeAvatar(ctx context.Context, s Session, operation string, data []byte,
	fence func(context.Context, func(context.Context) error) error, uploaded func(string)) error {
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || kind != "jpeg" || cfg.Width != 256 || cfg.Height != 256 || len(data) > 1<<20 ||
		!ValidID(s.UserID) || !ValidID(operation) || s.IMToken == "" || s.ChatToken == "" || fence == nil || uploaded == nil {
		return &Failure{Code: "lajiaoliao_avatar_invalid"}
	}
	var limit struct {
		Min int64 `json:"minPartSize"`
		Max int64 `json:"maxPartSize"`
		Num int   `json:"maxNumSize"`
	}
	raw, err := c.request(ctx, "/object/part_limit", struct{}{}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return err
	}
	err = json.Unmarshal(raw, &limit)
	clear(raw)
	if err != nil || limit.Min < int64(len(data)) || limit.Max < limit.Min || limit.Num < 1 {
		return &Failure{Code: "lajiaoliao_avatar_contract"}
	}
	sum := md5.Sum(data)
	parts := []string{hex.EncodeToString(sum[:])}
	hash := md5.Sum([]byte(parts[0]))
	name := s.UserID + "/avatar_" + operation + ".jpg"
	var response struct {
		URL    string `json:"url"`
		Upload *struct {
			ID   string     `json:"uploadID"`
			Size int64      `json:"partSize"`
			Sign avatarSign `json:"sign"`
		} `json:"upload"`
	}
	err = fence(ctx, func(work context.Context) error {
		raw, e := c.request(work, "/object/initiate_multipart_upload", map[string]any{
			"hash": hex.EncodeToString(hash[:]), "size": len(data), "partSize": limit.Min,
			"maxParts": 1, "name": name, "contentType": "image/jpeg", "cause": ""},
			s.IMToken, "5", s.DeviceID)
		if e != nil {
			return e
		}
		defer clear(raw)
		if json.Unmarshal(raw, &response) != nil {
			return &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if u := response.Upload; u != nil {
		if u.ID == "" || len(u.ID) > 4096 || u.Size != limit.Min {
			return &Failure{Code: "lajiaoliao_avatar_contract", Submitted: true}
		}
		target, headers, err := u.Sign.request()
		if err != nil {
			return err
		}
		err = fence(ctx, func(work context.Context) error {
			req, e := http.NewRequestWithContext(work, http.MethodPut, target, bytes.NewReader(data))
			if e != nil {
				return &Failure{Code: "lajiaoliao_avatar_contract", Submitted: true}
			}
			req.Header, req.GetBody = headers, nil
			resp, e := c.http.Do(req)
			if e != nil {
				return &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return &Failure{Code: "lajiaoliao_avatar_upload_rejected", Submitted: true}
			}
			raw, e := c.request(work, "/object/complete_multipart_upload", map[string]any{
				"uploadID": u.ID, "parts": parts, "name": name, "contentType": "image/jpeg", "cause": ""},
				s.IMToken, "5", s.DeviceID)
			if e != nil {
				return e
			}
			defer clear(raw)
			var result struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(raw, &result) != nil {
				return &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
			}
			response.URL = result.URL
			return nil
		})
		if err != nil {
			return err
		}
	}
	if !avatarURL(response.URL) {
		return uploadOriginFailure("avatar-resource", response.URL)
	}
	uploaded(response.URL)
	return fence(ctx, func(work context.Context) error {
		raw, err := c.request(work, "/user/update", map[string]any{"userID": s.UserID, "faceURL": response.URL},
			s.ChatToken, "3", s.DeviceID)
		clear(raw)
		return err
	})
}
