package apiclient

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoginChallenge projects only the server's public puzzle fields.
type LoginChallenge struct {
	ChallengeID string     `json:"challenge_id"`
	Image       LoginImage `json:"image"`
	ExpiresAt   time.Time  `json:"expires_at"`
}

type LoginImage struct {
	Background string `json:"background"`
	Tile       string `json:"tile"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	TileWidth  int    `json:"tile_width"`
	TileHeight int    `json:"tile_height"`
	InitialX   int    `json:"initial_x"`
	InitialY   int    `json:"initial_y"`
}

type loginChallengeState struct {
	Base        string    `json:"base"`
	Username    string    `json:"username"`
	ChallengeID string    `json:"challenge_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	HumanCookie string    `json:"wsl_human"`
}

func validLoginUsername(username string) bool {
	return strings.TrimSpace(username) != "" && len(username) <= 80
}

// IssueLoginChallenge saves browser proof separately from any authenticated session.
func (c *Client) IssueLoginChallenge(username, path string) (LoginChallenge, error) {
	var challenge LoginChallenge
	if !validLoginUsername(username) {
		return challenge, errors.New("a website username of at most 80 bytes is required")
	}
	body, _ := json.Marshal(map[string]string{"username": username})
	raw, err := c.Do(http.MethodPost, "/auth/captcha/challenge", body)
	if err != nil {
		return challenge, err
	}
	if json.Unmarshal(raw, &challenge) != nil || len(challenge.ChallengeID) != 43 ||
		!time.Now().Before(challenge.ExpiresAt) ||
		challenge.Image.Background == "" || challenge.Image.Tile == "" ||
		challenge.Image.Width <= 0 || challenge.Image.Height <= 0 ||
		challenge.Image.TileWidth <= 0 || challenge.Image.TileHeight <= 0 ||
		challenge.Image.TileWidth > challenge.Image.Width || challenge.Image.TileHeight > challenge.Image.Height {
		return LoginChallenge{}, errors.New("server returned an invalid login challenge")
	}
	state := loginChallengeState{Base: c.base.String(), Username: username,
		ChallengeID: challenge.ChallengeID, ExpiresAt: challenge.ExpiresAt}
	apiURL := *c.base
	apiURL.Path = "/api/v1/"
	for _, cookie := range c.http.Jar.Cookies(&apiURL) {
		if cookie.Name == "wsl_human" {
			state.HumanCookie = cookie.Value
		}
	}
	if len(state.HumanCookie) != 43 {
		return LoginChallenge{}, errors.New("server did not establish login browser proof")
	}
	if err := saveLoginChallenge(path, state); err != nil {
		return LoginChallenge{}, err
	}
	return challenge, nil
}

func saveLoginChallenge(path string, state loginChallengeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if existing, err := os.Lstat(path); err == nil {
		if !existing.Mode().IsRegular() || existing.Mode().Perm()&0077 != 0 {
			return errors.New("refusing an unsafe login challenge file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".wsl-login-challenge-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func consumeLoginChallenge(path string) (loginChallengeState, error) {
	var state loginChallengeState
	stat, err := os.Lstat(path)
	if err != nil {
		return state, errors.New("run login-challenge first; no CLI login challenge found")
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 4096 {
		return state, errors.New("refusing an unsafe login challenge file")
	}
	claimed, err := os.CreateTemp(filepath.Dir(path), ".wsl-login-consume-*")
	if err != nil {
		return state, err
	}
	claimedPath := claimed.Name()
	defer os.Remove(claimedPath)
	if err := claimed.Close(); err != nil {
		return state, err
	}
	// Renaming claims the file once, including across concurrent CLI processes.
	if err := os.Rename(path, claimedPath); err != nil {
		return state, errors.New("login challenge is no longer available; request a new challenge")
	}
	stat, err = os.Lstat(claimedPath)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 4096 {
		return state, errors.New("refusing an unsafe login challenge file")
	}
	f, err := os.Open(claimedPath)
	if err != nil {
		return state, err
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return state, err
	}
	if len(b) > 4096 || json.Unmarshal(b, &state) != nil {
		return loginChallengeState{}, errors.New("invalid login challenge file")
	}
	// No persisted challenge remains by the time verify can be submitted.
	if err := os.Remove(claimedPath); err != nil {
		return loginChallengeState{}, err
	}
	return state, nil
}

// LoginWithChallenge consumes local proof once and never retries verify or login.
func (c *Client) LoginWithChallenge(username, password, path string, x, y int) error {
	if !validLoginUsername(username) || len(password) > 256 || x < 0 || y < 0 {
		return errors.New("a valid username, password and nonnegative captcha coordinates are required")
	}
	state, err := consumeLoginChallenge(path)
	if err != nil {
		return err
	}
	if state.Base != c.base.String() || state.Username != username ||
		!time.Now().Before(state.ExpiresAt) || len(state.ChallengeID) != 43 || len(state.HumanCookie) != 43 {
		return errors.New("CLI login challenge expired or belongs to another server or username; request a new challenge")
	}
	cookie := &http.Cookie{Name: "wsl_human", Value: state.HumanCookie, Path: "/api/v1",
		HttpOnly: true, Secure: c.base.Scheme == "https", SameSite: http.SameSiteStrictMode}
	if cookie.Valid() != nil {
		return errors.New("invalid login browser proof; request a new challenge")
	}
	c.http.Jar.SetCookies(c.base, []*http.Cookie{cookie})
	body, _ := json.Marshal(struct {
		Username    string `json:"username"`
		ChallengeID string `json:"challenge_id"`
		X           int    `json:"x"`
		Y           int    `json:"y"`
	}{username, state.ChallengeID, x, y})
	raw, err := c.Do(http.MethodPost, "/auth/captcha/verify", body)
	if err != nil {
		return err
	}
	var ticket struct {
		Ticket    string    `json:"ticket"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if json.Unmarshal(raw, &ticket) != nil || len(ticket.Ticket) != 43 || !time.Now().Before(ticket.ExpiresAt) {
		return errors.New("server returned an invalid or expired login ticket; request a new challenge")
	}
	return c.Login(username, password, ticket.Ticket)
}
