package httpapi

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/nativeauth"
)

func (s *Server) recordNativeAuthDiagnostic(ctx context.Context, diagnostic nativeauth.FailureDiagnostic) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err == nil {
		defer tx.Rollback(ctx)
		var exists int
		err = tx.QueryRow(ctx, `SELECT 1 FROM accounts WHERE id=$1 FOR KEY SHARE`,
			diagnostic.AccountID).Scan(&exists)
		if err == pgx.ErrNoRows {
			return
		}
		if err == nil {
			err = auditTx(ctx, tx, Principal{ID: diagnostic.ActorID}, "", "native_auth_rejected",
				diagnostic.AccountID, diagnostic)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
	}
	if err != nil {
		slog.Warn("native authentication diagnostic unavailable", "error_type", "persistence")
	}
}
