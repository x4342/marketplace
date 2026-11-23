package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/platform"
	"golang.org/x/term"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	action := args[0]
	claim := len(args) == 2 && args[1] == "--claim-existing"
	switch action {
	case "bootstrap":
		if len(args) != 1 && !claim {
			return usage()
		}
	case "restore-admin":
		if len(args) != 1 {
			return usage()
		}
	case "set-role":
		if len(args) != 3 || (args[2] != "user" && args[2] != "moderator") {
			return usage()
		}
	default:
		return usage()
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("an interactive terminal is required; use docker compose exec without -T")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	password, err := secret("Administrator password: ")
	if err != nil {
		return err
	}
	var code []byte
	if action == "bootstrap" && !claim {
		check, err := secret("Confirm a new administrator password: ")
		if err != nil {
			return err
		}
		defer clear(check)
		if subtle.ConstantTimeCompare(password, check) != 1 {
			return errors.New("passwords do not match")
		}
	}
	defer clear(password)
	if claim {
		fmt.Fprintln(os.Stderr, "Verified claim will revoke existing sessions and replace all recovery codes.")
		code, err = secret("Current recovery code: ")
		if err != nil {
			return err
		}
		defer clear(code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := platform.OpenDatabase(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	service := identity.New(pool)
	switch action {
	case "bootstrap":
		var codes []string
		codes, err = service.BootstrapAdmin(ctx, string(password), string(code), claim)
		if err == nil {
			fmt.Println("Administrator bootstrapped. Store these one-time recovery codes privately:")
			for _, c := range codes {
				fmt.Println(c)
			}
		}
	case "restore-admin":
		err = service.RestoreAdmin(ctx, string(password))
		if err == nil {
			fmt.Println("Original administrator role is active.")
		}
	case "set-role":
		err = service.OperatorSetRole(ctx, string(password), args[1], args[2])
		if err == nil {
			fmt.Println("Selected account role is active.")
		}
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrBootstrapComplete), errors.Is(err, identity.ErrAdminNameInUse), errors.Is(err, identity.ErrInvalid), errors.Is(err, identity.ErrOperator):
		return err
	default:
		return errors.New("administrator operation failed; verify the migrated database and selected account")
	}
}

func secret(prompt string) ([]byte, error) {
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, errors.New("could not read the secret from the terminal")
	}
	return value, nil
}

func usage() error {
	return errors.New("usage: marketplace-admin bootstrap [--claim-existing] | restore-admin | set-role USERNAME user|moderator")
}
