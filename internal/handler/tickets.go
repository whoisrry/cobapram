package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/whoisrry/cobapram/internal/models"
	"github.com/whoisrry/cobapram/internal/ticket"
)

const (
	maxSubject = 200
	maxBody    = 10000
	maxName    = 100
)

func (h *Handler) listTickets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := ticket.ListFilter{Page: 1}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 && n <= 10000 {
		f.Page = n
	}
	v := h.newView(r, "Daftar tiket")
	if v.User.IsAdmin() {
		if s := models.Status(q.Get("status")); s.Valid() {
			f.Status = s
		}
		f.ClientName = truncate(strings.TrimSpace(q.Get("client")), maxName)
	}

	ctx, cancel := h.queryCtx(r)
	defer cancel()
	tickets, hasNext, err := ticket.List(ctx, h.db, viewer(r), f)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	v.Tickets = tickets
	v.Filter = f
	if f.Page > 1 {
		v.PrevURL = pageURL(f, f.Page-1)
	}
	if hasNext {
		v.NextURL = pageURL(f, f.Page+1)
	}
	h.render(w, r, http.StatusOK, "tickets.html", v)
}

func pageURL(f ticket.ListFilter, page int) string {
	q := url.Values{"page": {strconv.Itoa(page)}}
	if f.Status != "" {
		q.Set("status", string(f.Status))
	}
	if f.ClientName != "" {
		q.Set("client", f.ClientName)
	}
	return "/tickets?" + q.Encode()
}

func (h *Handler) newTicketForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "ticket_new.html", h.newView(r, "Buat tiket"))
}

func (h *Handler) createTicket(w http.ResponseWriter, r *http.Request) {
	v := h.newView(r, "Buat tiket")
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Permintaan tidak valid", "Formulir tidak dapat dibaca.")
		return
	}
	v.Form.Subject = formText(r, "subject")
	v.Form.Body = formText(r, "body")

	if msg := validateText("Subjek", v.Form.Subject, maxSubject); msg != "" {
		v.Error = msg
	} else if msg := validateText("Deskripsi", v.Form.Body, maxBody); msg != "" {
		v.Error = msg
	}
	if v.Error != "" {
		h.render(w, r, http.StatusUnprocessableEntity, "ticket_new.html", v)
		return
	}

	ctx, cancel := h.queryCtx(r)
	defer cancel()
	t, err := ticket.Create(ctx, h.db, v.User.UserID, v.Form.Subject, v.Form.Body)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tickets/%d", t.ID), http.StatusSeeOther)
}

func (h *Handler) showTicket(w http.ResponseWriter, r *http.Request) {
	h.renderTicket(w, r, http.StatusOK, "", "")
}

// renderTicket menampilkan detail tiket; dipakai juga untuk menampilkan
// ulang form balasan yang gagal validasi.
func (h *Handler) renderTicket(w http.ResponseWriter, r *http.Request, status int, errMsg, replyBody string) {
	id, ok := ticketID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	ctx, cancel := h.queryCtx(r)
	defer cancel()
	t, err := ticket.Get(ctx, h.db, viewer(r), id)
	if errors.Is(err, ticket.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	v := h.newView(r, t.Subject)
	v.Ticket = &t
	v.Error = errMsg
	v.Form.Body = replyBody
	h.render(w, r, status, "ticket_show.html", v)
}

func (h *Handler) createReply(w http.ResponseWriter, r *http.Request) {
	id, ok := ticketID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Permintaan tidak valid", "Formulir tidak dapat dibaca.")
		return
	}
	body := formText(r, "body")
	if msg := validateText("Balasan", body, maxBody); msg != "" {
		h.renderTicket(w, r, http.StatusUnprocessableEntity, msg, body)
		return
	}

	ctx, cancel := h.queryCtx(r)
	defer cancel()
	_, err := ticket.AddReply(ctx, h.db, viewer(r), id, body)
	if errors.Is(err, ticket.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tickets/%d", id), http.StatusSeeOther)
}

func (h *Handler) updateStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := ticketID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Permintaan tidak valid", "Formulir tidak dapat dibaca.")
		return
	}
	status := models.Status(r.PostForm.Get("status"))
	if !status.Valid() {
		h.renderTicket(w, r, http.StatusUnprocessableEntity, "Status tidak valid.", "")
		return
	}

	ctx, cancel := h.queryCtx(r)
	defer cancel()
	err := ticket.UpdateStatus(ctx, h.db, viewer(r), id, status)
	if errors.Is(err, ticket.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.log.Info("status tiket diubah", "ticket_id", id, "status", status, "by", viewer(r).UserID)
	http.Redirect(w, r, fmt.Sprintf("/tickets/%d", id), http.StatusSeeOther)
}

func ticketID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// formText membaca field teks, menyeragamkan baris baru dari textarea (CRLF)
// menjadi LF, lalu membuang spasi di awal/akhir.
func formText(r *http.Request, key string) string {
	return strings.TrimSpace(strings.ReplaceAll(r.PostForm.Get(key), "\r\n", "\n"))
}

func validateText(field, s string, max int) string {
	if s == "" {
		return field + " wajib diisi."
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return field + " berisi karakter tidak valid."
	}
	if utf8.RuneCountInString(s) > max {
		return fmt.Sprintf("%s maksimal %d karakter.", field, max)
	}
	return ""
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
