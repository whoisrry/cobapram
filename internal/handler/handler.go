// Package handler berisi handler HTTP dan definisi route.
package handler

import (
	"context"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/csrf"
	"gorm.io/gorm"

	"github.com/whoisrry/cobapram/internal/auth"
	"github.com/whoisrry/cobapram/internal/config"
	mw "github.com/whoisrry/cobapram/internal/middleware"
	"github.com/whoisrry/cobapram/internal/models"
	"github.com/whoisrry/cobapram/internal/templates"
	"github.com/whoisrry/cobapram/internal/ticket"
)

type Handler struct {
	cfg     config.Config
	db      *gorm.DB
	auth    *auth.Service
	limiter *auth.LoginLimiter
	views   *templates.Views
	log     *slog.Logger
}

func New(cfg config.Config, db *gorm.DB, authSvc *auth.Service, limiter *auth.LoginLimiter, views *templates.Views, log *slog.Logger) *Handler {
	return &Handler{cfg: cfg, db: db, auth: authSvc, limiter: limiter, views: views, log: log}
}

// Routes memakai routing bawaan Go 1.22+ (method + path parameter).
func (h *Handler) Routes() http.Handler {
	authn := &mw.Authenticator{Auth: h.auth, CookieSecure: h.cfg.CookieSecure, Log: h.log}
	authed := func(f http.HandlerFunc) http.Handler { return authn.RequireAuth(f) }
	clientOnly := func(f http.HandlerFunc) http.Handler { return authn.RequireAuth(mw.RequireRole(models.RoleClient, f)) }
	adminOnly := func(f http.HandlerFunc) http.Handler { return authn.RequireAuth(mw.RequireRole(models.RoleAdmin, f)) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(templates.Static())))

	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.Handle("POST /logout", authed(h.logout))

	mux.Handle("GET /{$}", http.RedirectHandler("/tickets", http.StatusSeeOther))
	mux.Handle("GET /tickets", authed(h.listTickets))
	mux.Handle("GET /tickets/new", clientOnly(h.newTicketForm))
	mux.Handle("POST /tickets", clientOnly(h.createTicket))
	mux.Handle("GET /tickets/{id}", authed(h.showTicket))
	mux.Handle("POST /tickets/{id}/replies", authed(h.createReply))
	mux.Handle("POST /tickets/{id}/status", adminOnly(h.updateStatus))

	// gorilla/csrf melindungi semua method non-GET/HEAD/OPTIONS/TRACE.
	protect := csrf.Protect(h.cfg.CSRFKey,
		csrf.Secure(h.cfg.CookieSecure),
		csrf.HttpOnly(true),
		csrf.Path("/"),
		csrf.SameSite(csrf.SameSiteLaxMode),
		csrf.CookieName("csrf"),
		csrf.FieldName("csrf_token"),
		csrf.ErrorHandler(http.HandlerFunc(h.csrfFailed)),
	)

	return mw.Chain(mux,
		mw.Recover(h.log),
		mw.Logger(h.log),
		mw.SecurityHeaders(h.cfg.IsProduction()),
		mw.MaxBody(64<<10),
		h.markPlaintext,
		protect,
	)
}

// markPlaintext: gorilla/csrf v1.7.3 mengasumsikan HTTPS dan menolak Referer
// http://. Saat development tanpa TLS (COOKIE_SECURE=false), request ditandai
// sebagai plaintext. Di produksi aplikasi selalu di belakang Caddy (HTTPS).
func (h *Handler) markPlaintext(next http.Handler) http.Handler {
	if h.cfg.CookieSecure {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, csrf.PlaintextHTTPRequest(r))
	})
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) csrfFailed(w http.ResponseWriter, r *http.Request) {
	h.log.Warn("csrf ditolak", "path", r.URL.Path, "reason", csrf.FailureReason(r))
	h.renderError(w, r, http.StatusForbidden, "Permintaan ditolak",
		"Formulir sudah kedaluwarsa atau tidak valid. Muat ulang halaman lalu coba lagi.")
}

// viewData adalah data untuk semua template. Satu struct sederhana dipakai
// bersama karena jumlah halamannya sedikit.
type viewData struct {
	Title   string
	User    *auth.Principal
	CSRF    template.HTML
	Error   string
	Message string

	Form     formValues
	Tickets  []models.Ticket
	Ticket   *models.Ticket
	Filter   ticket.ListFilter
	Statuses []models.Status
	PrevURL  string
	NextURL  string
}

type formValues struct {
	Email   string
	Subject string
	Body    string
}

func (h *Handler) newView(r *http.Request, title string) viewData {
	v := viewData{Title: title, CSRF: csrf.TemplateField(r), Statuses: models.Statuses}
	if p, ok := mw.CurrentUser(r.Context()); ok {
		v.User = &p
	}
	return v
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, page string, v viewData) {
	if err := h.views.Render(w, status, page, v); err != nil {
		h.serverError(w, r, err)
	}
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	v := h.newView(r, title)
	v.Message = msg
	if err := h.views.Render(w, status, "error.html", v); err != nil {
		h.log.Error("render halaman error", "error", err)
		http.Error(w, msg, status)
	}
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusNotFound, "Tidak ditemukan", "Halaman atau tiket yang Anda cari tidak ditemukan.")
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.log.Error("internal error", "error", err, "method", r.Method, "path", r.URL.Path)
	h.renderError(w, r, http.StatusInternalServerError, "Terjadi kesalahan", "Terjadi kesalahan pada server. Silakan coba lagi.")
}

// queryCtx memberi batas waktu untuk query database dalam satu handler.
func (h *Handler) queryCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), h.cfg.DB.QueryTimeout)
}

func viewer(r *http.Request) ticket.Viewer {
	p, _ := mw.CurrentUser(r.Context())
	return ticket.Viewer{UserID: p.UserID, Role: p.Role}
}

// clientIP mengambil IP asli client. X-Forwarded-For hanya dipercaya jika
// TRUST_PROXY_HEADERS=true (aplikasi hanya bisa dijangkau lewat Caddy).
// Caddy menimpa header ini dengan IP koneksi yang sebenarnya, dan kita
// mengambil entri paling kanan (ditambahkan oleh proxy terdekat).
func (h *Handler) clientIP(r *http.Request) string {
	if h.cfg.TrustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func retryMinutes(d time.Duration) int {
	m := int((d + time.Minute - 1) / time.Minute)
	if m < 1 {
		m = 1
	}
	return m
}
