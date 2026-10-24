package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"patiklinik/internal/auth"
	"patiklinik/internal/httpx"
)

// DB hem *pgxpool.Pool hem pgx.Tx tarafından karşılanır.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func principal(r *http.Request) auth.Principal { return auth.MustPrincipal(r.Context()) }

// ensureAnimalAccess sahip rolündeki kullanıcının yalnız kendi hayvanına erişmesini sağlar; personel her hayvana erişir.
// Hayvan yoksa 404 döner.
func ensureAnimalAccess(ctx context.Context, db DB, p auth.Principal, animalID int64) error {
	var exists, linked bool
	var ownerID int64
	if p.OwnerID != nil {
		ownerID = *p.OwnerID
	}
	err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM animals WHERE id = $1),
		       EXISTS (SELECT 1 FROM animal_owners WHERE animal_id = $1 AND owner_id = $2)`,
		animalID, ownerID).Scan(&exists, &linked)
	if err != nil {
		return err
	}
	if !exists {
		return httpx.NotFound("animal")
	}
	if p.Role == auth.RoleOwner && !linked {
		return httpx.Forbidden("you can only access your own animals")
	}
	return nil
}

func ensureOwnerAccess(p auth.Principal, ownerID int64) error {
	if p.Role == auth.RoleOwner && (p.OwnerID == nil || *p.OwnerID != ownerID) {
		return httpx.Forbidden("you can only access your own record")
	}
	return nil
}

func isOwner(p auth.Principal) bool { return p.Role == auth.RoleOwner }

func ownerIDOf(p auth.Principal) int64 {
	if p.OwnerID == nil {
		return 0
	}
	return *p.OwnerID
}

func trim(s string) string { return strings.TrimSpace(s) }

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}
