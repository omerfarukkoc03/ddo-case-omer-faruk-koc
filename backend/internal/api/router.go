// Package api HTTP uç noktalarını tanımlar. Her uç noktada kimlik doğrulama ve rol kontrolü router seviyesinde,
// sahip (owner) rolü için satır seviyesinde erişim kontrolü handler içinde yapılır.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"patiklinik/internal/auth"
	"patiklinik/internal/httpx"
	"patiklinik/internal/vaccination"
)

type Deps struct {
	Pool           *pgxpool.Pool
	Auth           *auth.Service
	Vaccination    *vaccination.Service
	Now            func() time.Time
	Loc            *time.Location
	RequestTimeout time.Duration
	Version        string
}

type API struct {
	Deps
}

func roles(rs ...auth.Role) func(http.Handler) http.Handler { return auth.Require(rs...) }

var (
	staff        = auth.Staff
	everyone     = append([]auth.Role{auth.RoleOwner}, auth.Staff...)
	clinical     = []auth.Role{auth.RoleVet, auth.RoleTechnician}
	frontDesk    = []auth.Role{auth.RoleReception}
	vetOnly      = []auth.Role{auth.RoleVet}
	pharmacy     = []auth.Role{auth.RolePharmacy}
	vetPharmacy  = []auth.Role{auth.RoleVet, auth.RolePharmacy}
	vetReception = []auth.Role{auth.RoleVet, auth.RoleReception}
)

func NewRouter(d Deps) http.Handler {
	a := &API{Deps: d}
	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware, httpx.Recoverer, httpx.Logger, httpx.CORS, httpx.Timeout(d.RequestTimeout))

	r.Get("/health", a.health)
	r.Get("/openapi.yaml", serveOpenAPI)
	r.Get("/docs", serveSwaggerUI)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", httpx.H(a.login))
		r.Post("/auth/refresh", httpx.H(a.refresh))
		r.Post("/auth/logout", httpx.H(a.logout))

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.Authenticate)
			r.Use(httpx.Idempotency(d.Pool, func(ctx context.Context) (int64, bool) {
				p, ok := auth.FromContext(ctx)
				return p.UserID, ok
			}))
			r.With(roles(everyone...)).Get("/auth/me", httpx.H(a.me))

			// F-1 sahip ve hayvan kaydı
			r.With(roles(staff...)).Get("/owners", httpx.H(a.listOwners))
			r.With(roles(frontDesk...)).Post("/owners", httpx.H(a.createOwner))
			r.With(roles(everyone...)).Get("/owners/{id}", httpx.H(a.getOwner))
			r.With(roles(everyone...)).Get("/animals", httpx.H(a.listAnimals))
			r.With(roles(frontDesk...)).Post("/animals", httpx.H(a.createAnimal))
			r.With(roles(everyone...)).Get("/animals/{id}", httpx.H(a.getAnimal))
			r.With(roles(frontDesk...)).Post("/animals/{id}/owners", httpx.H(a.addAnimalOwner))
			r.With(roles(frontDesk...)).Put("/animals/{id}/billing-owner", httpx.H(a.setBillingOwner))
			r.With(roles(everyone...)).Get("/animals/{id}/weights", httpx.H(a.listWeights))
			r.With(roles(clinical...)).Post("/animals/{id}/weights", httpx.H(a.addWeight))

			// F-3 / T-1 aşı takvimi
			r.With(roles(everyone...)).Get("/animals/{id}/vaccinations", httpx.H(a.getVaccinations))
			r.With(roles(staff...)).Post("/animals/{id}/vaccinations/replan", httpx.H(a.replanVaccinations))
			r.With(roles(clinical...)).Post("/vaccine-doses/{id}/administer", httpx.H(a.administerDose))
			r.With(roles(vetReception...)).Post("/jobs/reminders/run", httpx.H(a.runReminders))
			r.With(roles(everyone...)).Get("/reminders", httpx.H(a.listReminders))

			// F-2 / T-3 randevu, muayene, acil vaka
			r.With(roles(staff...)).Get("/resources", httpx.H(a.listResources))
			r.With(roles(everyone...)).Get("/appointments", httpx.H(a.listAppointments))
			r.With(roles(frontDesk...)).Post("/appointments", httpx.H(a.createAppointment))
			r.With(roles(vetReception...)).Post("/appointments/emergency", httpx.H(a.createEmergency))
			r.With(roles(staff...)).Post("/appointments/{id}/status", httpx.H(a.setAppointmentStatus))
			r.With(roles(vetOnly...)).Post("/examinations", httpx.H(a.createExamination))
			r.With(roles(everyone...)).Get("/animals/{id}/examinations", httpx.H(a.listExaminations))

			// F-4 / T-2 ilaç, reçete, stok
			r.With(roles(staff...)).Get("/drugs", httpx.H(a.listDrugs))
			r.With(roles(vetOnly...)).Post("/dosage/calculate", httpx.H(a.calculateDose))
			r.With(roles(vetOnly...)).Post("/prescriptions", httpx.H(a.createPrescription))
			r.With(roles(vetPharmacy...)).Get("/prescriptions", httpx.H(a.listPrescriptions))
			r.With(roles(everyone...)).Get("/prescriptions/{id}", httpx.H(a.getPrescription))
			r.With(roles(pharmacy...)).Post("/prescriptions/{id}/dispense", httpx.H(a.dispensePrescription))
			r.With(roles(vetPharmacy...)).Get("/stock", httpx.H(a.listStock))
			r.With(roles(pharmacy...)).Post("/stock/batches", httpx.H(a.receiveStock))

			// F-5 yatan hasta ve kafes
			r.With(roles(staff...)).Get("/cages", httpx.H(a.listCages))
			r.With(roles(clinical...)).Post("/cages/{id}/transition", httpx.H(a.transitionCage))
			r.With(roles(staff...)).Get("/hospitalizations", httpx.H(a.listHospitalizations))
			r.With(roles(vetOnly...)).Post("/hospitalizations", httpx.H(a.admit))
			r.With(roles(vetOnly...)).Post("/hospitalizations/{id}/discharge", httpx.H(a.discharge))
			r.With(roles(clinical...)).Post("/hospitalizations/{id}/notes", httpx.H(a.addHospitalNote))

			// F-6 / T-4 fatura, tahsilat, paket
			r.With(roles(everyone...)).Get("/invoices", httpx.H(a.listInvoices))
			r.With(roles(frontDesk...)).Post("/invoices", httpx.H(a.createInvoice))
			r.With(roles(everyone...)).Get("/invoices/{id}", httpx.H(a.getInvoice))
			r.With(roles(frontDesk...)).Post("/invoices/{id}/payments", httpx.H(a.payInvoice))
			r.With(roles(staff...)).Get("/packages", httpx.H(a.listPackages))
			r.With(roles(frontDesk...)).Post("/package-purchases", httpx.H(a.purchasePackage))
			r.With(roles(everyone...)).Get("/animals/{id}/packages", httpx.H(a.listAnimalPackages))

			// T-4 mükerrer kayıt birleştirme
			r.With(roles(frontDesk...)).Get("/duplicates/{entity}", httpx.H(a.listDuplicates))
			r.With(roles(frontDesk...)).Get("/merges", httpx.H(a.listMerges))
			r.With(roles(frontDesk...)).Post("/merges", httpx.H(a.merge))
			r.With(roles(frontDesk...)).Post("/merges/{id}/undo", httpx.H(a.undoMerge))

			// Gözlem
			r.With(roles(vetReception...)).Get("/audit", httpx.H(a.listAudit))
			r.With(roles(staff...)).Get("/stats/live", httpx.H(a.liveStats))
		})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.NewError(http.StatusNotFound, "not_found", "route not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.NewError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed"))
	})
	return r
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status, code := "ok", http.StatusOK
	dbStatus := "ok"
	if err := a.Pool.Ping(ctx); err != nil {
		slog.WarnContext(ctx, "health: database ping failed", "err", err.Error())
		status, dbStatus, code = "degraded", "unreachable", http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, code, map[string]any{
		"status":   status,
		"database": dbStatus,
		"version":  a.Version,
		"time":     a.Now().UTC().Format(time.RFC3339),
	})
}

func id(r *http.Request) (int64, error) { return httpx.PathID(r, "id", chi.URLParam) }
