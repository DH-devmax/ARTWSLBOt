package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/nativeauth"
)

func (m *ReceiverManager) RunDirectory(ctx context.Context) {
	var wg sync.WaitGroup
	for range ReadConcurrency {
		wg.Add(1)
		go func() { defer wg.Done(); m.directoryLoop(ctx) }()
	}
	wg.Wait()
}

func (m *ReceiverManager) directoryLoop(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		work, cancel := context.WithTimeout(ctx, 12*time.Second)
		_ = m.refreshDirectory(work)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (m *ReceiverManager) refreshDirectory(ctx context.Context) error {
	_, err := m.DB.Exec(ctx, `INSERT INTO native_directory_snapshots(account_id)
        SELECT id FROM accounts WHERE auth_state='authenticated' AND credential_sealed IS NOT NULL
        ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	var account string
	var requested bool
	err = m.DB.QueryRow(ctx, `WITH candidate AS (
        SELECT n.account_id,(n.attempted_at IS NULL OR n.requested_at>n.attempted_at) AS requested
        FROM native_directory_snapshots n JOIN accounts a ON a.id=n.account_id
            WHERE a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
                AND (n.retry_at IS NULL OR n.retry_at<=now())
                AND (n.attempted_at IS NULL OR n.requested_at>n.attempted_at
                    OR n.sampled_at IS NULL OR n.sampled_at<now()-interval '10 minutes')
            ORDER BY n.requested_at>n.attempted_at DESC NULLS LAST,n.attempted_at NULLS FIRST LIMIT 1 FOR UPDATE OF n SKIP LOCKED)
        UPDATE native_directory_snapshots n SET attempted_at=now(),retry_at=now()+interval '60 seconds'
        FROM candidate c WHERE n.account_id=c.account_id RETURNING n.account_id::text,c.requested`).Scan(&account, &requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	priority := ReadBackground
	if requested {
		priority = ReadRefresh
	}
	return m.read(ctx, account, "directory", priority, func(work context.Context) error {
		return m.readDirectory(work, account)
	})
}

func (m *ReceiverManager) readDirectory(ctx context.Context, account string) error {
	var nativePlatform string
	if err := m.DB.QueryRow(ctx, `SELECT platform FROM accounts WHERE id=$1`, account).Scan(&nativePlatform); err != nil {
		return err
	}
	if nativePlatform == "lajiaoliao" {
		return m.readChiliDirectory(ctx, account)
	}
	var sealed, deployment []byte
	var epoch, version int64
	err := m.DB.QueryRow(ctx, `SELECT a.credential_sealed,a.epoch,d.sealed,d.version FROM accounts a
        CROSS JOIN native_deployment d WHERE a.id=$1 AND a.auth_state='authenticated'`,
		account).Scan(&sealed, &epoch, &deployment, &version)
	if err != nil {
		return err
	}
	var d nativeauth.Deployment
	var saved savedNativeSession
	open := func(aad string, sealed []byte, out any) error {
		plain, e := m.Vault.Open(aad, sealed)
		if e != nil {
			return e
		}
		defer clear(plain)
		return json.Unmarshal(plain, out)
	}
	var groups []nativeauth.DirectoryGroup
	if open("native-deployment:v1", deployment, &d) != nil ||
		open("native-session:"+account, sealed, &saved) != nil || saved.DeploymentVersion != version {
		err = errors.New("native_directory_configuration")
	} else {
		var client *nativeauth.Client
		client, err = nativeauth.Resume(d, saved.Session, m.IDs(version))
		if err == nil {
			groups, err = client.Directory(ctx, saved.Session.UID)
			client.Close()
		}
	}
	code, retry := "", 60*time.Second
	var payload []byte
	if err != nil {
		code = "native_directory_unavailable"
		var native *nativeauth.Error
		if errors.As(err, &native) {
			switch native.Code {
			case "native_rate_limited":
				code, retry = native.Code, max(retry, min(native.RetryAfter, 24*time.Hour))
			case "native_reauth_required", "native_directory_shape", "native_directory_identity":
				code = native.Code
			}
		}
	} else {
		plain, marshalErr := json.Marshal(groups)
		if marshalErr != nil {
			return marshalErr
		}
		payload, err = m.Vault.Seal("native-directory:"+account, plain)
		clear(plain)
		if err != nil {
			return err
		}
	}
	// Failure retains the last good snapshot. Its age is always returned by API.
	readErr := err
	_, err = m.DB.Exec(ctx, `UPDATE native_directory_snapshots n SET
        payload_sealed=CASE WHEN $2='' THEN $3 ELSE payload_sealed END,
        sampled_at=CASE WHEN $2='' THEN now() ELSE sampled_at END,error=$2,
        retry_at=now()+$4::double precision*interval '1 second',
        group_count=CASE WHEN $2='' THEN $5 ELSE group_count END
        WHERE account_id=$1 AND EXISTS(SELECT 1 FROM accounts a CROSS JOIN native_deployment d
            WHERE a.id=n.account_id AND a.epoch=$6 AND a.auth_state='authenticated' AND d.version=$7)`,
		account, code, payload, retry.Seconds(), len(groups), epoch, version)
	if err == nil {
		m.Changes.Publish(Change{Account: account, Kind: "directory"})
	}
	if err != nil {
		return err
	}
	return readErr
}
