// Package audit kritik işlemleri append-only audit_log tablosuna yazar.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"patiklinik/internal/auth"
	"patiklinik/internal/httpx"
)

type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type Entry struct {
	Action   string
	Entity   string
	EntityID any
	Old      any
	New      any
}

// Write kaydı çağıranın transaction'ı içinde yazar; böylece işlem geri alınırsa denetim kaydı da geri alınır.
func Write(ctx context.Context, db Execer, e Entry) error {
	var actorID *int64
	var actorRole *string
	if p, ok := auth.FromContext(ctx); ok {
		actorID = &p.UserID
		r := string(p.Role)
		actorRole = &r
	}
	oldJSON, err := marshal(e.Old)
	if err != nil {
		return err
	}
	newJSON, err := marshal(e.New)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO audit_log (actor_id, actor_role, action, entity, entity_id, old_value, new_value, request_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		actorID, actorRole, e.Action, e.Entity, fmt.Sprint(e.EntityID), oldJSON, newJSON, nullable(httpx.RequestID(ctx)))
	return err
}

func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
