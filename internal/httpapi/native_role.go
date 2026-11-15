package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func (s *Server) nativeRole(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Group string `json:"group_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if requestWorkspace(r) == "lajiaoliao" {
		if !lajiaoliao.ValidID(in.Group) {
			return fail(400, "native_member_group")
		}
		if s.Receiver == nil {
			return fail(503, "native_read_owner_unavailable")
		}
		view, err := s.Receiver.Moderation(r.Context(), r.PathValue("id"), in.Group)
		if err != nil {
			return nativeSettingsError(w, err)
		}
		result, err := s.nativeSettingsService().RecordChiliRole(r.Context(), assistantRef(r), r.PathValue("id"), in.Group, view.GroupName, settingEvidence(view.SettingSnapshot))
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"observation": result, "grants_ownership": false, "native_ready": false})
		return nil
	}
	if !nativeGroupPattern.MatchString(in.Group) {
		return fail(400, "native_member_group")
	}
	if _, err := strconv.ParseUint(in.Group, 10, 64); err != nil {
		return fail(400, "native_member_group")
	}
	if requestWorkspace(r) != "wsl" {
		return fail(403, "workspace_forbidden")
	}
	if s.Receiver == nil {
		return fail(503, "native_read_owner_unavailable")
	}
	// Only group ID is accepted. Role, identity, owner, epochs and sample time
	// are collected from the authenticated connection, not supplied by a user.
	proof, err := s.Receiver.ObserveRole(r.Context(), r.PathValue("id"), in.Group)
	if err != nil {
		var upstream *nativeauth.Error
		switch {
		case errors.As(err, &upstream):
			if upstream.Code == "native_rate_limited" {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(upstream.RetryAfter.Seconds()))))
				return fail(429, upstream.Code)
			}
			return fail(409, upstream.Code)
		case errors.Is(err, gateway.ErrTextOwner), errors.Is(err, gateway.ErrTextRoute):
			return fail(409, err.Error())
		case errors.Is(err, gateway.ErrReadBusy):
			return fail(429, gateway.ErrReadBusy.Error())
		default:
			return fail(503, "native_role_unavailable")
		}
	}
	result, err := control.RecordNativeRole(r.Context(), s.DB.Platform, assistantRef(r), proof)
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"observation": result, "grants_ownership": false, "native_ready": false})
	return nil
}
