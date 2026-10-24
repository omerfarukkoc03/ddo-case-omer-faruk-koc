package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Idempotency, Idempotency-Key başlığıyla gelen durum değiştiren istekleri bir kez işler.
//
//   - Aynı kullanıcı + anahtar + aynı gövde → ilk yanıt birebir tekrar döner (Idempotent-Replayed: true).
//   - Aynı anahtar farklı gövde/yol ile → 422 idempotency_key_reused.
//   - İlk istek hâlâ işleniyorsa → 409 idempotency_in_progress.
//   - 5xx yanıtlar saklanmaz; istemci aynı anahtarla güvenle yeniden deneyebilir.
//
// userOf kimliği doğrulanmış kullanıcının kimliğini döndürür (auth paketine döngüsel bağımlılık olmasın diye fonksiyon).
func Idempotency(pool *pgxpool.Pool, userOf func(context.Context) (int64, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" || r.Method == http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}
			if len(key) < 8 || len(key) > 128 {
				WriteError(w, r, BadRequest("Idempotency-Key must be 8-128 characters"))
				return
			}
			uid, ok := userOf(r.Context())
			if !ok {
				WriteError(w, r, Unauthorized("authentication required"))
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
			if err != nil {
				WriteError(w, r, BadRequest("cannot read body"))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			h := sha256.New()
			h.Write([]byte(r.Method + " " + r.URL.Path + "\n"))
			h.Write(body)
			hash := h.Sum(nil)

			ctx := r.Context()
			tag, err := pool.Exec(ctx, `
				INSERT INTO idempotency_keys (user_id, key, method, path, request_hash)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, uid, key, r.Method, r.URL.Path, hash)
			if err != nil {
				WriteError(w, r, err)
				return
			}
			if tag.RowsAffected() == 0 {
				replay(w, r, pool, uid, key, hash)
				return
			}

			rec := &bufferingWriter{header: http.Header{}}
			next.ServeHTTP(rec, r)
			status := rec.statusCode()

			// Yanıtı saklama işi istek bağlamından bağımsızdır: istemci bağlantıyı kesse de sonuç kaydedilmeli.
			storeCtx := context.WithoutCancel(ctx)
			if status >= 500 {
				_, _ = pool.Exec(storeCtx, `DELETE FROM idempotency_keys WHERE user_id=$1 AND key=$2`, uid, key)
			} else {
				_, _ = pool.Exec(storeCtx, `UPDATE idempotency_keys SET status_code=$3, response_body=$4 WHERE user_id=$1 AND key=$2`,
					uid, key, status, rec.body.Bytes())
			}
			rec.flushTo(w)
		})
	}
}

func replay(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, uid int64, key string, hash []byte) {
	var (
		storedHash []byte
		status     *int
		body       []byte
	)
	err := pool.QueryRow(r.Context(),
		`SELECT request_hash, status_code, response_body FROM idempotency_keys WHERE user_id=$1 AND key=$2`,
		uid, key).Scan(&storedHash, &status, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		WriteError(w, r, Conflict("idempotency_in_progress", "a request with this Idempotency-Key is being processed, retry shortly"))
		return
	}
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if !bytes.Equal(storedHash, hash) {
		WriteError(w, r, Unprocessable("idempotency_key_reused", "Idempotency-Key was already used with a different request"))
		return
	}
	if status == nil {
		WriteError(w, r, Conflict("idempotency_in_progress", "a request with this Idempotency-Key is being processed, retry shortly"))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(*status)
	_, _ = w.Write(body)
}

type bufferingWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferingWriter) Header() http.Header         { return b.header }
func (b *bufferingWriter) WriteHeader(code int)        { b.status = code }
func (b *bufferingWriter) Write(p []byte) (int, error) { return b.body.Write(p) }

func (b *bufferingWriter) statusCode() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

func (b *bufferingWriter) flushTo(w http.ResponseWriter) {
	for k, v := range b.header {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Length", strconv.Itoa(b.body.Len()))
	w.WriteHeader(b.statusCode())
	_, _ = w.Write(b.body.Bytes())
}
