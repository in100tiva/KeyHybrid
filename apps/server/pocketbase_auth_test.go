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
