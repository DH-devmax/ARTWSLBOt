package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

var nativeGroupPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func (s *Server) nativeMembers(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Group string `json:"group_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var nativePlatform string
	if err := s.DB.Platform.QueryRow(r.Context(), `SELECT platform FROM accounts WHERE id=$1`, r.PathValue("id")).Scan(&nativePlatform); err != nil {
		return err
	}
	if nativePlatform == "lajiaoliao" {
		if in.Group == "" || len(in.Group) > 128 || s.Receiver == nil {
			return fail(409, "native_members_unavailable")
		}
		view, err := s.Receiver.MemberProjection(r.Context(), p.ID, p.TenantID, r.PathValue("id"), in.Group)
		if err != nil {
			return chiliFailure(err)
		}
		if err = s.memberResultAccess(r, p, view); err != nil {
			return err
		}
		send(w, 200, map[string]any{"snapshot": view, "read_only": true, "role_authorizes_actions": false, "native_ready": false})
		return nil
	}
	if !nativeGroupPattern.MatchString(in.Group) {
		return fail(400, "native_member_group")
	}
	if _, err := strconv.ParseUint(in.Group, 10, 64); err != nil {
		return fail(400, "native_member_group")
	}
	if s.Receiver == nil {
		return fail(503, "native_read_owner_unavailable")
	}
	view, err := s.Receiver.MemberProjection(r.Context(), p.ID, p.TenantID, r.PathValue("id"), in.Group)
	if err != nil {
		var native *nativeauth.Error
		switch {
		case errors.As(err, &native):
			if native.Code == "native_rate_limited" {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(native.RetryAfter.Seconds()))))
				return fail(429, native.Code)
			}
			return fail(409, native.Code)
		case errors.Is(err, gateway.ErrMemberDirectory), errors.Is(err, gateway.ErrMemberScope):
			return fail(409, err.Error())
		case errors.Is(err, gateway.ErrReadBusy):
			return fail(429, err.Error())
		default:
			return fail(503, "native_members_unavailable")
		}
	}
	// Waiting for native reads never keeps authorization alive. Recheck the
	// website session and account/deployment epochs before delivering identities.
	if err := s.memberResultAccess(r, p, view); err != nil {
		return err
	}
	send(w, 200, map[string]any{"snapshot": view, "read_only": true, "role_authorizes_actions": false, "native_ready": false})
	return nil
}

func (s *Server) memberResultAccess(r *http.Request, p Principal, view gateway.MemberSnapshot) error {
	return s.memberResultAccessForAccount(r, p, r.PathValue("id"), view)
}

func (s *Server) memberResultAccessForAccount(r *http.Request, p Principal, account string, view gateway.MemberSnapshot) error {
	if errors.Is(r.Context().Err(), context.Canceled) {
		return fail(409, "native_member_scope_changed")
	}
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return fail(401, "authentication_required")
	}
	var allowed bool
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(
        SELECT 1 FROM sessions se JOIN users u ON u.id=se.user_id
        CROSS JOIN accounts a LEFT JOIN native_deployment d ON a.platform='wsl'
        WHERE se.token_hash=$1 AND se.expires_at>now() AND NOT u.disabled
          AND u.id=$2 AND u.role='superadmin' AND a.id=$3 AND a.workspace=$4
          AND a.epoch=$5 AND coalesce(d.version,0)=$6 AND a.auth_state='authenticated')`,
		security.Digest(cookie.Value), p.ID, account, requestWorkspace(r),
		view.AccountEpoch, view.DeploymentVersion).Scan(&allowed)
	if err != nil {
		return fail(503, "database_unavailable")
	}
	if !allowed {
		return fail(403, "native_member_scope_changed")
	}
	return nil
}
