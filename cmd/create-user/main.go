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
	"net/mail"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
	"gorm.io/gorm"

	"github.com/whoisrry/cobapram/internal/auth"
	"github.com/whoisrry/cobapram/internal/config"
	"github.com/whoisrry/cobapram/internal/db"
	"github.com/whoisrry/cobapram/internal/models"
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

	u := models.User{
		Email: auth.NormalizeEmail(*email),
		Name:  strings.TrimSpace(*name),
		Role:  models.Role(*role),
	}
	if addr, err := mail.ParseAddress(u.Email); err != nil || addr.Address != u.Email || len(u.Email) > 254 {
		return errors.New("--email tidak valid")
	}
	if u.Name == "" || utf8.RuneCountInString(u.Name) > 100 {
		return errors.New("--name wajib diisi, maksimal 100 karakter")
	}
	if !u.Role.Valid() {
		return errors.New("--role harus client atau admin")
	}

	pw := *password
	if pw != "" {
		fmt.Fprintln(os.Stderr, "peringatan: --password tercatat di shell history; sebaiknya gunakan stdin")
	} else {
		var err error
		if pw, err = readPassword(); err != nil {
			return err
		}
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	u.PasswordHash = hash

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

	err = gdb.WithContext(ctx).Create(&u).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("email %s sudah terdaftar", u.Email)
	}
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
