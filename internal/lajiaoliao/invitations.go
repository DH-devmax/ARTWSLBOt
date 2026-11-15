package lajiaoliao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Desktop 1.0.3 distinguishes an invitation from an application by joinSource=2
// and a non-self inviter. The applicant-side list also contains self invitations.
type SelfInvitation struct {
	Group, User, Name, Inviter string
	Created                    int64
	Result                     int
}

func (r SelfInvitation) Time() time.Time {
	return time.UnixMilli(r.Created).UTC()
}

func (r SelfInvitation) ID() string {
	raw, _ := json.Marshal([]any{"chili-self-invitation-v1", r.Group, r.User, r.Inviter, r.Created})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validInvitationID(id string) bool {
	if len(id) != 64 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (c *Client) SelfInvitations(ctx context.Context, s Session, group string, page int) ([]SelfInvitation, bool, error) {
	if !ValidID(s.UserID) || !ValidID(group) || page < 1 || page > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_invalid_page"}
	}
	raw, err := c.request(ctx, "/group/get_user_req_group_applicationList", map[string]any{
		"userID": s.UserID, "groupIDs": []string{group},
		"pagination": map[string]int{"pageNumber": page, "showNumber": 100},
	}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, false, err
	}
	var result struct {
		Total    *int `json:"total"`
		Requests *[]struct {
			User struct {
				ID   string `json:"userID"`
				Name string `json:"nickname"`
			} `json:"userInfo"`
			Group struct {
				ID string `json:"groupID"`
			} `json:"groupInfo"`
			Source  int    `json:"joinSource"`
			Inviter string `json:"inviterUserID"`
			Created int64  `json:"reqTime"`
			Result  int    `json:"handleResult"`
		} `json:"groupRequests"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Total == nil || *result.Total < 0 || *result.Total > 10000 {
		return nil, false, &Failure{Code: "lajiaoliao_invitation_shape"}
	}
	if result.Requests == nil && *result.Total == 0 {
		return []SelfInvitation{}, false, nil
	}
	if result.Requests == nil || len(*result.Requests) > 100 ||
		len(*result.Requests) != min(100, max(0, *result.Total-(page-1)*100)) {
		return nil, false, &Failure{Code: "lajiaoliao_invitation_shape"}
	}
	out, seen := []SelfInvitation{}, map[string]bool{}
	for _, row := range *result.Requests {
		if row.User.ID != s.UserID || row.Group.ID != group || row.Created <= 0 || row.Created > 253402300799999 ||
			row.Result != -1 && row.Result != 0 && row.Result != 1 || !validText(row.User.Name, 1000, true) {
			return nil, false, &Failure{Code: "lajiaoliao_invitation_shape"}
		}
		if row.Source != 2 {
			continue // An ordinary application cannot authorize self acceptance.
		}
		if !ValidID(row.Inviter) || row.Inviter == s.UserID {
			return nil, false, &Failure{Code: "lajiaoliao_invitation_shape"}
		}
		item := SelfInvitation{Group: group, User: row.User.ID, Name: row.User.Name, Inviter: row.Inviter,
			Created: row.Created, Result: row.Result}
		if seen[item.ID()] {
			return nil, false, &Failure{Code: "lajiaoliao_invitation_shape"}
		}
		seen[item.ID()] = true
		out = append(out, item)
	}
	return out, page*100 < *result.Total, nil
}

func (c *Client) acceptSelfInvitation(ctx context.Context, s Session, change Change,
	fence func(context.Context, func(context.Context) error) error) error {
	if change.Target != s.UserID {
		return &Failure{Code: "lajiaoliao_invitation_self_required"}
	}
	// The wire response identifies group+user, not a request ID. Require exactly
	// one pending self invitation, and match its immutable fingerprint.
	pending, matched, complete := 0, false, false
	seen := map[string]bool{}
	for page := 1; page <= 100; page++ {
		rows, more, err := c.SelfInvitations(ctx, s, change.Group, page)
		if err != nil {
			return err
		}
		for _, row := range rows {
			id := row.ID()
			if seen[id] {
				return &Failure{Code: "lajiaoliao_invitation_shape"}
			}
			seen[id] = true
			if row.Result == 0 {
				pending++
				matched = matched || id == change.Text
			}
		}
		if !more {
			complete = true
			break
		}
	}
	if !complete || pending != 1 || !matched {
		return &Failure{Code: "lajiaoliao_invitation_self_required"}
	}
	return fence(ctx, func(work context.Context) error {
		_, err := c.request(work, "/group/group_application_response", map[string]any{
			"groupID": change.Group, "fromUserID": s.UserID, "handledMsg": "", "handleResult": 1,
		}, s.IMToken, "5", s.DeviceID)
		return err
	})
}
