package lajiaoliao

import (
	"context"
	"encoding/json"
	"time"

	"wangshangliao/internal/nativeauth"
)

// Pinned OpenIM RevokeMsgReq: conversationID, seq, userID. Never WSL IDs.
func (c *Client) Recall(ctx context.Context, session Session, ref nativeauth.MessageReference,
	fence func(context.Context, func(context.Context) error) error) error {
	raw, _ := json.Marshal(ref)
	if fence == nil || !ValidID(session.UserID) || !ValidID(ref.Group) ||
		!nativeauth.ValidMessageReference(string(raw), "lajiaoliao", ref.Group) || !ref.Fresh(time.Now()) {
		return &Failure{Code: "lajiaoliao_invalid_action"}
	}
	return fence(ctx, func(work context.Context) error {
		_, err := c.request(work, "/msg/revoke_msg", map[string]any{"conversationID": ref.Conversation,
			"seq": ref.Sequence, "userID": session.UserID}, session.IMToken, "5", session.DeviceID)
		return err
	})
}
