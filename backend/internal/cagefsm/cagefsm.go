// Package cagefsm kafes durum makinesidir: boş → dolu → temizlik → hazır → dolu.
// Aynı kural veritabanında cage_state_guard tetikleyicisiyle de uygulanır (savunma derinliği).
package cagefsm

import "fmt"

type State string

const (
	Empty    State = "empty"
	Occupied State = "occupied"
	Cleaning State = "cleaning"
	Ready    State = "ready"
)

var transitions = map[State][]State{
	Empty:    {Occupied},
	Occupied: {Cleaning},
	Cleaning: {Ready},
	Ready:    {Occupied},
}

func Valid(s State) bool {
	_, ok := transitions[s]
	return ok
}

// Allowed bir durumdan geçilebilecek durumları döndürür.
func Allowed(from State) []State { return append([]State(nil), transitions[from]...) }

// CanAccept kafesin yeni hasta alıp alamayacağını söyler.
func CanAccept(s State) bool { return s == Empty || s == Ready }

type TransitionError struct {
	From, To State
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid cage transition %s -> %s (allowed: %v)", e.From, e.To, transitions[e.From])
}

// Transition geçişi doğrular.
func Transition(from, to State) error {
	if !Valid(from) || !Valid(to) {
		return &TransitionError{From: from, To: to}
	}
	for _, s := range transitions[from] {
		if s == to {
			return nil
		}
	}
	return &TransitionError{From: from, To: to}
}
