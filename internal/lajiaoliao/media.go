package lajiaoliao

import (
	"bytes"
	"context"
	"crypto/md5" // OpenIM multipart content identifier, not an authentication hash.
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openimsdk/protocol/sdkws"
)

// MediaAsset is a sealed server upload receipt. OpenIM identities and URLs are
// strings; no WSL asset ID, application nonce or AAD is used here.
type MediaAsset struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	URL          string `json:"url"`
	SnapshotURL  string `json:"snapshot_url,omitempty"`
	SnapshotSize int64  `json:"snapshot_size,omitempty"`
	Size         int64  `json:"size"`
	Duration     int64  `json:"duration"`
	MIME         string `json:"mime,omitempty"`
	Width        int64  `json:"width,omitempty"`
	Height       int64  `json:"height,omitempty"`
}

type MediaUpload struct {
	Kind, Name      string
	Data, Thumbnail []byte
	Duration        int64
	Width, Height   int64
}

func SnapshotDimensions(data []byte) (int64, int64, error) {
	config, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || kind != "jpeg" || len(data) > 1<<20 || config.Width < 1 || config.Height < 1 ||
		config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 4*1024*1024 {
		return 0, 0, &Failure{Code: "lajiaoliao_media_format_unavailable"}
	}
	return int64(config.Width), int64(config.Height), nil
}

func mediaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 8192 && u.Scheme == "https" && u.User == nil &&
		(u.Host == "lj-im-chat-01.lajiaoliao.com" || u.Host == "im-api.lajiaoliao.com" ||
			u.Host == objectStorageHost || u.Host == objectReadHost) &&
		u.Fragment == "" && u.Path != ""
}

func (m MediaAsset) Validate() error {
	valid := ValidID(m.ID) && m.Size > 0 && m.Size <= 16<<20 && mediaURL(m.URL)
	switch m.Kind {
	case "image":
		valid = valid && m.Duration == 0 && m.Size <= 4<<20 && m.SnapshotURL == "" && m.SnapshotSize == 0 &&
			m.Width > 0 && m.Height > 0 && m.Width <= 4096 && m.Height <= 4096 &&
			m.Width*m.Height <= 4*1024*1024 && (m.MIME == "image/jpeg" || m.MIME == "image/png")
	case "voice":
		valid = valid && m.Duration > 0 && m.Duration <= 300 && m.SnapshotURL == "" && m.SnapshotSize == 0 &&
			m.Width == 0 && m.Height == 0 && (m.MIME == "" || m.MIME == "audio/wav" || m.MIME == "audio/mpeg")
	case "video":
		valid = valid && m.Duration > 0 && m.Duration <= 3600 && (m.MIME == "" || m.MIME == "video/mp4") &&
			mediaURL(m.SnapshotURL) && m.SnapshotSize > 0 && m.SnapshotSize <= 1<<20 &&
			m.Width > 0 && m.Height > 0 && m.Width <= 16384 && m.Height <= 16384 && m.Width*m.Height <= 40000000
	default:
		valid = false
	}
	if !valid {
		return &Failure{Code: "lajiaoliao_media_invalid"}
	}
	return nil
}

func (m MediaAsset) content() ([]byte, int32, error) {
	if err := m.Validate(); err != nil {
		return nil, 0, err
	}
	var value any
	kind := int32(103)
	if m.Kind == "image" {
		kind = 102
		picture := map[string]any{"uuid": m.ID, "type": m.MIME, "size": m.Size,
			"width": m.Width, "height": m.Height, "url": m.URL}
		value = map[string]any{"sourcePath": "", "sourcePicture": picture,
			"bigPicture": picture, "snapshotPicture": picture}
	} else if m.Kind == "voice" {
		mime := m.MIME
		if mime == "" {
			mime = "audio/wav"
		}
		value = map[string]any{"uuid": m.ID, "soundPath": "", "sourceUrl": m.URL,
			"soundType": mime, "dataSize": m.Size, "duration": m.Duration}
	} else {
		kind = 104
		value = map[string]any{"videoPath": "", "videoUUID": m.ID, "videoUrl": m.URL,
			"videoType": "video/mp4", "videoSize": m.Size, "duration": m.Duration,
			"snapshotPath": "", "snapshotUUID": m.ID + "-cover", "snapshotUrl": m.SnapshotURL, "snapshotSize": m.SnapshotSize,
			"snapshotWidth": m.Width, "snapshotHeight": m.Height, "snapshotType": "image/jpeg"}
	}
	raw, err := json.Marshal(value)
	return raw, kind, err
}

func (m MediaAsset) Content() ([]byte, error) {
	raw, _, err := m.content()
	return raw, err
}

type MediaEvidenceQuery struct {
	Account, Sender, Group, Peer, ClientID string
	Epoch, CurrentEpoch                    int64
	ContentType                            int32
	Digest                                 []byte
	SubmittedAt                            time.Time
}

type MediaEvidence struct {
	EventID          int64
	Source           string
	Receipt          Receipt
	Found, Truncated bool
	Check            *EvidenceCheck
}

// EvidenceCheck describes a bounded read, never message content or authority.
type EvidenceCheck struct {
	State          string    `json:"state"`
	DurableRecords int       `json:"durable_records"`
	SequenceHeads  int       `json:"sequence_heads"`
	Conversations  int       `json:"conversations"`
	Requested      int       `json:"requested"`
	Returned       int       `json:"returned"`
	SampledAt      time.Time `json:"sampled_at"`
}

func (c EvidenceCheck) Valid() bool {
	switch c.State {
	case "conversation_not_returned", "history_empty", "message_not_found", "scan_truncated", "matched":
	default:
		return false
	}
	return c.DurableRecords >= 0 && c.DurableRecords <= 200 &&
		c.SequenceHeads >= 0 && c.SequenceHeads <= 10000 &&
		c.Conversations >= 0 && c.Conversations <= 2 &&
		c.Requested >= 0 && c.Requested <= 200 &&
		c.Returned >= 0 && c.Returned <= 200 &&
		!c.SampledAt.IsZero()
}

func MediaEvidenceDiagnostic(code string) (stage, reason string, ok bool) {
	for _, stage = range []string{"input", "account", "durable_read", "durable_open", "durable_match",
		"connection", "schedule", "history_guard", "sequences", "conversation", "pull", "history_validate", "final_fence",
		"history_empty", "history_partial", "history_scope", "history_record", "history_sequence"} {
		if reason, ok = strings.CutPrefix(code, "lajiaoliao_evidence_"+stage+"_"); ok {
			switch reason {
			case "owner_changed", "route_changed", "busy", "rate_limited", "timeout", "cancelled",
				"reauth_required", "disconnected", "rejected", "protocol", "gap", "conflict", "unavailable":
				return stage, reason, true
			}
		}
	}
	return "", "", false
}

func (t *Transport) SendMedia(ctx context.Context, group, clientID string, media MediaAsset,
	fence func(context.Context, func(context.Context) error) error) (Receipt, error) {
	if !t.verified.Load() {
		return Receipt{}, &Failure{Code: "lajiaoliao_connection_unverified"}
	}
	if !ValidID(group) || !ValidID(clientID) || fence == nil {
		return Receipt{}, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	content, kind, err := media.content()
	if err != nil {
		return Receipt{}, err
	}
	defer clear(content)
	var result Receipt
	err = t.rpc(ctx, 1003, &sdkws.MsgData{SendID: t.session.UserID, RecvID: group, GroupID: group,
		ClientMsgID: clientID, SenderPlatformID: 5, SessionType: 3, MsgFrom: 100, ContentType: kind,
		Content: content, CreateTime: time.Now().UnixMilli(),
		Options: map[string]bool{"history": true, "persistent": true, "senderSync": true}},
		func(raw []byte) error { var e error; result, e = decodeReceipt(raw); return e }, fence)
	if err != nil {
		return Receipt{}, err
	}
	if result.ClientID != clientID || result.ServerID == "" {
		return Receipt{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	return result, nil
}

// UploadMedia runs one bounded multipart attempt. It neither resumes stored
// parts nor retries an uncertain request. The caller validates the file format.
func (c *Client) UploadMedia(ctx context.Context, s Session, operation string, input MediaUpload,
	guard func(context.Context) error) (MediaAsset, error) {
	out := MediaAsset{ID: operation, Kind: input.Kind, Size: int64(len(input.Data)), Duration: input.Duration,
		Width: input.Width, Height: input.Height, URL: "https://lj-im-chat-01.lajiaoliao.com/checked"}
	out.MIME = http.DetectContentType(input.Data)
	if input.Kind == "voice" {
		if len(input.Data) >= 12 && string(input.Data[:4]) == "RIFF" && string(input.Data[8:12]) == "WAVE" {
			out.MIME = "audio/wav"
		} else {
			out.MIME = "audio/mpeg"
		}
	}
	if input.Kind == "video" {
		out.MIME = "video/mp4"
	}
	if input.Kind == "video" {
		out.SnapshotURL = out.URL
		out.SnapshotSize = int64(len(input.Thumbnail))
	}
	if !ValidID(s.UserID) || s.IMToken == "" || !ValidID(operation) || guard == nil ||
		out.Validate() != nil || input.Kind == "voice" && len(input.Thumbnail) != 0 ||
		input.Kind == "video" && (len(input.Thumbnail) == 0 || len(input.Thumbnail) > 1<<20) {
		return MediaAsset{}, &Failure{Code: "lajiaoliao_media_invalid"}
	}
	var limit struct {
		MinPartSize int64 `json:"minPartSize"`
		MaxPartSize int64 `json:"maxPartSize"`
		MaxNumSize  int32 `json:"maxNumSize"`
	}
	raw, err := c.request(ctx, "/object/part_limit", struct{}{}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return MediaAsset{}, err
	}
	err = json.Unmarshal(raw, &limit)
	clear(raw)
	if err != nil || limit.MinPartSize < 1 || limit.MaxPartSize < limit.MinPartSize ||
		limit.MaxNumSize < 1 || limit.MaxNumSize > 10000 {
		return MediaAsset{}, &Failure{Code: "lajiaoliao_media_contract"}
	}
	// The whole group upload, including its video cover, consumes one attempt.
	if err = guard(ctx); err != nil {
		return MediaAsset{}, err
	}
	upload := func(data []byte, suffix, contentType string) (string, error) {
		size := int64(len(data))
		partSize := max(limit.MinPartSize, (size+int64(limit.MaxNumSize)-1)/int64(limit.MaxNumSize))
		if partSize > limit.MaxPartSize {
			return "", &Failure{Code: "lajiaoliao_media_format_unavailable"}
		}
		count := (size + partSize - 1) / partSize
		// 16 MiB must never turn into an unbounded number of HTTP requests.
		if count < 1 || count > 32 {
			return "", &Failure{Code: "lajiaoliao_media_contract"}
		}
		parts := make([]string, 0, count)
		for start := int64(0); start < size; start += partSize {
			sum := md5.Sum(data[start:min(start+partSize, size)])
			parts = append(parts, hex.EncodeToString(sum[:]))
		}
		sum := md5.Sum([]byte(strings.Join(parts, ",")))
		name := s.UserID + "/msg_" + suffix + "_" + operation + map[string]string{
			"audio/wav": ".wav", "audio/mpeg": ".mp3", "video/mp4": ".mp4",
			"image/jpeg": ".jpg", "image/png": ".png"}[contentType]
		cause := map[string]string{"image": "msg-picture", "voice": "msg-voice", "video": "msg-video", "videoSnapshot": "msg-video-snapshot"}[suffix]
		raw, err := c.request(ctx, "/object/initiate_multipart_upload", map[string]any{
			"hash": hex.EncodeToString(sum[:]), "size": size, "partSize": partSize,
			"maxParts": count, "cause": cause, "name": name, "contentType": contentType},
			s.IMToken, "5", s.DeviceID)
		if err != nil {
			return "", err
		}
		defer clear(raw)
		var response struct {
			URL    string `json:"url"`
			Upload *struct {
				ID       string     `json:"uploadID"`
				PartSize int64      `json:"partSize"`
				Sign     uploadSign `json:"sign"`
			} `json:"upload"`
		}
		if json.Unmarshal(raw, &response) != nil {
			return "", &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
		}
		if response.Upload == nil {
			if !mediaURL(response.URL) {
				return "", uploadOriginFailure("media-resource", response.URL)
			}
			return response.URL, nil
		}
		u := response.Upload
		if u.ID == "" || len(u.ID) > 4096 || u.PartSize != partSize || len(u.Sign.Parts) != len(parts) {
			return "", &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
		}
		for i := range parts {
			part := u.Sign.Parts[i]
			if part.Number != i+1 {
				return "", &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
			}
			target, headers, err := u.Sign.request(part)
			if err != nil {
				return "", err
			}
			start := int64(i) * partSize
			request, err := http.NewRequestWithContext(ctx, http.MethodPut, target,
				bytes.NewReader(data[start:min(start+partSize, size)]))
			if err != nil {
				return "", &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
			}
			request.Header, request.GetBody = headers, nil
			// The signed URL receives only its bounded signature headers.
			resp, err := c.http.Do(request)
			if err != nil {
				return "", &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return "", &Failure{Code: "lajiaoliao_media_upload_rejected", Submitted: true}
			}
		}
		raw, err = c.request(ctx, "/object/complete_multipart_upload", map[string]any{
			"uploadID": u.ID, "parts": parts, "name": name, "contentType": contentType, "cause": cause},
			s.IMToken, "5", s.DeviceID)
		if err != nil {
			return "", err
		}
		defer clear(raw)
		var completed struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(raw, &completed) != nil {
			return "", &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
		}
		if !mediaURL(completed.URL) {
			return "", uploadOriginFailure("media-resource", completed.URL)
		}
		return completed.URL, nil
	}
	if input.Kind == "video" {
		out.SnapshotURL, err = upload(input.Thumbnail, "videoSnapshot", "image/jpeg")
		if err != nil {
			return MediaAsset{}, err
		}
	}
	out.URL, err = upload(input.Data, input.Kind, out.MIME)
	if err != nil {
		return MediaAsset{}, err
	}
	return out, out.Validate()
}

type uploadValues struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}
type uploadPart struct {
	Number        int    `json:"partNumber"`
	URL           string `json:"url"`
	Query, Header []uploadValues
}
type uploadSign struct {
	URL           string `json:"url"`
	Query, Header []uploadValues
	Parts         []uploadPart `json:"parts"`
}

func (s uploadSign) request(part uploadPart) (string, http.Header, error) {
	target := s.URL
	if part.URL != "" {
		target = part.URL
	}
	if !mediaURL(target) {
		return "", nil, uploadOriginFailure("media-signed", target)
	}
	if len(s.Query)+len(part.Query)+len(s.Header)+len(part.Header) > 64 {
		return "", nil, &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
	}
	u, _ := url.Parse(target)
	query := u.Query()
	for _, set := range [][]uploadValues{s.Query, part.Query} {
		for _, field := range set {
			if len(field.Key) > 256 || len(field.Values) > 8 {
				return "", nil, &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
			}
			query[field.Key] = field.Values
		}
	}
	u.RawQuery = query.Encode()
	headers := http.Header{}
	for _, set := range [][]uploadValues{s.Header, part.Header} {
		for _, field := range set {
			key := http.CanonicalHeaderKey(field.Key)
			if key != "Content-Type" && key != "Content-Md5" && !strings.HasPrefix(key, "X-Amz-") &&
				!strings.HasPrefix(key, "X-Oss-") {
				return "", nil, &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
			}
			if len(field.Values) != 1 || len(field.Values[0]) > 8192 || strings.ContainsAny(field.Values[0], "\r\n") {
				return "", nil, &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
			}
			headers[key] = field.Values
		}
	}
	if !mediaURL(u.String()) || len(u.RawQuery) > 8192 {
		return "", nil, &Failure{Code: "lajiaoliao_media_contract", Submitted: true}
	}
	return u.String(), headers, nil
}
