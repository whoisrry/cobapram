package handler

import (
	"errors"
	"net/http"

	"github.com/whoisrry/cobapram/internal/models"
	"github.com/whoisrry/cobapram/internal/user"
)

func (h *Handler) newClientForm(w http.ResponseWriter, r *http.Request) {
	v := h.newView(r, "Tambah client")
	if r.URL.Query().Get("created") == "1" {
		v.Message = "Client berhasil dibuat. Berikan email dan password awal kepada client."
	}
	h.render(w, r, http.StatusOK, "client_new.html", v)
}

// createClient membuat akun baru dengan role client. Role selalu ditetapkan
// di server, sehingga form ini tidak bisa dipakai untuk membuat admin.
func (h *Handler) createClient(w http.ResponseWriter, r *http.Request) {
	v := h.newView(r, "Tambah client")
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Permintaan tidak valid", "Formulir tidak dapat dibaca.")
		return
	}
	v.Form.Name = r.PostForm.Get("name")
	v.Form.Email = r.PostForm.Get("email")
	password := r.PostForm.Get("password")

	if password != r.PostForm.Get("password_confirm") {
		v.Error = "Konfirmasi password tidak sama."
		h.render(w, r, http.StatusUnprocessableEntity, "client_new.html", v)
		return
	}

	ctx, cancel := h.queryCtx(r)
	defer cancel()
	u, err := user.Create(ctx, h.db, user.NewUser{
		Name: v.Form.Name, Email: v.Form.Email, Password: password, Role: models.RoleClient,
	})
	var inputErr *user.InputError
	if errors.As(err, &inputErr) {
		v.Error = inputErr.Msg
		h.render(w, r, http.StatusUnprocessableEntity, "client_new.html", v)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "client dibuat", "client_id", u.ID, "by", viewer(r).UserID)
	// Post/Redirect/Get: refresh halaman tidak mengirim ulang form.
	http.Redirect(w, r, "/admin/clients/new?created=1", http.StatusSeeOther)
}
