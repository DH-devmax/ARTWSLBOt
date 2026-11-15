package control

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/lajiaoliao"
)

// robotAddingProjection reads only the authenticated session's public identity.
// The native UID remains separate. Reading progress performs no upstream work.
func (s *GroupBindingService) robotAddingProjection(ctx context.Context, q Queryer, platform string, robot *GroupBindingRobot) error {
	robot.AddReason = "robot_adding_identity_unavailable"
	if s.Vault == nil {
		return nil
	}
	var sealed []byte
	if err := q.QueryRow(ctx, `SELECT credential_sealed FROM accounts
		WHERE id=$1 AND platform=$2 AND auth_state='authenticated'`, robot.ID, platform).Scan(&sealed); err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	domain := "native-session:"
	if platform == "lajiaoliao" {
		domain = "lajiaoliao-session:"
	}
	plain, err := s.Vault.Open(domain+robot.ID, sealed)
	if err != nil {
		return nil
	}
	defer clear(plain)
	var identity struct {
		Session struct {
			AccountID string `json:"account_id"`
			UID       uint64 `json:"uid"`
		} `json:"session"`
		UserID string `json:"user_id"`
	}
	if json.Unmarshal(plain, &identity) != nil {
		return nil
	}
	value := identity.Session.AccountID
	if platform == "lajiaoliao" {
		value = identity.UserID
		if value != robot.NativeID {
			return nil
		}
	} else if identity.Session.UID == 0 || strconv.FormatUint(identity.Session.UID, 10) != robot.NativeID {
		return nil
	}
	if !validRobotAddingAccount(platform, value) {
		return nil
	}
	robot.AddAccount, robot.AddAvailable, robot.AddReason = value, true, ""
	return nil
}

func validRobotAddingAccount(platform, value string) bool {
	switch platform {
	case "wsl":
		n, err := strconv.ParseUint(value, 10, 31)
		return err == nil && n > 0 && strconv.FormatUint(n, 10) == value
	case "lajiaoliao":
		return lajiaoliao.ValidID(value)
	default:
		return false
	}
}
