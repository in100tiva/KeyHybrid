package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// Trava para as atualizações do PocketBase: o "Bearer kh-<alias>" do /v1 não pode virar 401.
func TestAuthorizationProprioNaoViraUnauthorized(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())

	status := func(liberar bool) int {
		r, err := apis.NewRouter(app)
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		if liberar {
			liberarAuthorizationProprio(r)
		}
		r.GET("/v1/teste", func(e *core.RequestEvent) error { return e.NoContent(http.StatusNoContent) })
		mux, err := r.BuildMux()
		if err != nil {
			t.Fatalf("BuildMux: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/teste", nil)
		req.Header.Set("Authorization", "Bearer kh-teste")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}

	// Sem a liberação, o PocketBase recusa: prova que o teste enxerga o middleware (se o id mudar, a linha abaixo falha)
	if got := status(false); got != http.StatusUnauthorized {
		t.Fatalf("sem liberar: status %d, esperado 401 (o PocketBase mudou o middleware? revise liberarAuthorizationProprio)", got)
	}
	if got := status(true); got != http.StatusNoContent {
		t.Fatalf("com liberar: status %d, esperado 204", got)
	}
}

// Painel /_/ e APIs nativas do PocketBase ficam fora do gateway (issue #46).
func TestRotasNativasDoPocketBaseRespondem404(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	r, err := apis.NewRouter(app) // registra /api/collections, /api/superusers, /api/health...
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	bloquearRotasNativas(r)
	ok := func(e *core.RequestEvent) error { return e.NoContent(http.StatusNoContent) }
	r.GET("/_/{path...}", ok) // o Serve registra o painel quando o build não usa no_ui
	r.GET("/api/keyhybrid/health", ok)
	r.GET("/v1/models", ok)
	r.Any("/proxy/{alias}/{path...}", ok)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}

	casos := map[string]bool{ // caminho -> deve responder 404
		"/_/":                                  true,
		"/_/images/logo.svg":                   true,
		"/api/collections":                     true,
		"/api/collections/vault_keys/records":  true,
		"/api/superusers":                      true,
		"/api/collections/_superusers/records": true,
		"/api/keyhybrid/health":                false,
		"/api/health":                          false,
		"/v1/models":                           false,
		"/proxy/supabase/v1/projects":          false,
	}
	for caminho, quer404 := range casos {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, caminho, nil))
		if (rec.Code == http.StatusNotFound) != quer404 {
			t.Errorf("%s: status %d, quer404=%v", caminho, rec.Code, quer404)
		}
	}
}

func TestRotaPermitida(t *testing.T) {
	for p, quer := range map[string]bool{
		"/v1": true, "/v1/chat/completions": true, "/api/ai/proxy/chat": true,
		"/api/keyhybrid/reveal": true, "/proxy/a/b": true, "/api/health": true,
		"/v1x": false, "/api/keyhybrid": false, "/proxyx/a": false,
		"/_/": false, "/api/files/x": false, "/api/realtime": false, "": false,
		"/api/keyhybrid/" + ".." + "/superusers": false,
	} {
		if got := rotaPermitida(p); got != quer {
			t.Errorf("rotaPermitida(%q) = %v, quer %v", p, got, quer)
		}
	}
}
