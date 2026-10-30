package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/whoisrry/cobapram/internal/models"
)

var testSecret = []byte("test-secret-yang-panjangnya-lebih-dari-32-byte")

func newTestIssuer() *Issuer {
	return NewIssuer(testSecret, "https://tiket.test", "ticketing-web", 15*time.Minute, 7*24*time.Hour)
}

var testUser = models.User{ID: 42, Name: "Budi", Role: models.RoleClient}

func TestIssueAndParse(t *testing.T) {
	iss := newTestIssuer()
	tok, err := iss.Issue(testUser, "sid-1")
	if err != nil {
		t.Fatal(err)
	}

	c, err := iss.Parse(tok.Access, UseAccess)
	if err != nil {
		t.Fatalf("access token valid ditolak: %v", err)
	}
	if uid, _ := c.UserID(); uid != 42 || c.Role != models.RoleClient || c.SID != "sid-1" || c.Name != "Budi" {
		t.Fatalf("claims tidak sesuai: %+v", c)
	}

	rc, err := iss.Parse(tok.Refresh, UseRefresh)
	if err != nil {
		t.Fatalf("refresh token valid ditolak: %v", err)
	}
	if rc.ID != tok.RefreshJTI {
		t.Fatalf("jti refresh = %q, want %q", rc.ID, tok.RefreshJTI)
	}
}

func TestParseRejectsWrongTokenUse(t *testing.T) {
	iss := newTestIssuer()
	tok, _ := iss.Issue(testUser, "sid-1")

	if _, err := iss.Parse(tok.Refresh, UseAccess); !errors.Is(err, ErrWrongTokenUse) {
		t.Fatalf("refresh token sebagai access: err = %v, want ErrWrongTokenUse", err)
	}
	if _, err := iss.Parse(tok.Access, UseRefresh); !errors.Is(err, ErrWrongTokenUse) {
		t.Fatalf("access token sebagai refresh: err = %v, want ErrWrongTokenUse", err)
	}
}

func TestParseRejectsExpired(t *testing.T) {
	iss := newTestIssuer()
	issuedAt := time.Now()
	iss.now = func() time.Time { return issuedAt }
	tok, _ := iss.Issue(testUser, "sid-1")

	iss.now = func() time.Time { return issuedAt.Add(16 * time.Minute) }
	if _, err := iss.Parse(tok.Access, UseAccess); !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

// signRaw membuat token dengan claims dan metode bebas untuk menguji penolakan.
func signRaw(t *testing.T, method jwt.SigningMethod, key any, mutate func(*Claims)) string {
	t.Helper()
	now := time.Now()
	c := Claims{
		Role: models.RoleAdmin, SID: "sid-x", TokenUse: UseAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://tiket.test",
			Subject:   "1",
			Audience:  jwt.ClaimStrings{"ticketing-web"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
			ID:        "jti-x",
		},
	}
	if mutate != nil {
		mutate(&c)
	}
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseRejectsInvalidTokens(t *testing.T) {
	iss := newTestIssuer()

	cases := map[string]string{
		"alg none":            signRaw(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, nil),
		"alg HS512":           signRaw(t, jwt.SigningMethodHS512, testSecret, nil),
		"secret lain":         signRaw(t, jwt.SigningMethodHS256, []byte("secret-lain-yang-juga-panjang-sekali-123"), nil),
		"issuer salah":        signRaw(t, jwt.SigningMethodHS256, testSecret, func(c *Claims) { c.Issuer = "https://jahat.test" }),
		"audience salah":      signRaw(t, jwt.SigningMethodHS256, testSecret, func(c *Claims) { c.Audience = jwt.ClaimStrings{"lain"} }),
		"tanpa exp":           signRaw(t, jwt.SigningMethodHS256, testSecret, func(c *Claims) { c.ExpiresAt = nil }),
		"tanpa sid":           signRaw(t, jwt.SigningMethodHS256, testSecret, func(c *Claims) { c.SID = "" }),
		"subject bukan angka": signRaw(t, jwt.SigningMethodHS256, testSecret, func(c *Claims) { c.Subject = "admin" }),
		"bukan jwt":           "abc.def.ghi",
	}
	// Pastikan token kontrol (tanpa mutasi) memang lolos, agar kasus di atas
	// gagal karena mutasinya, bukan karena helper salah.
	if _, err := iss.Parse(signRaw(t, jwt.SigningMethodHS256, testSecret, nil), UseAccess); err != nil {
		t.Fatalf("token kontrol ditolak: %v", err)
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := iss.Parse(tok, UseAccess); err == nil {
				t.Fatal("token tidak valid diterima")
			}
		})
	}
}
