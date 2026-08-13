package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/nathanpls/godo/core/http/plugins/apikey"
)

const authGitignore = `*
!.gitignore
`

func (a *app) runAuth(arguments []string) error {
	if len(arguments) == 0 || isHelp(arguments) {
		return printHelp(a.stdout, authHelp)
	}
	switch arguments[0] {
	case "init":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, authInitHelp)
		}
		if len(arguments) != 1 {
			return errors.New("auth init does not accept arguments")
		}
		return a.initAuth()
	case "create":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, authCreateHelp)
		}
		name, scopes, err := parseAuthCreate(arguments[1:])
		if err != nil {
			return err
		}
		return a.createAuthKey(name, scopes)
	case "list", "ls":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, authListHelp)
		}
		if len(arguments) != 1 {
			return errors.New("auth list does not accept arguments")
		}
		return a.listAuthKeys()
	case "revoke":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, authRevokeHelp)
		}
		id, err := parseAuthID(arguments[1:])
		if err != nil {
			return err
		}
		return a.revokeAuthKey(id)
	default:
		return fmt.Errorf("unknown auth command %q; run godo auth --help", arguments[0])
	}
}

func parseAuthCreate(arguments []string) (string, []string, error) {
	var name string
	var scopes []string
	for i := 0; i < len(arguments); i++ {
		option, inline, hasInline := strings.Cut(arguments[i], "=")
		if option != "--name" && option != "--scope" {
			return "", nil, fmt.Errorf("unknown auth create option %q", arguments[i])
		}
		value := inline
		if !hasInline {
			i++
			if i >= len(arguments) {
				return "", nil, fmt.Errorf("%s requires a value", option)
			}
			value = arguments[i]
		}
		if option == "--name" {
			name = value
		} else {
			scopes = append(scopes, value)
		}
	}
	if strings.TrimSpace(name) == "" {
		return "", nil, errors.New("auth create requires --name <name>")
	}
	return name, scopes, nil
}

func parseAuthID(arguments []string) (int, error) {
	if len(arguments) != 1 {
		return 0, errors.New("auth revoke requires one key ID")
	}
	id, err := strconv.Atoi(arguments[0])
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid API key ID %q", arguments[0])
	}
	return id, nil
}

func (a *app) initAuth() error {
	root, path, err := a.authPath()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure auth directory: %w", err)
	}
	gitignore := filepath.Join(directory, ".gitignore")
	if err := ensureAuthGitignore(gitignore); err != nil {
		return err
	}
	if err := apikey.InitFile(path); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Initialized API key storage in %s\n", filepath.Join(root, ".godo", "auth.json"))
	fmt.Fprintln(a.stdout, "Next: godo auth create --name <name>")
	return nil
}

func (a *app) createAuthKey(name string, scopes []string) error {
	_, path, err := a.authPath()
	if err != nil {
		return err
	}
	identity, token, err := apikey.CreateKeyWithScopes(path, name, scopes)
	if err != nil {
		return err
	}
	response := fmt.Sprintf("Created API key %d: %s\nSecret (shown once):\n%s\n", identity.ID, identity.Name, token)
	if _, err := fmt.Fprint(a.stdout, response); err != nil {
		_, _ = apikey.RevokeKey(path, identity.ID)
		return fmt.Errorf("display API key secret: %w", err)
	}
	return nil
}

func (a *app) listAuthKeys() error {
	_, path, err := a.authPath()
	if err != nil {
		return err
	}
	keys, err := apikey.ListKeys(path)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		_, err := fmt.Fprintln(a.stdout, "No API keys.")
		return err
	}
	writer := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tNAME\tPREFIX\tSCOPES\tCREATED")
	for _, key := range keys {
		fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\n", key.ID, key.Name, key.Prefix, strings.Join(key.Scopes, ","), key.CreatedAt.Format("2006-01-02 15:04:05Z"))
	}
	return writer.Flush()
}

func (a *app) revokeAuthKey(id int) error {
	_, path, err := a.authPath()
	if err != nil {
		return err
	}
	revoked, err := apikey.RevokeKey(path, id)
	if err != nil {
		return err
	}
	if !revoked {
		return fmt.Errorf("API key %d does not exist", id)
	}
	_, err = fmt.Fprintf(a.stdout, "Revoked API key %d\n", id)
	return err
}

func ensureAuthGitignore(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := writeNewFile(path, []byte(authGitignore), 0o644); err != nil {
			return fmt.Errorf("protect auth directory from Git: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect auth .gitignore: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("auth .gitignore must be a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read auth .gitignore: %w", err)
	}
	if string(content) != authGitignore {
		return errors.New("existing .godo/.gitignore does not match the required secret protection")
	}
	return nil
}

func (a *app) authPath() (string, string, error) {
	root, err := projectRoot(a.cwd)
	if err != nil {
		return "", "", err
	}
	return root, filepath.Join(root, ".godo", "auth.json"), nil
}
