package auth

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// LoginLimiter membatasi percobaan login per IP dengan fixed window di Redis.
// Setiap percobaan dihitung (berhasil maupun gagal), jadi serangan paralel
// tetap terhitung secara atomik oleh INCR.
type LoginLimiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

func NewLoginLimiter(rdb *redis.Client, limit int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{rdb: rdb, limit: limit, window: window}
}

// Allow mencatat satu percobaan dan melaporkan apakah masih di bawah batas.
// Jika tidak, retryAfter berisi sisa waktu sampai window berakhir.
func (l *LoginLimiter) Allow(ctx context.Context, ip string) (allowed bool, retryAfter time.Duration, err error) {
	key := "rl:login:" + ip
	var incr *redis.IntCmd
	var ttl *redis.DurationCmd
	_, err = l.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		// SET NX EX membuat key beserta TTL hanya pada percobaan pertama;
		// INCR berikutnya mempertahankan TTL tersebut.
		p.SetNX(ctx, key, 0, l.window)
		incr = p.Incr(ctx, key)
		ttl = p.TTL(ctx, key)
		return nil
	})
	if err != nil {
		return false, 0, err
	}
	if incr.Val() > int64(l.limit) {
		return false, ttl.Val(), nil
	}
	return true, 0, nil
}
