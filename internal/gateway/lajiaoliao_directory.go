package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func (m *ReceiverManager) ChiliMemberSnapshot(ctx context.Context, account, group string) (MemberSnapshot, error) {
	value, err := m.readValue(ctx, account, "lajiao-members-all:"+group, ReadRefresh, func(work context.Context) (any, error) {
		s, epoch, err := m.chiliDirectorySession(work, account)
		if err != nil {
			return nil, err
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		self, err := client.Member(work, s, group, s.UserID)
		if err != nil {
			return nil, err
		}
		view := MemberSnapshot{AccountEpoch: epoch, Team: group, SelfRole: map[int]string{20: "member", 60: "admin", 100: "owner"}[self.Role]}
		view.Group = group
		view.Members = []nativeauth.GroupMember{}
		view.MappingComplete = true
		seen := map[string]bool{}
		for page := 1; page <= 100; page++ {
			members, more, e := client.Members(work, s, group, page)
			if e != nil {
				return nil, e
			}
			view.Pages = page
			for _, person := range members {
				if seen[person.UserID] {
					return nil, errors.New("lajiaoliao_directory_shape")
				}
				seen[person.UserID] = true
				view.Members = append(view.Members, nativeauth.GroupMember{ID: person.UserID, Transport: person.UserID,
					Name: person.Name, Card: person.Name, Role: map[int]string{20: "member", 60: "admin", 100: "owner"}[person.Role]})
			}
			if !more {
				view.Complete = true
				break
			}
		}
		if !view.Complete {
			return nil, errors.New("lajiaoliao_directory_truncated")
		}
		if err = m.saveChiliRole(work, account, group, view.SelfRole, epoch); err != nil {
			return nil, err
		}
		view.SampledAt = time.Now().UTC()
		return view, nil
	})
	if err != nil {
		return MemberSnapshot{}, err
	}
	view, ok := value.(MemberSnapshot)
	if !ok {
		return view, ErrMemberScope
	}
	return view, nil
}

func (m *ReceiverManager) chiliDirectorySession(ctx context.Context, account string) (lajiaoliao.Session, int64, error) {
	var sealed []byte
	var epoch int64
	if err := m.DB.QueryRow(ctx, `SELECT credential_sealed,epoch FROM accounts WHERE id=$1
        AND platform='lajiaoliao' AND auth_state='authenticated'`, account).Scan(&sealed, &epoch); err != nil {
		return lajiaoliao.Session{}, 0, err
	}
	plain, err := m.Vault.Open("lajiaoliao-session:"+account, sealed)
	if err != nil {
		return lajiaoliao.Session{}, 0, err
	}
	defer clear(plain)
	var s lajiaoliao.Session
	if json.Unmarshal(plain, &s) != nil || s.UserID == "" || s.IMToken == "" {
		return s, 0, errors.New("receiver_configuration_invalid")
	}
	return s, epoch, nil
}

func (m *ReceiverManager) readChiliDirectory(ctx context.Context, account string) error {
	s, epoch, err := m.chiliDirectorySession(ctx, account)
	if err != nil {
		return err
	}
	client := lajiaoliao.NewClient()
	defer client.Close()
	groups := []nativeauth.DirectoryGroup{}
	seen := map[string]bool{}
	for page := 1; page <= 10; page++ {
		rows, more, e := client.Groups(ctx, s, page)
		if e != nil {
			err = e
			break
		}
		for _, g := range rows {
			if seen[g.ID] {
				err = errors.New("lajiaoliao_directory_shape")
				break
			}
			seen[g.ID] = true
			name := strings.Join(strings.Fields(g.Name), " ")
			if name == "" {
				name = g.ID
			}
			groups = append(groups, nativeauth.DirectoryGroup{ID: g.ID, Team: g.ID, Name: name, Role: "unknown"})
		}
		if err != nil || !more {
			break
		}
		if page == 10 {
			err = errors.New("lajiaoliao_directory_truncated")
		}
	}
	code := ""
	retry := time.Minute
	var sealed []byte
	if err != nil {
		code = "lajiaoliao_directory_unavailable"
		var e *lajiaoliao.Failure
		if errors.As(err, &e) {
			code = e.Code
			if e.RetryAfter > retry {
				retry = e.RetryAfter
			}
		}
	} else {
		plain, e := json.Marshal(groups)
		if e != nil {
			return e
		}
		sealed, e = m.Vault.Seal("native-directory:"+account, plain)
		clear(plain)
		if e != nil {
			return e
		}
	}
	_, saveErr := m.DB.Exec(ctx, `UPDATE native_directory_snapshots n SET
        payload_sealed=CASE WHEN $2='' THEN $3 ELSE payload_sealed END,
        sampled_at=CASE WHEN $2='' THEN now() ELSE sampled_at END,error=$2,
        retry_at=now()+$4::double precision*interval '1 second',
        group_count=CASE WHEN $2='' THEN $5 ELSE group_count END
        WHERE account_id=$1 AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=n.account_id AND a.epoch=$6
            AND a.platform='lajiaoliao' AND a.auth_state='authenticated')`,
		account, code, sealed, retry.Seconds(), len(groups), epoch)
	if saveErr != nil {
		return saveErr
	}
	m.Changes.Publish(Change{Account: account, Kind: "directory"})
	return err
}

// This stores native evidence only, not tenant authorization or action approval.
func (m *ReceiverManager) saveChiliRole(work context.Context, account, group, role string, epoch int64) error {
	tx, err := m.DB.Begin(work)
	if err != nil {
		return err
	}
	defer tx.Rollback(work)
	var id string
	if err = tx.QueryRow(work, `SELECT id::text FROM accounts WHERE id=$1 AND epoch=$2 AND platform='lajiaoliao'
            AND auth_state='authenticated' FOR SHARE`, account, epoch).Scan(&id); err != nil {
		return err
	}
	if err = tx.QueryRow(work, `INSERT INTO native_groups(platform,native_id,name)VALUES('lajiaoliao',$1,$1)
            ON CONFLICT(platform,native_id)DO UPDATE SET native_id=excluded.native_id RETURNING id::text`, group).Scan(&id); err != nil {
		return err
	}
	if _, err = tx.Exec(work, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
            VALUES($1,$2,$3,now(),'native')ON CONFLICT(account_id,group_id)DO UPDATE SET role=excluded.role,observed_at=now(),
            observation_source='native'`, account, id, role); err != nil {
		return err
	}
	return tx.Commit(work)
}
