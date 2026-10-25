package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"patiklinik/internal/httpx"
)

// Davet kodu alfabesi karışabilecek karakterleri (I/1, O/0) içermez: 32^8 ≈ 1,1 × 10^12 olasılık.
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// InviteTTL portal davet kodunun geçerlilik süresidir.
const InviteTTL = 48 * time.Hour

// NewInviteCode XXXX-XXXX biçiminde kriptografik olarak rastgele bir kod üretir.
func NewInviteCode() (string, error) {
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if i == 4 {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteAlphabet))))
		if err != nil {
			return "", err
		}
		b.WriteByte(inviteAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// HashInviteCode kodu büyük harf, tire ve boşluksuz biçime getirip SHA-256 özetini döndürür; veritabanında yalnız özet tutulur.
func HashInviteCode(code string) []byte {
	norm := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte(norm))
	return sum[:]
}

// ValidatePassword parola politikasıdır: en az 8 karakter, en az bir harf ve bir rakam.
func ValidatePassword(pw string) error {
	var letter, digit bool
	for _, r := range pw {
		letter = letter || unicode.IsLetter(r)
		digit = digit || unicode.IsDigit(r)
	}
	if len([]rune(pw)) < 8 || !letter || !digit {
		return httpx.Validation{"password": "at least 8 characters with a letter and a digit"}.Err() //nolint:gosec // alan adı ve kural metni, kimlik bilgisi değil
	}
	return nil
}

var errInvalidInvite = httpx.NewError(http.StatusUnauthorized, "invalid_invite", "activation code is invalid, expired or does not match the e-mail")

// Activate davet koduyla sahip portal hesabını oluşturur ve oturum açar. Davet satırı kilitlenir; aynı kodla
// eşzamanlı iki etkinleştirmeden yalnız biri başarılı olur. Hangi bilginin yanlış olduğu söylenmez.
func (s *Service) Activate(ctx context.Context, email, code, password string) (Tokens, error) {
	if err := ValidatePassword(password); err != nil {
		return Tokens{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Tokens{}, err
	}
	var out Tokens
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			inviteID, ownerID int64
			inviteEmail       string
			expiresAt         time.Time
			usedAt, revokedAt *time.Time
			ownerName         string
		)
		err := tx.QueryRow(ctx, `
			SELECT i.id, i.owner_id, i.email, i.expires_at, i.used_at, i.revoked_at, o.full_name
			FROM portal_invites i JOIN owners o ON o.id = i.owner_id
			WHERE i.code_hash = $1 FOR UPDATE OF i`, HashInviteCode(code)).
			Scan(&inviteID, &ownerID, &inviteEmail, &expiresAt, &usedAt, &revokedAt, &ownerName)
		if errors.Is(err, pgx.ErrNoRows) {
			return errInvalidInvite
		}
		if err != nil {
			return err
		}
		if usedAt != nil || revokedAt != nil || s.now().After(expiresAt) || !strings.EqualFold(strings.TrimSpace(email), inviteEmail) {
			return errInvalidInvite
		}
		var uid int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, full_name, role, owner_id) VALUES ($1,$2,$3,'owner',$4) RETURNING id`,
			inviteEmail, hash, ownerName, ownerID).Scan(&uid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE portal_invites SET used_at = now() WHERE id = $1`, inviteID); err != nil {
			return err
		}
		p := Principal{UserID: uid, Role: RoleOwner, OwnerID: &ownerID, Name: ownerName}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_log (actor_id, actor_role, action, entity, entity_id, new_value)
			VALUES ($1, 'owner', 'owner.portal_activate', 'owner', $2, jsonb_build_object('user_id', $1::bigint, 'invite_id', $3::bigint))`,
			uid, strconv.FormatInt(ownerID, 10), inviteID); err != nil {
			return err
		}
		out, err = s.issue(ctx, tx, p, uuid.New())
		return err
	})
	return out, err
}
