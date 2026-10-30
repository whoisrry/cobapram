// Command create-user membuat akun baru (aplikasi tidak memiliki self-register).
//
//	create-user --email a@b.com --name "Nama" --role client
//
// Tanpa --password, password dibaca dari stdin: diminta secara interaktif jika
// stdin adalah terminal, atau dibaca dari baris pertama jika di-pipe. Cara
// ini menghindari password tercatat di shell history.
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
	"time"

	"golang.org/x/term"

	"github.com/whoisrry/cobapram/internal/config"
	"github.com/whoisrry/cobapram/internal/db"
	"github.com/whoisrry/cobapram/internal/models"
	"github.com/whoisrry/cobapram/internal/user"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	email := flag.String("email", "", "email user (wajib)")
	name := flag.String("name", "", "nama user (wajib)")
	role := flag.String("role", "client", "role: client atau admin")
	password := flag.String("password", "", "password (tidak disarankan, tercatat di shell history; kosongkan untuk membaca dari stdin)")
	flag.Parse()

	in := user.NewUser{Name: *name, Email: *email, Role: models.Role(*role)}
	if err := user.Validate(in); err != nil {
		return err
	}

	if *password != "" {
		fmt.Fprintln(os.Stderr, "peringatan: --password tercatat di shell history; sebaiknya gunakan stdin")
		in.Password = *password
	} else {
		var err error
		if in.Password, err = readPassword(); err != nil {
			return err
		}
	}

	dbCfg, err := config.LoadDB()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	gdb, err := db.Open(ctx, dbCfg, logger)
	if err != nil {
		return err
	}

	u, err := user.Create(ctx, gdb, in)
	if err != nil {
		return err
	}
	fmt.Printf("user dibuat: id=%d email=%s role=%s\n", u.ID, u.Email, u.Role)
	return nil
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Password: ")
		p1, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(os.Stderr, "Ulangi password: ")
		p2, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(p1) != string(p2) {
			return "", errors.New("password tidak sama")
		}
		return string(p1), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
