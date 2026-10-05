package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const profileUsage = `usage:
  photon-server profile add -name NAME -role admin|member|restricted [-no-password]
(the password is read from the terminal, or from the first line of standard input)`

// profileCommand is how the first admin comes to exist: from the server's own command line, never
// a setup page that whoever reaches a new server first could claim.
func profileCommand(ctx context.Context, logger *slog.Logger, databaseURL string, out io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "add" {
		return errors.New(profileUsage)
	}
	fs := flag.NewFlagSet("profile add", flag.ContinueOnError)
	name := fs.String("name", "", "the profile's name")
	role := fs.String("role", "", "admin, member or restricted")
	noPassword := fs.Bool("no-password", false, "a profile chosen on a signed-in device, never signed in with directly")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *name == "" || fs.NArg() != 0 {
		return errors.New(profileUsage)
	}
	r, err := domain.ParseRole(*role)
	if err != nil {
		return err
	}
	if r == domain.RoleAdmin && *noPassword {
		return auth.ErrAdminPassword
	}
	st, err := store.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer st.Close()
	hash := ""
	if !*noPassword {
		password, err := readPassword(out)
		if err != nil {
			return err
		}
		if hash, err = auth.HashPassword(ctx, password); err != nil {
			return err
		}
	}
	p, err := st.AddProfile(ctx, *name, r, hash)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "added %s (%s)\n", p.Name, p.Role)
	return err
}

func readPassword(out io.Writer) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		_, _ = fmt.Fprint(out, "Password: ")
		b, err := term.ReadPassword(fd)
		_, _ = fmt.Fprintln(out)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
