package httpapi

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) activities(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT a.*,(SELECT count(*) FROM activity_members m WHERE m.activity_id=a.id) participant_count
        FROM activities a ORDER BY created_at DESC LIMIT 200`)
}

func (s *Server) createActivity(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		GroupID  string    `json:"group_id"`
		Name     string    `json:"name"`
		ClosesAt time.Time `json:"closes_at"`
		Winners  int       `json:"winners"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(in.GroupID) || in.Name == "" || len(in.Name) > 200 || !in.ClosesAt.After(time.Now()) || in.Winners < 1 || in.Winners > 100 {
		return fail(400, "invalid_activity")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	tenant, err := authorizeBusiness(r, tx, p, in.GroupID, "activities")
	if err != nil {
		return err
	}
	var id string
	if err = tx.QueryRow(r.Context(), `INSERT INTO activities(tenant_id,group_id,name,closes_at,winners)
        VALUES($1,$2,$3,$4,$5) RETURNING id::text`, tenant, in.GroupID, in.Name, in.ClosesAt, in.Winners).Scan(&id); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "activity_created", id, in); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 201, map[string]string{"id": id})
	return nil
}

func (s *Server) drawActivity(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var group, tenant, state string
	var closes time.Time
	var winners int
	var version int64
	err = tx.QueryRow(r.Context(), `SELECT group_id::text,tenant_id::text,state,closes_at,winners,version FROM activities WHERE id=$1 FOR UPDATE`,
		r.PathValue("id")).Scan(&group, &tenant, &state, &closes, &winners, &version)
	if err == pgx.ErrNoRows || (err == nil && p.Role != "superadmin" && tenant != p.TenantID) {
		return fail(404, "activity_not_found")
	}
	if err != nil {
		return err
	}
	if state != "open" || closes.After(time.Now()) || version != in.Version {
		return fail(409, "activity_not_ready")
	}
	if _, err = authorizeBusiness(r, tx, p, group, "activities"); err != nil {
		return err
	}
	rows, err := tx.Query(r.Context(), `SELECT member_id FROM activity_members WHERE activity_id=$1 ORDER BY member_id`, r.PathValue("id"))
	if err != nil {
		return err
	}
	members := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		members = append(members, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(members) < winners {
		return fail(409, "insufficient_participants")
	}
	selected := make([]string, 0, winners)
	for i := 0; i < winners; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(members)-i)))
		if err != nil {
			return err
		}
		j := i + int(n.Int64())
		members[i], members[j] = members[j], members[i]
		selected = append(selected, members[i])
		if _, err = tx.Exec(r.Context(), `UPDATE activity_members SET winner=true WHERE activity_id=$1 AND member_id=$2`, r.PathValue("id"), members[i]); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), `UPDATE activities SET state='drawn',version=version+1 WHERE id=$1`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "activity_drawn", r.PathValue("id"), map[string]any{"winners": selected}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"winners": selected, "version": version + 1})
	return nil
}
