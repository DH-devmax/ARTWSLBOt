package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"wangshangliao/internal/apiclient"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	return runCLI(os.Args[1:], os.Stdin, os.Stdout)
}

func runCLI(argv []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("wslctl", flag.ContinueOnError)
	base := flags.String("url", "http://127.0.0.1:5173", "platform origin")
	session := flags.String("session", "var/cli-session.json", "private session file")
	challengeState := flags.String("challenge-state", "", "private single-use login state (default: session file + .challenge.json)")
	if err := flags.Parse(argv); err != nil {
		return err
	}
	args := flags.Args()
	if len(args) == 0 {
		return errors.New("usage: wslctl [--url origin] [--session file] [--challenge-state file] login-challenge|login|logout|me|tests|tools|tool-call KEY|test-create|test-cancel ID|operations|operation-create KEY|operation-cancel ID")
	}
	c, err := apiclient.New(*base)
	if err != nil {
		return err
	}
	if args[0] == "login-challenge" || args[0] == "login" {
		if *challengeState == "" {
			*challengeState = *session + ".challenge.json"
		}
		sessionPath, err := filepath.Abs(*session)
		if err != nil {
			return err
		}
		statePath, err := filepath.Abs(*challengeState)
		if err != nil {
			return err
		}
		sessionInfo, sessionErr := os.Stat(sessionPath)
		stateInfo, stateErr := os.Stat(statePath)
		if sessionPath == statePath || (sessionErr == nil && stateErr == nil && os.SameFile(sessionInfo, stateInfo)) {
			return errors.New("login challenge state must be separate from the authenticated session file")
		}
		f := flag.NewFlagSet(args[0], flag.ContinueOnError)
		name := f.String("username", "", "website username")
		var stdin bool
		var x, y int
		if args[0] == "login" {
			f.BoolVar(&stdin, "password-stdin", false, "read password from standard input, never command arguments")
			f.IntVar(&x, "captcha-x", -1, "human-provided tile X position in original image pixels")
			f.IntVar(&y, "captcha-y", -1, "human-provided tile Y position in original image pixels")
		}
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		if args[0] == "login-challenge" {
			if *name == "" || f.NArg() != 0 {
				return errors.New("login-challenge requires --username NAME")
			}
			challenge, err := c.IssueLoginChallenge(*name, *challengeState)
			if err != nil {
				return err
			}
			return json.NewEncoder(output).Encode(challenge)
		}
		if *name == "" || !stdin || x < 0 || y < 0 || f.NArg() != 0 {
			return errors.New("login requires --username NAME --password-stdin --captcha-x N --captcha-y N; run login-challenge first")
		}
		value, err := io.ReadAll(io.LimitReader(input, 259))
		password := strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r")
		if err != nil || len(value) > 258 || len(password) > 256 {
			return errors.New("invalid password input")
		}
		if err = c.LoginWithChallenge(*name, password, *challengeState, x, y); err != nil {
			return err
		}
		if err = c.Save(*session); err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "Authenticated. The CLI uses the same server permissions as WebUI.")
		return err
	}
	if err = c.Load(*session); err != nil {
		return err
	}
	method, path := "GET", ""
	var body json.RawMessage
	switch args[0] {
	case "me":
		path = "/me"
	case "tests":
		path = "/test-windows"
	case "tools":
		path = "/test-tools"
	case "operations":
		path = "/operations"
	case "test-create", "operation-create", "tool-call":
		method = "POST"
		path = "/test-windows"
		if args[0] == "operation-create" {
			path = "/operations"
		}
		if args[0] == "tool-call" {
			path = "/test-tools/call"
		}
		data, err := io.ReadAll(io.LimitReader(input, 65537))
		if err != nil || len(data) > 65536 || !json.Valid(data) {
			return errors.New("provide a JSON object of at most 64 KiB on standard input")
		}
		body = data
	case "test-cancel", "operation-cancel":
		if len(args) != 2 {
			return errors.New("a resource ID is required")
		}
		method, body = "POST", json.RawMessage(`{}`)
		path = "/test-windows/" + args[1] + "/cancel"
		if args[0] == "operation-cancel" {
			path = "/operations/" + args[1] + "/cancel"
		}
	case "logout":
		method, path, body = "POST", "/auth/logout", json.RawMessage(`{}`)
	default:
		return errors.New("unknown command")
	}
	var result json.RawMessage
	if path == "/test-tools/call" {
		if len(args) != 2 {
			return errors.New("tool-call requires a stable idempotency key")
		}
		result, err = c.DoTool(body, args[1])
	} else if path == "/operations" && method == "POST" {
		if len(args) != 2 {
			return errors.New("operation-create requires a stable idempotency key")
		}
		result, err = c.DoOperation(body, args[1])
	} else {
		result, err = c.Do(method, path, body)
	}
	if err != nil {
		return err
	}
	if args[0] == "logout" {
		if err = os.Remove(*session); err != nil {
			return err
		}
	}
	return json.NewEncoder(output).Encode(result)
}
