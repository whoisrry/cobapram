// Package models berisi struct GORM yang memetakan tabel database.
// Skema sebenarnya dikelola oleh migrasi goose di folder migrations/,
// bukan oleh AutoMigrate.
package models

import "time"

type Role string

const (
	RoleClient Role = "client"
	RoleAdmin  Role = "admin"
)

func (r Role) Valid() bool { return r == RoleClient || r == RoleAdmin }

type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusClosed     Status = "closed"
)

// Statuses berurutan untuk ditampilkan di filter dan form.
var Statuses = []Status{StatusOpen, StatusInProgress, StatusClosed}

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusInProgress || s == StatusClosed
}

func (s Status) Label() string {
	switch s {
	case StatusOpen:
		return "Terbuka"
	case StatusInProgress:
		return "Diproses"
	case StatusClosed:
		return "Ditutup"
	}
	return string(s)
}

type User struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash string
	Role         Role
	CreatedAt    time.Time
}

type Ticket struct {
	ID        int64
	UserID    int64
	User      User // pemilik tiket (client)
	Subject   string
	Body      string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
	Replies   []Reply
}

type Reply struct {
	ID        int64
	TicketID  int64
	UserID    int64
	User      User // penulis balasan
	Body      string
	CreatedAt time.Time
}
