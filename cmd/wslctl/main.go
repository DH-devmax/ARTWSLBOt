package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	flags := flag.NewFlagSet("wslctl", flag.ContinueOnError)
	base := flags.String("url", "http://127.0.0.1:5173", "platform origin")
	session := flags.String("session", "var/cli-session.json", "private session file")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	args := flags.Args()
	if len(args) == 0 {
		return errors.New("usage: wslctl [--url origin] [--session file] login|logout|me|tests|test-create|test-cancel ID|operations|operation-create KEY|operation-cancel ID")
	}
	c, err := apiclient.New(*base)
	if err != nil {
		return err
	}
	if args[0] == "login" {
		f := flag.NewFlagSet("login", flag.ContinueOnError)
		name := f.String("username", "", "website username")
		stdin := f.Bool("password-stdin", false, "read password from standard input, never command arguments")
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || !*stdin {
			return errors.New("login requires --username NAME --password-stdin")
		}
		value, err := io.ReadAll(io.LimitReader(os.Stdin, 258))
		if err != nil || len(value) > 257 {
			return errors.New("invalid password input")
		}
		if err = c.Login(*name, strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r")); err != nil {
			return err
		}
		if err = c.Save(*session); err != nil {
			return err
		}
		fmt.Println("Authenticated. The CLI uses the same server permissions as WebUI.")
		return nil
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
	case "operations":
		path = "/operations"
	case "test-create", "operation-create":
		method = "POST"
		path = "/test-windows"
		if args[0] == "operation-create" {
			path = "/operations"
		}
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 65537))
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
	if path == "/operations" && method == "POST" {
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
	return json.NewEncoder(os.Stdout).Encode(result)
}
