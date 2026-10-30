// Package templates memuat template html/template dan file statis yang
// di-embed ke dalam binary.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"
)

//go:embed *.html static
var files embed.FS

var pages = []string{"login.html", "tickets.html", "ticket_new.html", "ticket_show.html", "error.html"}

type Views struct {
	pages map[string]*template.Template
}

var jakarta = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*60*60)
	}
	return loc
}()

var funcs = template.FuncMap{
	"datetime": func(t time.Time) string { return t.In(jakarta).Format("02 Jan 2006 15:04") },
}

// Load mem-parse setiap halaman bersama layout.html. Setiap halaman diparse
// terpisah agar blok "content" tidak saling menimpa.
func Load() (*Views, error) {
	v := &Views{pages: make(map[string]*template.Template, len(pages))}
	for _, p := range pages {
		t, err := template.New(p).Funcs(funcs).ParseFS(files, "layout.html", p)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", p, err)
		}
		v.pages[p] = t
	}
	return v, nil
}

// Render menulis ke buffer terlebih dahulu, supaya error template tidak
// menghasilkan halaman setengah jadi dengan status 200.
func (v *Views) Render(w http.ResponseWriter, status int, page string, data any) error {
	t, ok := v.pages[page]
	if !ok {
		return fmt.Errorf("template %s tidak ada", page)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// Static mengembalikan isi folder static/.
func Static() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
