package control

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Original ChaCha20-Poly1305 uses an eight-byte nonce. Seed an unused ledger
// randomly, then share its durable counter across all local senders. A restored
// ledger must be reconciled before sending; wall clocks are not a high-water mark.
func reserveNativeNonce(ctx context.Context, tx pgx.Tx) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	seed := binary.LittleEndian.Uint64(random[:])
	if seed == 0 || seed == ^uint64(0) {
		return "", Denied(503, "native_random_unavailable")
	}
	var value string
	err := tx.QueryRow(ctx, `UPDATE native_message_nonce
		SET value=CASE WHEN value=0 THEN $1::numeric ELSE value+1 END
		WHERE singleton AND value<18446744073709551615 RETURNING value::text`,
		strconv.FormatUint(seed, 10)).Scan(&value)
	return value, err
}
