package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"text/tabwriter"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/store"
)

const usage = `Usage: diarum <command> [options]

Commands:
  serve                           Start the server (default)
  version                         Print the version
  users list                      List users and their roles
  users set-role <user> <role>    Set a user's role to "user" or "admin"
                                  (<user> is a username, email or ID)
  help                            Show this help

Common options:
  --data-dir DIR   Data directory (default: $DIARUM_DATA_PATH or ./diarum_data)
  --json           (users list) Print JSON instead of a table
`

// runUsers handles "diarum users ...". It opens the same database the server
// uses, so it also works inside a running container.
func runUsers(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing subcommand: users list | users set-role <user> <role>")
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("users "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dataDir := fs.String("data-dir", getDataDir(), "the directory to store application data")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, err := parseInterspersed(fs, rest)
	if err != nil {
		return err
	}

	switch sub {
	case "list", "ls":
		if len(positional) != 0 {
			return errors.New("usage: diarum users list [--data-dir DIR] [--json]")
		}
		appStore, err := store.Open(*dataDir)
		if err != nil {
			return err
		}
		defer appStore.Close()
		return listUsers(appStore, stdout, *asJSON)
	case "set-role", "role":
		if len(positional) != 2 {
			return errors.New("usage: diarum users set-role <username|email|id> <user|admin> [--data-dir DIR]")
		}
		role := strings.ToLower(strings.TrimSpace(positional[1]))
		if !store.ValidRole(role) {
			return store.ErrInvalidRole
		}
		appStore, err := store.Open(*dataDir)
		if err != nil {
			return err
		}
		defer appStore.Close()
		return setUserRole(appStore, stdout, positional[0], role)
	default:
		return fmt.Errorf("unknown users subcommand: %s", sub)
	}
}

// parseInterspersed lets flags appear before, between or after positional
// arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	positional := []string{}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func listUsers(appStore *store.Store, stdout io.Writer, asJSON bool) error {
	users, total, err := appStore.ListUsers(store.UserListOptions{Sort: "created"})
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(users)
	}
	if total == 0 {
		_, err := fmt.Fprintln(stdout, "No users yet.")
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSERNAME\tEMAIL\tROLE\tDIARIES\tCREATED\tLAST DIARY")
	for _, u := range users {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", u.ID, u.Username, dash(u.Email), u.Role, u.Diaries, shortTime(u.Created), shortTime(u.LastDiaryAt))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "\n%d user(s)\n", total)
	return err
}

func setUserRole(appStore *store.Store, stdout io.Writer, identity, role string) error {
	target, err := appStore.FindUser(identity)
	if err != nil {
		return fmt.Errorf("user %q not found", identity)
	}
	if target.Role == role {
		_, err := fmt.Fprintf(stdout, "%s is already %s.\n", target.Username, role)
		return err
	}
	if err := appStore.SetUserRole(target.ID, role); err != nil {
		return err
	}
	detail := map[string]any{"user_id": target.ID, "from": target.Role, "to": role}
	if current, err := user.Current(); err == nil {
		detail["os_user"] = current.Username
	}
	if host, err := os.Hostname(); err == nil {
		detail["host"] = host
	}
	if err := audit.AppendDirect(appStore.DataDir, audit.Entry{Source: audit.SourceCLI, Action: audit.ActionAdminRole, Target: target.Username, Detail: detail}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write audit log: %v\n", err)
	}
	if _, err := fmt.Fprintf(stdout, "%s: %s -> %s\n", target.Username, target.Role, role); err != nil {
		return err
	}
	if role == store.RoleUser {
		if admins, err := appStore.CountAdmins(); err == nil && admins == 0 {
			fmt.Fprintln(stdout, "Note: there is no admin left; promote one with 'diarum users set-role <user> admin'.")
		}
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// shortTime trims stored timestamps ("2006-01-02 15:04:05.000Z") to minutes.
func shortTime(s string) string {
	if len(s) >= 16 {
		return s[:16]
	}
	return dash(s)
}
