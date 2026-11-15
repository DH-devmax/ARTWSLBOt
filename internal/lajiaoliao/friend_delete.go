package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

const friendDeleteRoute = "/friend/delete_friend"

func init() {
	imRoutes[friendDeleteRoute] = true
}

// Group is the caller's controlled scope, not part of DeleteFriendReq.
func (c *Client) deleteFriend(ctx context.Context, s Session, change Change,
	fence func(context.Context, func(context.Context) error) error) error {
	if !ValidID(s.UserID) || change.Target == s.UserID {
		return &Failure{Code: "lajiaoliao_invalid_identity"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var once sync.Once
	var attempted bool
	var result error
	err := fence(ctx, func(work context.Context) error {
		once.Do(func() {
			if result = work.Err(); result != nil {
				return
			}
			attempted = true
			var raw json.RawMessage
			raw, result = c.request(work, friendDeleteRoute,
				map[string]any{"ownerUserID": s.UserID, "friendUserID": change.Target}, s.IMToken, "5", s.DeviceID)
			if result == nil && !friendDeleteEmptyResponse(raw) {
				result = &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
			}
		})
		return result
	})
	if err == nil {
		err = result
	}
	if err != nil && attempted {
		var failure *Failure
		if errors.As(err, &failure) {
			copy := *failure
			copy.Submitted = true
			return &copy
		}
		return &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	if !attempted && err == nil {
		return &Failure{Code: "lajiaoliao_fence_required"}
	}
	return err
}

func friendDeleteEmptyResponse(raw json.RawMessage) bool {
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return false
	}
	if len(data) == 0 {
		return true
	}
	// request preserves the success envelope when proto3 omits empty data.
	var envelope struct {
		Code *int            `json:"errCode"`
		Data json.RawMessage `json:"data"`
	}
	return json.Unmarshal(raw, &envelope) == nil && envelope.Code != nil && *envelope.Code == 0 &&
		(len(envelope.Data) == 0 || string(envelope.Data) == "null")
}
