package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"patiklinik/internal/httpx"
)

type AuditRow struct {
	ID        int64           `json:"id"`
	At        time.Time       `json:"at"`
	ActorID   *int64          `json:"actor_id"`
	ActorRole *string         `json:"actor_role"`
	Action    string          `json:"action"`
	Entity    string          `json:"entity"`
	EntityID  string          `json:"entity_id"`
	OldValue  json.RawMessage `json:"old_value"`
	NewValue  json.RawMessage `json:"new_value"`
}

func scanAudit(row pgx.CollectableRow) (AuditRow, error) {
	var x AuditRow
	return x, row.Scan(&x.ID, &x.At, &x.ActorID, &x.ActorRole, &x.Action, &x.Entity, &x.EntityID, &x.OldValue, &x.NewValue)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) error {
	page, err := httpx.ParsePage(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	rows, err := a.Pool.Query(r.Context(), `
		SELECT id, at, actor_id, actor_role, action, entity, entity_id, old_value, new_value FROM audit_log
		WHERE ($1 = '' OR entity = $1) AND ($2 = '' OR entity_id = $2) AND ($3 = 0 OR id < $3)
		ORDER BY id DESC LIMIT $4`, q.Get("entity"), q.Get("entity_id"), page.After, page.Limit+1)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, scanAudit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, page.Limit, func(x AuditRow) int64 { return x.ID }))
	return nil
}

// liveStats arayüzdeki canlı pano içindir: simülatörün ürettiği olaylar burada akar.
func (a *API) liveStats(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	now := a.Now()
	dayStart, dayEnd, _ := a.dayBounds(now.In(a.Loc).Format(time.DateOnly))
	var s struct {
		Animals           int64            `json:"animals"`
		Owners            int64            `json:"owners"`
		AppointmentsToday int64            `json:"appointments_today"`
		EmergenciesToday  int64            `json:"emergencies_today"`
		ActiveInpatients  int64            `json:"active_inpatients"`
		OpenInvoices      int64            `json:"open_invoices"`
		OpenBalanceKurus  int64            `json:"open_balance_kurus"`
		PlannedDosesWeek  int64            `json:"vaccine_doses_due_7d"`
		PendingDuplicates int64            `json:"pending_duplicate_owners"`
		EventsLastMinute  int64            `json:"events_last_minute"`
		LastEventAt       *time.Time       `json:"last_event_at"`
		Cages             map[string]int64 `json:"cages"`
		Recent            []AuditRow       `json:"recent_events"`
		ServerTime        time.Time        `json:"server_time"`
	}
	s.ServerTime = now
	// Büyük tablolarda tam sayım yerine planlayıcı tahmini kullanılabilirdi; 15 bin satırda count(*) milisaniyeler sürer.
	err := a.Pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM animals WHERE merged_into IS NULL),
		       (SELECT count(*) FROM owners WHERE merged_into IS NULL),
		       (SELECT count(*) FROM appointments WHERE starts_at >= $1 AND starts_at < $2 AND status <> 'cancelled'),
		       (SELECT count(*) FROM appointments WHERE starts_at >= $1 AND starts_at < $2 AND priority = 'emergency'),
		       (SELECT count(*) FROM hospitalizations WHERE status = 'active'),
		       (SELECT count(*) FROM invoices WHERE status = 'open'),
		       (SELECT coalesce(sum(total_kurus - paid_kurus), 0) FROM invoices WHERE status = 'open'),
		       (SELECT count(*) FROM vaccine_doses WHERE status = 'planned' AND due_date BETWEEN $3::date AND $3::date + 7),
		       (SELECT count(*) FROM owners o1 JOIN owners o2 ON o2.phone = o1.phone AND o2.id > o1.id
		         WHERE o1.merged_into IS NULL AND o2.merged_into IS NULL),
		       (SELECT count(*) FROM audit_log WHERE at > now() - interval '1 minute'),
		       (SELECT max(at) FROM audit_log)`,
		dayStart, dayEnd, a.Vaccination.Today()).
		Scan(&s.Animals, &s.Owners, &s.AppointmentsToday, &s.EmergenciesToday, &s.ActiveInpatients, &s.OpenInvoices,
			&s.OpenBalanceKurus, &s.PlannedDosesWeek, &s.PendingDuplicates, &s.EventsLastMinute, &s.LastEventAt)
	if err != nil {
		return err
	}
	s.Cages = map[string]int64{"empty": 0, "occupied": 0, "cleaning": 0, "ready": 0}
	rows, err := a.Pool.Query(ctx, `SELECT state, count(*) FROM cages GROUP BY state`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var st string
		var n int64
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return err
		}
		s.Cages[st] = n
	}
	rows.Close()
	arows, err := a.Pool.Query(ctx, `
		SELECT id, at, actor_id, actor_role, action, entity, entity_id, NULL::jsonb, NULL::jsonb FROM audit_log ORDER BY id DESC LIMIT 15`)
	if err != nil {
		return err
	}
	if s.Recent, err = pgx.CollectRows(arows, scanAudit); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

//go:embed openapi.yaml
var openapiSpec []byte

// OpenAPISpec gömülü OpenAPI belgesini döndürür (testler rol matrisini buradan okur).
func OpenAPISpec() []byte { return openapiSpec }

func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openapiSpec)
}

const swaggerHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>PatiKlinik API</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css"></head>
<body><div id="ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url: '/openapi.yaml', dom_id: '#ui', persistAuthorization: true});</script>
</body></html>`

func serveSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerHTML))
}
