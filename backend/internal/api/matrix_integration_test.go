//go:build integration

package api_test

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v3"

	"patiklinik/internal/api"
	"patiklinik/internal/apiclient"
)

// Bu dosyadaki testler tüm uç noktaları tüm rollerle tarar. Beklenen roller OpenAPI belgesindeki "Roller:" satırından
// okunur; böylece hem her uç noktada sunucu tarafı yetki kontrolü olduğu hem de belgenin doğru olduğu kanıtlanır.

var roleUsers = map[string]string{
	"owner":      ownerEmail,
	"vet":        vetEmail,
	"technician": techEmail,
	"reception":  receptionEmail,
	"pharmacy":   pharmacyEmail,
}

var allRoles = []string{"owner", "vet", "technician", "reception", "pharmacy"}

type route struct{ method, path string }

func servedRoutes(t *testing.T) []route {
	t.Helper()
	var out []route
	err := chi.Walk(api.NewRouter(api.Deps{}).(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(path, "/api/") {
			out = append(out, route{method, strings.TrimSuffix(path, "/")})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path+out[i].method < out[j].path+out[j].method })
	return out
}

var rolesLine = regexp.MustCompile(`Roller:\s*([^.\n]+)`)

// documentedRoles route → izinli roller; "tümü" tüm rollerdir. Kimlik doğrulaması gerektirmeyen rotalar (security: [])
// için nil döner.
func documentedRoles(t *testing.T) map[route][]string {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]struct {
			Description string `yaml:"description"`
			Security    *[]any `yaml:"security"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(api.OpenAPISpec(), &spec); err != nil {
		t.Fatal(err)
	}
	out := map[route][]string{}
	for path, ops := range spec.Paths {
		for method, op := range ops {
			r := route{strings.ToUpper(method), path}
			if op.Security != nil && len(*op.Security) == 0 {
				out[r] = nil
				continue
			}
			m := rolesLine.FindStringSubmatch(op.Description)
			if m == nil {
				t.Errorf("%s %s: belgede 'Roller:' satırı yok", r.method, r.path)
				continue
			}
			text := strings.TrimSpace(m[1])
			if strings.HasPrefix(text, "tümü") {
				out[r] = allRoles
				continue
			}
			var roles []string
			for _, w := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '(' }) {
				if _, ok := roleUsers[w]; ok {
					roles = append(roles, w)
				}
			}
			out[r] = roles
		}
	}
	return out
}

func concrete(path string) string {
	return strings.NewReplacer("{id}", "1", "{entity}", "owners").Replace(path)
}

const roleForbidden = "is not allowed to access this resource"

func TestEveryEndpointEnforcesDocumentedRoles(t *testing.T) {
	clients := map[string]*apiclient.Client{}
	for role, email := range roleUsers {
		clients[role] = as(t, email)
	}
	docs := documentedRoles(t)
	checked := 0
	for _, r := range servedRoutes(t) {
		allowed, ok := docs[r]
		if !ok {
			t.Errorf("%s %s belgelenmemiş", r.method, r.path)
			continue
		}
		if allowed == nil {
			continue // kimlik doğrulaması gerektirmeyen uç nokta
		}
		// Token'sız istek her korumalı uç noktada 401 almalı.
		if resp := ignoreErr(apiclient.New(server.URL).Do(ctx(), r.method, concrete(r.path), map[string]any{})); resp.Status != http.StatusUnauthorized {
			t.Errorf("%s %s token'sız: %d, beklenen 401", r.method, r.path, resp.Status)
		}
		for _, role := range allRoles {
			// Durum değiştiren isteklerde gövde boş: izinli rol doğrulama/bulunamadı hatası alır, veri değişmez.
			resp := ignoreErr(clients[role].Do(ctx(), r.method, concrete(r.path), map[string]any{}))
			isAllowed := false
			for _, a := range allowed {
				isAllowed = isAllowed || a == role
			}
			deniedByRole := resp.Status == http.StatusForbidden && strings.Contains(string(resp.Body), roleForbidden)
			switch {
			case resp.Status >= 500:
				t.Errorf("%s %s rol=%s: sunucu hatası %s", r.method, r.path, role, resp)
			case isAllowed && deniedByRole:
				t.Errorf("%s %s rol=%s: belgede izinli ama reddedildi", r.method, r.path, role)
			case !isAllowed && !deniedByRole:
				t.Errorf("%s %s rol=%s: belgede izinsiz ama %d döndü", r.method, r.path, role, resp.Status)
			}
			checked++
		}
	}
	if checked < 250 {
		t.Fatalf("yalnız %d (rota × rol) denendi", checked)
	}
	t.Logf("%d (rota × rol) birleşimi denendi", checked)
}

// TestMalformedInputNeverCausesServerError her uç noktaya izinli bir rolle bozuk JSON, yanlış tipler, aşırı uzun
// ve sınır dışı değerler, var olmayan kimlikler gönderir; hiçbiri 5xx üretmemeli ve tüm hatalar ortak biçimde olmalı.
func TestMalformedInputNeverCausesServerError(t *testing.T) {
	clients := map[string]*apiclient.Client{}
	for role, email := range roleUsers {
		clients[role] = as(t, email)
	}
	docs := documentedRoles(t)
	long := strings.Repeat("ç", 5000)
	bodies := []any{
		"{bozuk json",
		[]int{1, 2, 3},
		map[string]any{"animal_id": "bir", "owner_id": -5, "amount_kurus": 1e30, "quantity": -1, "weight_kg": "abc"},
		map[string]any{"name": long, "full_name": long, "phone": long, "email": long, "reason": long, "findings": long},
		map[string]any{"starts_at": "dün", "birth_date": "2099-13-45", "duration_min": 99999, "doses_per_day": 0, "mg_per_kg": "-1"},
		map[string]any{"bilinmeyen_alan": true},
	}
	paths := map[string][]string{}
	for _, r := range servedRoutes(t) {
		allowed := docs[r]
		var c *apiclient.Client
		if len(allowed) > 0 {
			c = clients[allowed[0]]
		} else {
			c = apiclient.New(server.URL)
		}
		targets := []string{concrete(r.path)}
		if strings.Contains(r.path, "{id}") {
			targets = append(targets,
				strings.ReplaceAll(r.path, "{id}", "999999999"),
				strings.ReplaceAll(r.path, "{id}", "abc"),
				strings.ReplaceAll(r.path, "{id}", "-1"))
		}
		for _, p := range targets {
			for _, b := range bodies {
				var resp apiclient.Response
				if s, ok := b.(string); ok {
					resp = rawPost(t, c, r.method, p, s)
				} else {
					resp = ignoreErr(c.Do(ctx(), r.method, p, b))
				}
				if resp.Status >= 500 {
					t.Errorf("%s %s: %s", r.method, p, resp)
				}
				if resp.Status >= 400 && resp.ErrorCode() == "" {
					t.Errorf("%s %s: hata ortak biçimde değil: %s", r.method, p, resp)
				}
				paths[r.method+" "+r.path] = append(paths[r.method+" "+r.path], fmt.Sprint(resp.Status))
			}
		}
	}
	t.Logf("%d uç nokta bozuk girdilerle denendi", len(paths))
}

func rawPost(t *testing.T, c *apiclient.Client, method, path, body string) apiclient.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx(), method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return apiclient.Response{Status: resp.StatusCode, Body: b, Header: resp.Header}
}
