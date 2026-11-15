// Package webcaptcha issues scoped, single-use slider challenges and tickets.
package webcaptcha

import (
	"errors"
	"time"
)

var (
	ErrInvalid     = errors.New("webcaptcha_invalid")
	ErrLimited     = errors.New("webcaptcha_limited")
	ErrBusy        = errors.New("webcaptcha_busy")
	ErrUnavailable = errors.New("webcaptcha_unavailable")
)

type Image struct {
	Background string `json:"background"`
	Tile       string `json:"tile"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	TileWidth  int    `json:"tile_width"`
	TileHeight int    `json:"tile_height"`
	InitialX   int    `json:"initial_x"`
	InitialY   int    `json:"initial_y"`
}

// Puzzle is generator output, including the server-side answer.
type Puzzle struct {
	Image Image `json:"image"`
	X     int   `json:"-"`
	Y     int   `json:"-"`
}

type Generator func() (Puzzle, error)

// Scope fields are compared verbatim. The HTTP caller defines purpose values.
type Scope struct {
	Purpose, Subject, Browser, Session, Host, IP, Platform, Target string
}

type Challenge struct {
	ChallengeID string    `json:"challenge_id"`
	Image       Image     `json:"image"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Ticket struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
}
