package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/whoisrry/cobapram/internal/models"
)

type TokenUse string

const (
	UseAccess  TokenUse = "access"
	UseRefresh TokenUse = "refresh"
)

var ErrWrongTokenUse = errors.New("jenis token tidak sesuai")

// Claims dipakai untuk access dan refresh token. Field TokenUse mencegah
// refresh token dipakai sebagai access token (dan sebaliknya).
type Claims struct {
	Name     string      `json:"name,omitempty"`
	Role     models.Role `json:"role"`
	SID      string      `json:"sid"`
	TokenUse TokenUse    `json:"token_use"`
	jwt.RegisteredClaims
}

func (c *Claims) UserID() (int64, error) {
	return strconv.ParseInt(c.Subject, 10, 64)
}

// Tokens adalah pasangan token hasil login atau refresh.
type Tokens struct {
	Access        string
	AccessExpiry  time.Time
	Refresh       string
	RefreshExpiry time.Time
	RefreshJTI    string
	SID           string
}

// Issuer menandatangani dan memvalidasi JWT dengan HS256.
//
// HS256 dipilih karena hanya satu service yang menandatangani dan memverifikasi
// token; tidak ada pihak ketiga yang perlu memverifikasi dengan public key,
// jadi keunggulan EdDSA (kunci asimetris) tidak terpakai, sementara HS256
// cukup dengan satu secret dari environment.
type Issuer struct {
	secret     []byte
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

func NewIssuer(secret []byte, issuer, audience string, accessTTL, refreshTTL time.Duration) *Issuer {
	return &Issuer{
		secret:     secret,
		issuer:     issuer,
		audience:   audience,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		now:        time.Now,
	}
}

// Issue membuat access token dan refresh token baru untuk sesi sid.
func (i *Issuer) Issue(user models.User, sid string) (Tokens, error) {
	now := i.now()
	t := Tokens{SID: sid, RefreshJTI: randomID()}

	var err error
	t.AccessExpiry = now.Add(i.accessTTL)
	t.Access, err = i.sign(Claims{
		Name: user.Name, Role: user.Role, SID: sid, TokenUse: UseAccess,
		RegisteredClaims: i.registered(user.ID, randomID(), now, t.AccessExpiry),
	})
	if err != nil {
		return Tokens{}, err
	}

	t.RefreshExpiry = now.Add(i.refreshTTL)
	t.Refresh, err = i.sign(Claims{
		SID: sid, TokenUse: UseRefresh,
		RegisteredClaims: i.registered(user.ID, t.RefreshJTI, now, t.RefreshExpiry),
	})
	if err != nil {
		return Tokens{}, err
	}
	return t, nil
}

func (i *Issuer) registered(userID int64, jti string, now, exp time.Time) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Issuer:    i.issuer,
		Subject:   strconv.FormatInt(userID, 10),
		Audience:  jwt.ClaimStrings{i.audience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
		ID:        jti,
	}
}

func (i *Issuer) sign(c Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(i.secret)
}

// Parse memvalidasi token: algoritma dikunci ke HS256 (menolak "none",
// HS512, RS256, dll.), exp wajib ada, iss dan aud harus cocok, dan jenis
// token harus sesuai dengan yang diharapkan.
func (i *Issuer) Parse(token string, want TokenUse) (*Claims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(i.issuer),
		jwt.WithAudience(i.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(5*time.Second),
		jwt.WithTimeFunc(i.now),
	)
	var c Claims
	_, err := parser.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) {
		return i.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if c.TokenUse != want {
		return nil, ErrWrongTokenUse
	}
	if _, err := c.UserID(); err != nil || c.SID == "" || c.ID == "" {
		return nil, fmt.Errorf("claim token tidak lengkap")
	}
	return &c, nil
}

// randomID menghasilkan identifier acak 128-bit untuk jti dan session id.
func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand tidak pernah gagal di Linux modern
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
