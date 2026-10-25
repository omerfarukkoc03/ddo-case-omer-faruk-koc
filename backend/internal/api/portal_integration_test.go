//go:build integration

package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"patiklinik/internal/apiclient"
)

func decodeID(r apiclient.Response, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	if r.Status != http.StatusCreated {
		return 0, fmt.Errorf("unexpected %s", r)
	}
	var v struct{ ID int64 }
	return v.ID, r.JSON(&v)
}

func TestOwnerPortalInviteAndActivation(t *testing.T) {
	rec := as(t, receptionEmail)
	ownerID, err := decodeID(rec.Post(ctx(), "/api/owners", map[string]any{"full_name": "Portal Testi", "phone": "05447778899"}))
	if err != nil {
		t.Fatal(err)
	}
	animalID, err := decodeID(rec.Post(ctx(), "/api/animals", map[string]any{"name": "Portakal", "species": "cat", "birth_date": "2025-01-10",
		"owners": []map[string]any{{"owner_id": ownerID}}}))
	if err != nil {
		t.Fatal(err)
	}
	invPath := fmt.Sprintf("/api/owners/%d/portal-invite", ownerID)
	mustStatus(t, ignoreErr(as(t, vetEmail).Post(ctx(), invPath, map[string]string{"email": "portal@example.com"})), nil, http.StatusForbidden)

	key := apiclient.WithIdempotencyKey("davet-" + apiclient.NewKey())
	r := mustStatus(t, ignoreErr(rec.Post(ctx(), invPath, map[string]string{"email": "Portal@Example.com"}, key)), nil, http.StatusCreated)
	var inv struct {
		Code string `json:"code"`
	}
	_ = r.JSON(&inv)
	if len(inv.Code) != 9 || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected invite: %s", r)
	}
	// Kod düz metin olarak hiçbir yerde saklanmaz: ne idempotency önbelleğinde ne denetim kaydında.
	if n := scalar[int](t, `SELECT count(*) FROM idempotency_keys WHERE position($1::bytea in coalesce(response_body, ''::bytea)) > 0`, []byte(inv.Code)); n != 0 {
		t.Fatal("invite code leaked into idempotency cache")
	}
	if n := scalar[int](t, `SELECT count(*) FROM audit_log WHERE strpos(coalesce(new_value::text, ''), $1) > 0`, inv.Code); n != 0 {
		t.Fatal("invite code leaked into audit log")
	}

	anon := apiclient.New(server.URL)
	activate := func(email, code, pw string) apiclient.Response {
		return ignoreErr(anon.Post(ctx(), "/api/auth/activate", map[string]string{"email": email, "code": code, "password": pw}))
	}
	if r := activate("portal@example.com", inv.Code, "kisa"); r.ErrorCode() != "validation_failed" {
		t.Fatalf("weak password must be rejected: %s", r)
	}
	if r := activate("baska@example.com", inv.Code, "Guclu12345"); r.ErrorCode() != "invalid_invite" {
		t.Fatalf("e-mail mismatch must fail: %s", r)
	}
	if r := activate("portal@example.com", "AAAA-BBBB", "Guclu12345"); r.ErrorCode() != "invalid_invite" {
		t.Fatalf("wrong code must fail: %s", r)
	}
	// Kod büyük/küçük harf ve tireden bağımsız kabul edilir.
	r = mustStatus(t, activate("portal@example.com", strings.ToLower(strings.ReplaceAll(inv.Code, "-", "")), "Guclu12345"), nil, http.StatusCreated)
	var tok struct {
		AccessToken string `json:"access_token"`
		User        struct {
			Role    string `json:"role"`
			OwnerID int64  `json:"owner_id"`
		} `json:"user"`
	}
	_ = r.JSON(&tok)
	if tok.User.Role != "owner" || tok.User.OwnerID != ownerID {
		t.Fatalf("account not linked to owner: %+v", tok.User)
	}
	if r := activate("portal@example.com", inv.Code, "Guclu12345"); r.ErrorCode() != "invalid_invite" {
		t.Fatalf("code must be single-use: %s", r)
	}

	newOwner := apiclient.New(server.URL)
	newOwner.Token = tok.AccessToken
	var list struct {
		Items []struct{ ID int64 } `json:"items"`
	}
	_ = mustStatus(t, ignoreErr(newOwner.Get(ctx(), "/api/animals")), nil, http.StatusOK).JSON(&list)
	if len(list.Items) != 1 || list.Items[0].ID != animalID {
		t.Fatalf("new owner should see exactly their own animal, got %+v", list.Items)
	}
	mustStatus(t, ignoreErr(newOwner.Get(ctx(), "/api/animals/2")), nil, http.StatusForbidden)
	if _, err := apiclient.New(server.URL).As(ctx(), "portal@example.com", "Guclu12345"); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if r := ignoreErr(rec.Post(ctx(), invPath, map[string]string{"email": "yeni@example.com"})); r.ErrorCode() != "portal_account_exists" {
		t.Fatalf("second account for the same owner: %s", r)
	}

	// Yeni davet eskisini iptal eder; süresi dolmuş davet kullanılamaz.
	o2, err := decodeID(rec.Post(ctx(), "/api/owners", map[string]any{"full_name": "Süresi Dolan", "phone": "05447778800"}))
	if err != nil {
		t.Fatal(err)
	}
	p2 := fmt.Sprintf("/api/owners/%d/portal-invite", o2)
	var first, second struct {
		Code string `json:"code"`
	}
	_ = mustStatus(t, ignoreErr(rec.Post(ctx(), p2, map[string]string{"email": "gec@example.com"})), nil, http.StatusCreated).JSON(&first)
	_ = mustStatus(t, ignoreErr(rec.Post(ctx(), p2, map[string]string{"email": "gec@example.com"})), nil, http.StatusCreated).JSON(&second)
	if r := activate("gec@example.com", first.Code, "Guclu12345"); r.ErrorCode() != "invalid_invite" {
		t.Fatalf("revoked invite must fail: %s", r)
	}
	if _, err := pool.Exec(ctx(), `
		UPDATE portal_invites SET created_at = created_at - interval '3 days', expires_at = created_at - interval '2 days'
		WHERE owner_id=$1 AND revoked_at IS NULL`, o2); err != nil {
		t.Fatal(err)
	}
	if r := activate("gec@example.com", second.Code, "Guclu12345"); r.ErrorCode() != "invalid_invite" {
		t.Fatalf("expired invite must fail: %s", r)
	}
}
