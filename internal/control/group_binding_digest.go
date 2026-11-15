package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/security"
)

func bindingTokenDigest(token string) []byte { return security.Digest(token) }

func bindingCreateDigest(in GroupBindingCreate) []byte {
	raw, _ := json.Marshal(in)
	return security.Digest(string(raw))
}

func bindingAcceptDigest(challenge string, in GroupBindingAcceptInput) []byte {
	raw, _ := json.Marshal(struct {
		Challenge string
		Input     GroupBindingAcceptInput
	}{challenge, in})
	return security.Digest(string(raw))
}

func bindingIdentityDigest(platform, group, identity string) []byte {
	raw, _ := json.Marshal([]string{platform, group, identity})
	return security.Digest(string(raw))
}

func bindingTokenMatches(expected []byte, text, prefix string) bool {
	token, ok := strings.CutPrefix(text, prefix)
	return ok && onboardingToken(token) && subtle.ConstantTimeCompare(expected, security.Digest(token)) == 1
}

func bindingHumanSender(ctx context.Context, q Queryer, platform, sender string) error {
	var pooled bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE platform=$1 AND native_uid=$2)`,
		platform, sender).Scan(&pooled); err != nil {
		return err
	}
	if pooled {
		return Denied(409, "group_binding_pool_identity")
	}
	return nil
}

func (s *GroupBindingService) connection(ctx context.Context, tx pgx.Tx, c bindingRecord, proof *GroupBindingConnectionEvidence) (string, error) {
	var self string
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT native_uid,credential_sealed FROM accounts WHERE id=$1 AND platform=$2
		AND workspace=$2 AND purpose='management' AND epoch=$3 AND version=$4
		AND auth_state='authenticated' AND credential_sealed IS NOT NULL FOR SHARE`,
		c.Account, c.Platform, c.Epoch, c.AccountVersion).Scan(&self, &sealed)
	if err == pgx.ErrNoRows {
		return "", Denied(409, "group_binding_account_changed")
	}
	if err != nil {
		return "", err
	}
	var owner string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1 AND enabled
		AND owner<>'' AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds' AND snapshot->>'state'='connected' FOR SHARE`, c.Account).
		Scan(&owner, &revision)
	if err == pgx.ErrNoRows {
		return "", Denied(409, "group_binding_receiver_unavailable")
	}
	if err != nil {
		return "", err
	}
	if proof != nil {
		deployment := int64(0)
		if c.Platform == "wsl" {
			// Configuration writers take the exclusive deployment lock. A row
			// share lock would also block this request's durable ID allocation.
			var pinned bool
			if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock_shared(798426)`).Scan(&pinned); err != nil {
				return "", err
			}
			if !pinned {
				return "", Denied(409, "group_binding_evidence_changed")
			}
			if err = tx.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton`).Scan(&deployment); err != nil {
				return "", err
			}
		}
		now := time.Now()
		if proof.Platform != c.Platform || proof.AccountID != c.Account || proof.AccountEpoch != c.Epoch ||
			proof.AccountVersion != c.AccountVersion || proof.ReceiverOwner != owner || proof.ReceiverRevision != revision ||
			proof.DeploymentVersion != deployment || subtle.ConstantTimeCompare(proof.CredentialDigest[:], security.Digest(string(sealed))) != 1 ||
			proof.ObservedAt.IsZero() || proof.ObservedAt.After(now) || now.Sub(proof.ObservedAt) > 5*time.Second {
			return "", Denied(409, "group_binding_evidence_changed")
		}
		if proof.MobileDeploymentVersion != 0 || proof.MobileSessionVersion != 0 {
			if c.Platform != "wsl" || proof.MobileDeploymentVersion < 1 || proof.MobileSessionVersion < 1 {
				return "", Denied(409, "native_mobile_search_changed")
			}
			var version int64
			err = tx.QueryRow(ctx, `SELECT version FROM native_mobile_deployment
				WHERE singleton AND version=$1 FOR SHARE`, proof.MobileDeploymentVersion).Scan(&version)
			if err == pgx.ErrNoRows {
				return "", Denied(409, "native_mobile_search_changed")
			}
			if err != nil {
				return "", err
			}
			err = tx.QueryRow(ctx, `SELECT m.version FROM native_mobile_sessions m
				JOIN native_mobile_deployment d ON d.singleton AND d.version=m.deployment_version
				WHERE m.account_id=$1 AND m.native_uid=$2 AND m.account_epoch=$3
				AND m.version=$4 AND d.version=$5 AND m.state='authenticated'
				AND m.credential_sealed IS NOT NULL FOR SHARE OF m`,
				c.Account, self, c.Epoch, proof.MobileSessionVersion, proof.MobileDeploymentVersion).Scan(&version)
			if err == pgx.ErrNoRows {
				return "", Denied(409, "native_mobile_search_changed")
			}
			if err != nil {
				return "", err
			}
		}
	}
	return self, nil
}
