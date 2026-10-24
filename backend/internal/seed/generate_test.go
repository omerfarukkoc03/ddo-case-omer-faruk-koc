package seed

import (
	"testing"
	"time"
)

var ist, _ = time.LoadLocation("Europe/Istanbul")

var anchor = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

func small() Scale { return Scale{Owners: 300, Animals: 450, DupOwners: 10, DupAnimals: 8} }

func TestGenerateIsDeterministic(t *testing.T) {
	a := Generate(42, anchor, ist, small())
	b := Generate(42, anchor, ist, small())
	if a.Hash() != b.Hash() {
		t.Fatal("same seed produced different datasets")
	}
	c := Generate(43, anchor, ist, small())
	if a.Hash() == c.Hash() {
		t.Fatal("different seeds produced identical datasets")
	}
}

func TestGenerateScaleAndInvariants(t *testing.T) {
	ds := Generate(7, anchor, ist, small())
	counts := ds.Counts()
	if counts["animals"] != 450 || counts["owners"] != 310 {
		t.Fatalf("unexpected counts: %v", counts)
	}
	// Mikroçipler tekil olmalı (aktif kayıtlar için veritabanında UNIQUE).
	seen := map[string]bool{}
	for _, row := range ds.byName["animals"].Rows {
		if chip, ok := row[5].(string); ok {
			if seen[chip] {
				t.Fatalf("duplicate microchip %s", chip)
			}
			seen[chip] = true
		}
	}
	// Seed randevuları kaynak bazında çakışmamalı (EXCLUDE kısıtı yüklemeyi reddederdi).
	type iv struct{ s, e time.Time }
	busy := map[int64][]iv{}
	for _, row := range ds.byName["appointments"].Rows {
		if row[8] == "cancelled" {
			continue
		}
		s, e := row[6].(time.Time), row[7].(time.Time)
		for _, res := range []int64{row[2].(int64), row[3].(int64)} {
			for _, o := range busy[res] {
				if s.Before(o.e) && o.s.Before(e) {
					t.Fatalf("overlapping seed appointments on resource %d", res)
				}
			}
			busy[res] = append(busy[res], iv{s, e})
		}
	}
	// Hiçbir fatura fazla tahsil edilmemiş olmalı.
	for _, row := range ds.byName["invoices"].Rows {
		if row[6].(int64) > row[5].(int64) {
			t.Fatalf("invoice %v overpaid", row[0])
		}
	}
}
