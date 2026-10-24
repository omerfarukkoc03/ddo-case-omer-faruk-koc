package api

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v3"
)

// TestOpenAPICoversAllRoutes router'daki her uç noktanın openapi.yaml'da belgelendiğini ve belgede olmayan bir
// uç nokta kalmadığını doğrular; elle yazılan doküman koddan koparsa CI kırmızıya döner.
func TestOpenAPICoversAllRoutes(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openapiSpec, &spec); err != nil {
		t.Fatalf("openapi.yaml geçersiz: %v", err)
	}
	documented := map[string]bool{}
	for path, ops := range spec.Paths {
		for method := range ops {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}
	router, ok := NewRouter(Deps{}).(chi.Routes)
	if !ok {
		t.Fatal("router chi.Routes değil")
	}
	served := map[string]bool{}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/openapi.yaml" || route == "/docs" {
			return nil
		}
		served[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing, extra []string
	for r := range served {
		if !documented[r] {
			missing = append(missing, r)
		}
	}
	for r := range documented {
		if !served[r] {
			extra = append(extra, r)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("belgelenmemiş: %v\nkodda olmayan: %v", missing, extra)
	}
	if len(served) < 50 {
		t.Fatalf("yalnız %d uç nokta bulundu; router gezilemedi mi?", len(served))
	}
}
