// Package aigateway defines bounded scheduling and exact AI cost arithmetic.
// Authorization, credentials and durable accounting belong to Go control.
package aigateway

import (
	"errors"
	"math/big"
)

const MicroPerCredit int64 = 1_000_000

type Rates struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}
type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

var ErrCost = errors.New("invalid_ai_cost")

// Rates are micro USDT / million tokens; input includes cache tokens.
// Sum first and round up once, to a millionth of a credit.
func Cost(u Usage, r Rates) (int64, error) {
	if u.Input < 0 || u.Output < 0 || u.Input+u.Output > 10_000_000 || u.CacheRead < 0 || u.CacheWrite < 0 ||
		u.CacheRead+u.CacheWrite > u.Input {
		return 0, ErrCost
	}
	sum := new(big.Int)
	for _, p := range [][2]int64{{u.Input - u.CacheRead - u.CacheWrite, r.Input}, {u.Output, r.Output}, {u.CacheRead, r.CacheRead}, {u.CacheWrite, r.CacheWrite}} {
		if p[1] < 0 || p[1] > 1_000_000_000_000 {
			return 0, ErrCost
		}
		sum.Add(sum, new(big.Int).Mul(big.NewInt(p[0]), big.NewInt(p[1])))
	}
	sum.Add(sum, big.NewInt(999))
	sum.Div(sum, big.NewInt(1000))
	if !sum.IsInt64() {
		return 0, ErrCost
	}
	return sum.Int64(), nil
}
