package applypark

import (
	"database/sql"
	"strconv"
	"strings"
)

// ConfigMaxParked is the operator-only config row holding the cap
// (internal/operator reserves it).
const ConfigMaxParked = "applications.max_parked"

// Cap bounds: how many filled-but-unsubmitted tabs may be open at once.
const (
	DefaultMaxParked = 3
	MinMaxParked     = 1
	MaxMaxParked     = 10
)

// ClampMax puts n in the allowed range.
func ClampMax(n int) int {
	if n < MinMaxParked {
		return MinMaxParked
	}
	if n > MaxMaxParked {
		return MaxMaxParked
	}
	return n
}

// MaxParked reads the configured cap: DefaultMaxParked when unset or
// unreadable, otherwise clamped to 1..10.
func MaxParked(db *sql.DB) int {
	var raw string
	if err := db.QueryRow(`SELECT config_data FROM configs WHERE name = ?`, ConfigMaxParked).Scan(&raw); err != nil {
		return DefaultMaxParked
	}
	n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(raw), `"`))
	if err != nil {
		return DefaultMaxParked
	}
	return ClampMax(n)
}
