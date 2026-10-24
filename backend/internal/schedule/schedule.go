// Package schedule kaynak planlamalı randevu ve acil vaka yeniden planlama algoritmasıdır (T-3). Saf Go; veritabanı yok.
//
// Acil vaka "şimdi" başlar. Her (veteriner, oda) çifti aday olarak denenir: çifte çakışan taşınabilir randevular
// çıkarılır ve boş slotlara yeniden yerleştirilir. Bozulma maliyeti:
//
//	maliyet = Σ |kayma dakikası| (öne çekme 2 kat sayılır) + veteriner değişirse VetChangePenalty + oda değişirse RoomChangePenalty
//
// En düşük toplam maliyetli çift seçilir. Etkilenen randevu sayısı 5 veya daha azsa bütün yerleştirme sıraları denenir
// (permütasyon), fazlaysa başlangıç saatine göre açgözlü yerleştirme yapılır. Yerleştirme yalnız boş slotlara yapıldığı için
// zincirleme kaydırma olmaz. Başlamış (checked_in) ve başka acil randevular taşınamaz.
package schedule

import (
	"errors"
	"sort"
	"time"
)

type Booking struct {
	ID       int64
	VetID    int64
	RoomID   int64
	Start    time.Time
	End      time.Time
	Movable  bool
	RoomKind string
}

type Hours struct {
	Loc   *time.Location
	Open  time.Duration // gece yarısından itibaren, ör. 9h
	Close time.Duration // ör. 19h
}

type Config struct {
	Step              time.Duration
	HorizonDays       int
	VetChangePenalty  int
	RoomChangePenalty int
	Hours             Hours
}

func DefaultConfig(loc *time.Location) Config {
	return Config{Step: 5 * time.Minute, HorizonDays: 7, VetChangePenalty: 60, RoomChangePenalty: 5,
		Hours: Hours{Loc: loc, Open: 9 * time.Hour, Close: 19 * time.Hour}}
}

type Emergency struct {
	Duration time.Duration
	RoomKind string
	// PreferredVetID doluysa yalnız bu veteriner denenir.
	PreferredVetID int64
}

type Problem struct {
	Now       time.Time
	Emergency Emergency
	Vets      []int64
	// Rooms oda kimliği → oda türü (exam_room, surgery_room).
	Rooms    map[int64]string
	Bookings []Booking
}

type Move struct {
	BookingID int64     `json:"appointment_id"`
	FromStart time.Time `json:"from_start"`
	ToStart   time.Time `json:"to_start"`
	ToEnd     time.Time `json:"to_end"`
	FromVet   int64     `json:"from_vet_id"`
	ToVet     int64     `json:"to_vet_id"`
	FromRoom  int64     `json:"from_room_id"`
	ToRoom    int64     `json:"to_room_id"`
	Cost      int       `json:"cost"`
}

type Result struct {
	VetID     int64     `json:"vet_id"`
	RoomID    int64     `json:"room_id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Moves     []Move    `json:"moves"`
	Unplaced  []int64   `json:"unplaced"`
	TotalCost int       `json:"total_cost"`
	// WaitMinutes tüm çiftler doluyken acil vakanın ileri atıldığı süredir (normalde 0).
	WaitMinutes int `json:"wait_minutes"`
}

var ErrNoCapacity = errors.New("no vet/room pair becomes free for the emergency within the maximum wait")

type interval struct{ s, e time.Time }

type occupancy map[int64][]interval

func (o occupancy) free(res int64, s, e time.Time) bool {
	for _, iv := range o[res] {
		if s.Before(iv.e) && iv.s.Before(e) {
			return false
		}
	}
	return true
}

func (o occupancy) add(res int64, s, e time.Time) { o[res] = append(o[res], interval{s, e}) }

func (o occupancy) clone() occupancy {
	c := make(occupancy, len(o))
	for k, v := range o {
		c[k] = append([]interval(nil), v...)
	}
	return c
}

func ceilTo(t time.Time, step time.Duration) time.Time {
	r := t.Truncate(step)
	if r.Before(t) {
		r = r.Add(step)
	}
	return r
}

// MaxEmergencyWait tüm veteriner/oda çiftleri başlamış ya da başka acil randevularla doluyken acil vakanın en fazla
// ne kadar ileri atılabileceğidir.
const MaxEmergencyWait = 4 * time.Hour

// PlanEmergency acil vakayı yerleştirir ve çakışan randevuların yeni yerlerini hesaplar.
//
// Öncelik: önce en erken başlangıç, eşit başlangıçta en düşük bozulma. Acil vaka normalde "şimdi" başlar; tüm
// çiftler taşınamaz randevularla (başlamış muayene, başka acil vaka) doluysa ilk boşalan çifte, en fazla
// MaxEmergencyWait kadar ileriye planlanır. Bekleme Result.WaitMinutes ile bildirilir.
func PlanEmergency(p Problem, cfg Config) (Result, error) {
	earliest := ceilTo(p.Now, cfg.Step)
	for start := earliest; !start.After(earliest.Add(MaxEmergencyWait)); start = start.Add(cfg.Step) {
		if res, ok := planAt(p, cfg, start); ok {
			res.WaitMinutes = int(start.Sub(earliest).Minutes())
			return res, nil
		}
	}
	return Result{}, ErrNoCapacity
}

func planAt(p Problem, cfg Config, start time.Time) (Result, bool) {
	end := start.Add(p.Emergency.Duration)
	vets := append([]int64(nil), p.Vets...)
	sort.Slice(vets, func(i, j int) bool { return vets[i] < vets[j] })
	rooms := roomsOfKind(p.Rooms, p.Emergency.RoomKind)

	best := Result{TotalCost: -1}
	for _, vet := range vets {
		if p.Emergency.PreferredVetID != 0 && vet != p.Emergency.PreferredVetID {
			continue
		}
		for _, room := range rooms {
			var displaced []Booking
			blocked := false
			occ := occupancy{}
			for _, b := range p.Bookings {
				hits := (b.VetID == vet || b.RoomID == room) && start.Before(b.End) && b.Start.Before(end)
				switch {
				case hits && !b.Movable:
					blocked = true
				case hits:
					displaced = append(displaced, b)
				default:
					occ.add(b.VetID, b.Start, b.End)
					occ.add(b.RoomID, b.Start, b.End)
				}
			}
			if blocked {
				continue
			}
			occ.add(vet, start, end)
			occ.add(room, start, end)
			moves, unplaced, cost := replan(displaced, occ, vets, p.Rooms, start, cfg)
			// Yerleştirilemeyen randevu ağır cezalıdır: elle ele alınması gerekir.
			cost += len(unplaced) * 100000
			if best.TotalCost < 0 || cost < best.TotalCost {
				best = Result{VetID: vet, RoomID: room, Start: start, End: end, Moves: moves, Unplaced: unplaced, TotalCost: cost}
			}
		}
	}
	return best, best.TotalCost >= 0
}

func roomsOfKind(rooms map[int64]string, kind string) []int64 {
	var out []int64
	for id, k := range rooms {
		if k == kind {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

const exhaustiveLimit = 5

// replan çıkarılan randevuları en düşük toplam maliyetle yeniden yerleştirir.
func replan(displaced []Booking, occ occupancy, vets []int64, rooms map[int64]string, notBefore time.Time, cfg Config) ([]Move, []int64, int) {
	if len(displaced) == 0 {
		return nil, nil, 0
	}
	sort.Slice(displaced, func(i, j int) bool {
		if !displaced[i].Start.Equal(displaced[j].Start) {
			return displaced[i].Start.Before(displaced[j].Start)
		}
		return displaced[i].ID < displaced[j].ID
	})
	try := func(order []Booking) ([]Move, []int64, int) {
		o := occ.clone()
		var moves []Move
		var unplaced []int64
		total := 0
		for _, b := range order {
			m, ok := bestSlot(b, o, vets, rooms, notBefore, cfg)
			if !ok {
				unplaced = append(unplaced, b.ID)
				continue
			}
			o.add(m.ToVet, m.ToStart, m.ToEnd)
			o.add(m.ToRoom, m.ToStart, m.ToEnd)
			moves = append(moves, m)
			total += m.Cost
		}
		sort.Slice(moves, func(i, j int) bool { return moves[i].BookingID < moves[j].BookingID })
		return moves, unplaced, total
	}
	if len(displaced) > exhaustiveLimit {
		return try(displaced)
	}
	var bestMoves []Move
	var bestUnplaced []int64
	bestCost := -1
	permute(displaced, func(order []Booking) {
		m, u, c := try(order)
		c += len(u) * 100000
		if bestCost < 0 || c < bestCost {
			bestMoves, bestUnplaced, bestCost = m, u, c
		}
	})
	return bestMoves, bestUnplaced, bestCost - len(bestUnplaced)*100000
}

func permute(items []Booking, visit func([]Booking)) {
	a := append([]Booking(nil), items...)
	var rec func(k int)
	rec = func(k int) {
		if k == len(a) {
			visit(a)
			return
		}
		for i := k; i < len(a); i++ {
			a[k], a[i] = a[i], a[k]
			rec(k + 1)
			a[k], a[i] = a[i], a[k]
		}
	}
	rec(0)
}

// bestSlot bir randevu için en düşük maliyetli boş slotu bulur. Her (veteriner, oda) çifti için orijinal saatten
// dışa doğru taranır; bulunan en iyi maliyeti aşan arama dalları budanır.
func bestSlot(b Booking, occ occupancy, vets []int64, rooms map[int64]string, notBefore time.Time, cfg Config) (Move, bool) {
	dur := b.End.Sub(b.Start)
	kind := b.RoomKind
	if kind == "" {
		kind = rooms[b.RoomID]
	}
	candRooms := roomsOfKind(rooms, kind)
	// Önce orijinal veteriner/oda denenir ki eşit maliyette değişiklik yapılmasın.
	orderedVets := prefer(vets, b.VetID)
	orderedRooms := prefer(candRooms, b.RoomID)
	maxShift := time.Duration(cfg.HorizonDays) * 24 * time.Hour

	best := Move{Cost: -1}
	for _, vet := range orderedVets {
		for _, room := range orderedRooms {
			penalty := 0
			if vet != b.VetID {
				penalty += cfg.VetChangePenalty
			}
			if room != b.RoomID {
				penalty += cfg.RoomChangePenalty
			}
			if best.Cost >= 0 && penalty >= best.Cost {
				continue
			}
			for shift := time.Duration(0); shift <= maxShift; shift += cfg.Step {
				shiftCost := int(shift.Minutes())
				if best.Cost >= 0 && penalty+shiftCost >= best.Cost {
					break
				}
				for _, dir := range []int{1, -1} {
					if shift == 0 && dir == -1 {
						continue
					}
					s := b.Start.Add(time.Duration(dir) * shift)
					cost := penalty + shiftCost
					if dir == -1 {
						cost = penalty + 2*shiftCost
					}
					if best.Cost >= 0 && cost >= best.Cost {
						continue
					}
					e := s.Add(dur)
					if s.Before(notBefore) || !withinHours(s, e, cfg.Hours) || !occ.free(vet, s, e) || !occ.free(room, s, e) {
						continue
					}
					best = Move{BookingID: b.ID, FromStart: b.Start, ToStart: s, ToEnd: e, FromVet: b.VetID, ToVet: vet,
						FromRoom: b.RoomID, ToRoom: room, Cost: cost}
				}
			}
		}
	}
	return best, best.Cost >= 0
}

func prefer(ids []int64, first int64) []int64 {
	out := []int64{}
	for _, id := range ids {
		if id == first {
			out = append(out, id)
		}
	}
	for _, id := range ids {
		if id != first {
			out = append(out, id)
		}
	}
	return out
}

func withinHours(s, e time.Time, h Hours) bool {
	ls, le := s.In(h.Loc), e.In(h.Loc)
	y, m, d := ls.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, h.Loc)
	return !ls.Before(midnight.Add(h.Open)) && !le.After(midnight.Add(h.Close))
}

// SlotFree normal randevu oluştururken uygulama tarafı hızlı kontrol içindir; asıl garanti veritabanı EXCLUDE kısıtıdır.
func SlotFree(bookings []Booking, vet, room int64, s, e time.Time) bool {
	for _, b := range bookings {
		if (b.VetID == vet || b.RoomID == room) && s.Before(b.End) && b.Start.Before(e) {
			return false
		}
	}
	return true
}
