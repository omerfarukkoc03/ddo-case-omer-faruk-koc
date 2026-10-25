package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"patiklinik/internal/audit"
	"patiklinik/internal/auth"
	"patiklinik/internal/httpx"
)

type inviteReq struct {
	Email string `json:"email"`
}

type inviteResp struct {
	OwnerID   int64     `json:"owner_id"`
	Email     string    `json:"email"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// createPortalInvite kimliği klinikte doğrulanan sahip için tek kullanımlık, 48 saat geçerli bir etkinleştirme kodu
// üretir. Önceki kullanılmamış davet iptal edilir. Kod yalnız bu yanıtta görünür: veritabanında özeti tutulur,
// denetim kaydına ve idempotency önbelleğine yazılmaz (Cache-Control: no-store).
func (a *API) createPortalInvite(w http.ResponseWriter, r *http.Request) error {
	oid, err := id(r)
	if err != nil {
		return err
	}
	var req inviteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	email := strings.ToLower(trim(req.Email))
	if !strings.Contains(email, "@") || len(email) > 200 {
		return httpx.Validation{"email": "valid e-mail required"}.Err()
	}
	code, err := auth.NewInviteCode()
	if err != nil {
		return err
	}
	expires := a.Now().Add(auth.InviteTTL)
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var merged *int64
		if err := tx.QueryRow(r.Context(), `SELECT merged_into FROM owners WHERE id=$1 FOR UPDATE`, oid).Scan(&merged); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.NotFound("owner")
			}
			return err
		}
		if merged != nil {
			return httpx.Conflict("already_merged", "owner record was merged into another record")
		}
		var hasAccount, emailTaken bool
		if err := tx.QueryRow(r.Context(), `
			SELECT EXISTS (SELECT 1 FROM users WHERE owner_id=$1), EXISTS (SELECT 1 FROM users WHERE lower(email)=$2)`,
			oid, email).Scan(&hasAccount, &emailTaken); err != nil {
			return err
		}
		if hasAccount {
			return httpx.Conflict("portal_account_exists", "this owner already has a portal account")
		}
		if emailTaken {
			return httpx.Conflict("email_taken", "this e-mail is already used by another account")
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE portal_invites SET revoked_at = now() WHERE owner_id=$1 AND used_at IS NULL AND revoked_at IS NULL`, oid); err != nil {
			return err
		}
		var inviteID int64
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO portal_invites (owner_id, email, code_hash, expires_at, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			oid, email, auth.HashInviteCode(code), expires, principal(r).UserID).Scan(&inviteID); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "owner.portal_invite", Entity: "owner", EntityID: oid,
			New: map[string]any{"invite_id": inviteID, "email": email, "expires_at": expires}})
	})
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, inviteResp{OwnerID: oid, Email: email, Code: code, ExpiresAt: expires})
	return nil
}

type activateReq struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

// activate sahibin davet koduyla hesabını etkinleştirmesidir; kimlik doğrulama gerektirmez, başarılıysa oturum açar.
func (a *API) activate(w http.ResponseWriter, r *http.Request) error {
	var req activateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	if trim(req.Email) == "" {
		v.Add("email", "required")
	}
	if trim(req.Code) == "" {
		v.Add("code", "required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	tokens, err := a.Auth.Activate(r.Context(), req.Email, req.Code, req.Password)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, tokens)
	return nil
}
