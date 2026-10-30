package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/whoisrry/cobapram/internal/models"
)

var (
	ErrInvalidCredentials = errors.New("email atau password salah")
	// ErrUnauthenticated: access token tidak valid/kedaluwarsa atau sesinya
	// sudah dicabut. Error lain dari Authenticate berarti gangguan
	// infrastruktur (misalnya Redis tidak bisa dihubungi).
	ErrUnauthenticated = errors.New("tidak terautentikasi")
)

// Principal adalah identitas user yang sudah terautentikasi untuk satu request.
type Principal struct {
	UserID int64
	Name   string
	Role   models.Role
	SID    string
}

func (p Principal) IsAdmin() bool { return p.Role == models.RoleAdmin }

// Service menggabungkan database (data user), Issuer (JWT) dan SessionStore
// (Redis) untuk alur login, refresh, dan logout.
type Service struct {
	DB           *gorm.DB
	Tokens       *Issuer
	Sessions     *SessionStore
	QueryTimeout time.Duration
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Login memverifikasi kredensial dan membuat sesi baru (session id baru
// setiap login, mencegah session fixation).
func (s *Service) Login(ctx context.Context, email, password string) (Tokens, Principal, error) {
	user, err := s.findUser(ctx, "email = ?", NormalizeEmail(email))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		CheckPassword(dummyHash(), password) // samakan waktu respons
		return Tokens{}, Principal{}, ErrInvalidCredentials
	}
	if err != nil {
		return Tokens{}, Principal{}, err
	}
	if !CheckPassword(user.PasswordHash, password) {
		return Tokens{}, Principal{}, ErrInvalidCredentials
	}

	t, err := s.Tokens.Issue(user, randomID())
	if err != nil {
		return Tokens{}, Principal{}, err
	}
	if err := s.Sessions.Create(ctx, user.ID, t.SID, t.RefreshJTI); err != nil {
		return Tokens{}, Principal{}, err
	}
	return t, principalOf(user, t.SID), nil
}

// Authenticate memvalidasi access token dan memastikan sesinya belum dicabut.
// Pengecekan Redis per request membuat logout dan pencabutan langsung
// berlaku, tidak menunggu access token kedaluwarsa.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (Principal, error) {
	c, err := s.Tokens.Parse(accessToken, UseAccess)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	active, err := s.Sessions.Active(ctx, c.SID)
	if err != nil {
		return Principal{}, err
	}
	if !active {
		return Principal{}, ErrUnauthenticated
	}
	uid, _ := c.UserID()
	return Principal{UserID: uid, Name: c.Name, Role: c.Role, SID: c.SID}, nil
}

// Refresh merotasi refresh token: token lama tidak berlaku lagi dan
// pasangan token baru diterbitkan untuk sesi yang sama. Data user dibaca
// ulang dari database sehingga perubahan role atau user yang dihapus
// berlaku paling lambat setelah satu umur access token.
//
// Jika token lama dipakai ulang, seluruh sesi user dicabut dan
// ErrRefreshReused dikembalikan.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, Principal, error) {
	c, err := s.Tokens.Parse(refreshToken, UseRefresh)
	if err != nil {
		return Tokens{}, Principal{}, ErrRefreshInvalid
	}
	uid, _ := c.UserID()

	user, err := s.findUser(ctx, "id = ?", uid)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_ = s.Sessions.RevokeAll(ctx, uid)
		return Tokens{}, Principal{}, ErrRefreshInvalid
	}
	if err != nil {
		return Tokens{}, Principal{}, err
	}

	t, err := s.Tokens.Issue(user, c.SID)
	if err != nil {
		return Tokens{}, Principal{}, err
	}
	err = s.Sessions.Rotate(ctx, uid, c.SID, c.ID, t.RefreshJTI)
	if errors.Is(err, ErrRefreshReused) {
		if rerr := s.Sessions.RevokeAll(ctx, uid); rerr != nil {
			return Tokens{}, Principal{}, rerr
		}
		return Tokens{}, Principal{}, ErrRefreshReused
	}
	if err != nil {
		return Tokens{}, Principal{}, err
	}
	return t, principalOf(user, c.SID), nil
}

func (s *Service) Logout(ctx context.Context, p Principal) error {
	return s.Sessions.Revoke(ctx, p.UserID, p.SID)
}

func (s *Service) findUser(ctx context.Context, cond string, arg any) (models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, s.QueryTimeout)
	defer cancel()
	var u models.User
	err := s.DB.WithContext(ctx).Where(cond, arg).Take(&u).Error
	return u, err
}

func principalOf(u models.User, sid string) Principal {
	return Principal{UserID: u.ID, Name: u.Name, Role: u.Role, SID: sid}
}
