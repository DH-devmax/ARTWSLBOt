package gateway

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wangshangliao/internal/security"
)

type PostgresLedger struct {
	DB     *pgxpool.Pool
	Vault  *security.Vault
	mu     sync.Mutex
	groups map[string]string
}

func (p *PostgresLedger) Commit(ctx context.Context, messages []Message) error {
	if len(messages) == 0 || len(messages) > 32 {
		return fmt.Errorf("invalid commit batch")
	}
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL synchronous_commit=on`); err != nil {
		return err
	}
	resolved := map[string]string{}
	batch := &pgx.Batch{}
	for _, m := range messages {
		group := m.GroupID
		if m.NativeGroupID != "" {
			if group != "" {
				return fmt.Errorf("ambiguous group identity")
			}
			group = resolved[m.NativeGroupID]
			if group == "" {
				p.mu.Lock()
				group = p.groups[m.NativeGroupID]
				p.mu.Unlock()
			}
			if group == "" {
				err := tx.QueryRow(ctx, `INSERT INTO native_groups(native_id,name) VALUES($1,'待同步群资料')
                    ON CONFLICT(native_id) DO UPDATE SET native_id=excluded.native_id RETURNING id::text`,
					m.NativeGroupID).Scan(&group)
				if err != nil {
					return err
				}
				resolved[m.NativeGroupID] = group
			}
		}
		sealed, err := p.Vault.Seal("inbox:"+m.AccountID+":"+group+":"+m.NativeID, m.Payload)
		if err != nil {
			return err
		}
		state := "unresolved"
		if m.Resolved {
			state = "pending"
		}
		batch.Queue(`INSERT INTO inbox(account_id,group_id,native_message_id,sender_id,received_at,payload_sealed,state,binding_version)
            VALUES($1,$2,$3,$4,$5,$6,$7,(SELECT version FROM group_bindings WHERE group_id=$2))
            ON CONFLICT(account_id,group_id,native_message_id) DO NOTHING`,
			m.AccountID, group, m.NativeID, m.SenderID, m.ReceivedAt, sealed, state)
	}
	results := tx.SendBatch(ctx, batch)
	for range messages {
		if _, err = results.Exec(); err != nil {
			results.Close()
			return err
		}
	}
	if err = results.Close(); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Publish mappings only after commit, never after a rolled-back metadata insert.
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.groups == nil {
		p.groups = map[string]string{}
	}
	for native, id := range resolved {
		if len(p.groups) >= 2048 {
			for key := range p.groups {
				delete(p.groups, key)
				break
			}
		}
		p.groups[native] = id
	}
	return nil
}
