package apiclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	base *url.URL
	http *http.Client
}

type Session struct {
	Base    string    `json:"base"`
	Token   string    `json:"token"`
	CSRF    string    `json:"csrf"`
	Expires time.Time `json:"expires"`
}

type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Code) }

func New(base string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("an absolute API origin without credentials or a path is required")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || u.Hostname() == "localhost")) {
		return nil, errors.New("HTTPS is required except on localhost")
	}
	jar, _ := cookiejar.New(nil)
	return &Client{base: u, http: &http.Client{
		Jar: jar, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) Do(method, path string, body json.RawMessage) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\\r\n") || strings.Contains(path, "..") {
		return nil, errors.New("invalid API path")
	}
	if len(body) > 65536 || (len(body) > 0 && !json.Valid(body)) {
		return nil, errors.New("a bounded JSON request body is required")
	}
	req, err := http.NewRequest(method, c.base.String()+"/api/v1"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range c.http.Jar.Cookies(c.base) {
		if cookie.Name == "wsl_csrf" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
		}
	}
	if method == http.MethodPost && path == "/operations" {
		// Operation callers supply their own stable key in the JSON CLI wrapper.
		return nil, errors.New("use DoOperation with a stable idempotency key")
	}
	return c.request(req)
}

func (c *Client) DoOperation(body json.RawMessage, key string) (json.RawMessage, error) {
	if !json.Valid(body) || len(body) > 65536 || len(key) < 8 || len(key) > 100 || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("valid JSON and an explicit stable idempotency key are required")
	}
	req, err := http.NewRequest(http.MethodPost, c.base.String()+"/api/v1/operations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	for _, cookie := range c.http.Jar.Cookies(c.base) {
		if cookie.Name == "wsl_csrf" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
		}
	}
	return c.request(req)
}

func (c *Client) request(req *http.Request) (json.RawMessage, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("API transport failed; the result of a submitted action may be unknown")
	}
	defer resp.Body.Close()
	var result struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&result); err != nil {
		return nil, fmt.Errorf("API returned HTTP %d with an invalid response", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.Code != 0 {
		if len(result.Msg) > 120 {
			result.Msg = "request_failed"
		}
		return nil, &APIError{Status: resp.StatusCode, Code: result.Msg}
	}
	return result.Data, nil
}

func (c *Client) Login(username, password string) error {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	_, err := c.Do(http.MethodPost, "/auth/login", body)
	return err
}

func (c *Client) Save(path string) error {
	session := Session{Base: c.base.String()}
	for _, cookie := range c.http.Jar.Cookies(c.base) {
		switch cookie.Name {
		case "wsl_session":
			session.Token = cookie.Value
		case "wsl_csrf":
			session.CSRF = cookie.Value
		}
	}
	// The server remains authoritative on the real session expiration.
	session.Expires = time.Now().Add(12 * time.Hour)
	if len(session.Token) != 43 || len(session.CSRF) != 43 {
		return errors.New("server did not establish a complete session")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if existing, err := os.Lstat(path); err == nil && (!existing.Mode().IsRegular() || existing.Mode().Perm()&0077 != 0) {
		return errors.New("refusing an unsafe session file")
	}
	b, _ := json.Marshal(session)
	f, err := os.CreateTemp(filepath.Dir(path), ".wsl-session-*")
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

func (c *Client) Load(path string) error {
	stat, err := os.Lstat(path)
	if err != nil {
		return errors.New("login first; no CLI session found")
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() > 4096 {
		return errors.New("refusing an unsafe session file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var session Session
	if err = json.Unmarshal(b, &session); err != nil {
		return errors.New("invalid session file")
	}
	if session.Base != c.base.String() || session.Expires.Before(time.Now()) || len(session.Token) != 43 || len(session.CSRF) != 43 {
		return errors.New("CLI session expired or belongs to another server")
	}
	c.http.Jar.SetCookies(c.base, []*http.Cookie{
		{Name: "wsl_session", Value: session.Token, Path: "/", HttpOnly: true, Secure: c.base.Scheme == "https"},
		{Name: "wsl_csrf", Value: session.CSRF, Path: "/", Secure: c.base.Scheme == "https"},
	})
	return nil
}
