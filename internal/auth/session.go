package auth

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRefreshInvalid = errors.New("refresh token tidak valid")
	// ErrRefreshReused berarti refresh token yang sudah dirotasi dipakai lagi.
	// Ini indikasi token dicuri, sehingga semua sesi user dicabut.
	ErrRefreshReused = errors.New("refresh token dipakai ulang")
)

// SessionStore menyimpan state sesi di Redis.
//
// Struktur key:
//
//	sess:{sid}          -> user_id   sesi aktif; dicek pada setiap request
//	rt:{jti}            -> sid       refresh token yang masih berlaku
//	rt_used:{jti}       -> sid       refresh token yang sudah dirotasi (deteksi reuse)
//	user_sess:{user_id} -> set(sid)  semua sesi milik user (untuk cabut semua)
type SessionStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewSessionStore(rdb *redis.Client, refreshTTL time.Duration) *SessionStore {
	return &SessionStore{rdb: rdb, ttl: refreshTTL}
}

func sessKey(sid string) string       { return "sess:" + sid }
func rtKey(jti string) string         { return "rt:" + jti }
func rtUsedKey(jti string) string     { return "rt_used:" + jti }
func userSessKey(userID int64) string { return "user_sess:" + strconv.FormatInt(userID, 10) }

// Create mendaftarkan sesi baru beserta refresh token pertamanya.
func (s *SessionStore) Create(ctx context.Context, userID int64, sid, jti string) error {
	_, err := s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, sessKey(sid), userID, s.ttl)
		p.Set(ctx, rtKey(jti), sid, s.ttl)
		p.SAdd(ctx, userSessKey(userID), sid)
		p.Expire(ctx, userSessKey(userID), s.ttl)
		return nil
	})
	return err
}

// Active melaporkan apakah sesi belum di-logout atau dicabut.
func (s *SessionStore) Active(ctx context.Context, sid string) (bool, error) {
	n, err := s.rdb.Exists(ctx, sessKey(sid)).Result()
	return n == 1, err
}

// rotateScript menjalankan rotasi secara atomik di Redis, sehingga dua
// request yang memakai refresh token yang sama tidak bisa sama-sama berhasil.
var rotateScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if not cur then
  if redis.call('EXISTS', KEYS[2]) == 1 then return 'reuse' end
  return 'invalid'
end
if cur ~= ARGV[1] or redis.call('EXISTS', KEYS[4]) == 0 then
  return 'invalid'
end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[1], 'EX', ARGV[2])
redis.call('SET', KEYS[3], ARGV[1], 'EX', ARGV[2])
redis.call('EXPIRE', KEYS[4], ARGV[2])
redis.call('EXPIRE', KEYS[5], ARGV[2])
return 'ok'
`)

// Rotate menukar refresh token oldJTI dengan newJTI pada sesi sid.
// Jika oldJTI sudah pernah dirotasi, mengembalikan ErrRefreshReused.
func (s *SessionStore) Rotate(ctx context.Context, userID int64, sid, oldJTI, newJTI string) error {
	keys := []string{rtKey(oldJTI), rtUsedKey(oldJTI), rtKey(newJTI), sessKey(sid), userSessKey(userID)}
	res, err := rotateScript.Run(ctx, s.rdb, keys, sid, int64(s.ttl/time.Second)).Text()
	if err != nil {
		return err
	}
	switch res {
	case "ok":
		return nil
	case "reuse":
		return ErrRefreshReused
	default:
		return ErrRefreshInvalid
	}
}

// Revoke menghapus satu sesi (logout). Refresh token milik sesi itu otomatis
// tidak berlaku karena Rotate mensyaratkan sess:{sid} masih ada.
func (s *SessionStore) Revoke(ctx context.Context, userID int64, sid string) error {
	_, err := s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Del(ctx, sessKey(sid))
		p.SRem(ctx, userSessKey(userID), sid)
		return nil
	})
	return err
}

// RevokeAll mencabut seluruh sesi milik user.
func (s *SessionStore) RevokeAll(ctx context.Context, userID int64) error {
	sids, err := s.rdb.SMembers(ctx, userSessKey(userID)).Result()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(sids)+1)
	for _, sid := range sids {
		keys = append(keys, sessKey(sid))
	}
	keys = append(keys, userSessKey(userID))
	return s.rdb.Del(ctx, keys...).Err()
}
