package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"patiklinik/internal/apiclient"
	"patiklinik/internal/config"
	"patiklinik/internal/seed"
)

// live API üzerinden gerçek kullanıcı gibi olay üretir; böylece arayüzde sistemin canlı olduğu görülür ve
// olaylar aynı iş kurallarından (yetki, çakışma, idempotans, denetim) geçer.
func live(ctx context.Context, apiURL string, seedValue int64, loc *time.Location) error {
	base := apiclient.New(apiURL)
	if err := base.WaitHealthy(ctx, 2*time.Minute); err != nil {
		return err
	}
	reception, err := base.As(ctx, "resepsiyon@patiklinik.test", seed.DemoPassword)
	if err != nil {
		return err
	}
	vet, err := base.As(ctx, "vet2@patiklinik.test", seed.DemoPassword)
	if err != nil {
		return err
	}
	interval := time.Duration(config.EnvInt("SIM_INTERVAL_MS", 4000)) * time.Millisecond
	rng := rand.New(rand.NewPCG(uint64(seedValue)+1, 99)) //nolint:gosec // tohumun bit deseni olduğu gibi kullanılır
	maxAnimal := int64(config.EnvInt("SIM_ANIMALS", seed.DefaultScale().Animals))
	slog.Info("live simulation started", "interval", interval.String())

	tick := time.NewTicker(interval)
	defer tick.Stop()
	relogin := time.NewTicker(10 * time.Minute) // access token 15 dk geçerli
	defer relogin.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-relogin.C:
			if r, err := base.As(ctx, "resepsiyon@patiklinik.test", seed.DemoPassword); err == nil {
				reception = r
			}
			if v, err := base.As(ctx, "vet2@patiklinik.test", seed.DemoPassword); err == nil {
				vet = v
			}
		case <-tick.C:
			completeFinishedEmergencies(ctx, vet, loc)
			animal := 5 + rng.Int64N(maxAnimal-200)
			x := rng.Float64()
			var action string
			var resp apiclient.Response
			var err error
			key := apiclient.WithIdempotencyKey(apiclient.NewKey())
			switch {
			case x < 0.45:
				action = "appointment"
				day := time.Now().In(loc).AddDate(0, 0, 1+rng.IntN(7))
				start := time.Date(day.Year(), day.Month(), day.Day(), 9+rng.IntN(9), 30*rng.IntN(2), 0, 0, loc)
				v := int64(1 + rng.IntN(3))
				resp, err = reception.Post(ctx, "/api/appointments", map[string]any{
					"animal_id": animal, "vet_id": v, "room_id": v + 3, "kind": "exam", "starts_at": start, "duration_min": 30,
					"reason": "Simülatör: kontrol muayenesi"}, key)
			case x < 0.48:
				action = "emergency"
				resp, err = vet.Post(ctx, "/api/appointments/emergency", map[string]any{
					"animal_id": animal, "duration_min": 30, "reason": "Simülatör: acil vaka"}, key)
			case x < 0.72:
				action = "payment"
				resp, err = payOpenInvoice(ctx, reception, rng)
			case x < 0.95:
				action = "weight"
				resp, err = vet.Post(ctx, fmt.Sprintf("/api/animals/%d/weights", animal), map[string]any{"weight_kg": fmt.Sprintf("%.1f", 3+rng.Float64()*25)}, key)
			default:
				action = "reminder_job"
				resp, err = reception.Post(ctx, "/api/jobs/reminders/run", nil, key)
			}
			if err != nil {
				slog.Warn("sim action failed", "action", action, "err", err.Error())
				continue
			}
			if resp.Status == http.StatusUnauthorized {
				relogin.Reset(time.Millisecond)
			}
			slog.Info("sim action", "action", action, "status", resp.Status, "code", resp.ErrorCode())
		}
	}
}

func payOpenInvoice(ctx context.Context, c *apiclient.Client, rng *rand.Rand) (apiclient.Response, error) {
	resp, err := c.Get(ctx, "/api/invoices?status=open&limit=20")
	if err != nil || resp.Status != http.StatusOK {
		return resp, err
	}
	var list struct {
		Items []struct {
			ID       int64 `json:"id"`
			DueKurus int64 `json:"due_kurus"`
		} `json:"items"`
	}
	if err := resp.JSON(&list); err != nil || len(list.Items) == 0 {
		return resp, err
	}
	inv := list.Items[rng.IntN(len(list.Items))]
	return c.Post(ctx, fmt.Sprintf("/api/invoices/%d/payments", inv.ID), map[string]any{"amount_kurus": inv.DueKurus, "method": "card"},
		apiclient.WithIdempotencyKey(apiclient.NewKey()))
}

// completeFinishedEmergencies süresi dolmuş acil vakaları tamamlar; gerçek klinikte veterinerin muayeneyi kapatmasıdır.
// Bu yapılmazsa simülatörün açtığı acil vakalar kaynakları sonsuza dek meşgul ederdi.
func completeFinishedEmergencies(ctx context.Context, c *apiclient.Client, loc *time.Location) {
	resp, err := c.Get(ctx, "/api/appointments?date="+time.Now().In(loc).Format(time.DateOnly))
	if err != nil || resp.Status != http.StatusOK {
		return
	}
	var list struct {
		Items []struct {
			ID       int64     `json:"id"`
			Priority string    `json:"priority"`
			Status   string    `json:"status"`
			EndsAt   time.Time `json:"ends_at"`
		} `json:"items"`
	}
	if resp.JSON(&list) != nil {
		return
	}
	for _, a := range list.Items {
		if a.Priority == "emergency" && a.Status == "checked_in" && a.EndsAt.Before(time.Now()) {
			_, _ = c.Post(ctx, fmt.Sprintf("/api/appointments/%d/status", a.ID), map[string]string{"status": "completed"})
		}
	}
}
