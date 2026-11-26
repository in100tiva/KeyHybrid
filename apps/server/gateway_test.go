package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

const tokenTeste = "token-local-de-teste"

// Sobe as rotas de chaves e do /v1 num mux de teste, sem porta e sem o cofre real
func muxGateway(t *testing.T, app core.App) http.Handler {
	t.Helper()
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	liberarAuthorizationProprio(r)
	registrarRotasDeChaves(r, app, tokenTeste)
	r.POST("/v1/chat/completions", proxyChat(app, tokenTeste))
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	return mux
}

func novaChave(t *testing.T, app core.App, alias, kind string, ativa bool, modelo, baseURL string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("vault_keys")
	if err != nil {
		t.Fatalf("coleção: %v", err)
	}
	enc, err := encryptSecretAtRest("segredo-" + alias)
	if err != nil {
		t.Fatalf("cifrar: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("alias", alias)
	rec.Set("provider", "openrouter")
	rec.Set("kind", kind)
	rec.Set("secret_key", enc)
	rec.Set("base_url", baseURL)
	rec.Set("default_model", modelo)
	rec.Set("active", ativa)
	if err := app.Save(rec); err != nil {
		t.Fatalf("salvar %s: %v", alias, err)
	}
	return rec
}

func chamar(t *testing.T, mux http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func ativas(t *testing.T, app core.App) []string {
	t.Helper()
	recs, err := app.FindAllRecords("vault_keys")
	if err != nil {
		t.Fatalf("listar: %v", err)
	}
	var out []string
	for _, r := range recs {
		if r.GetBool("active") {
			out = append(out, r.GetString("alias"))
		}
	}
	return out
}

// Incidente de 18/11: o 404 da OpenRouter virava openrouter/auto e o cliente recebia outro modelo
func TestProxy404VoltaComoVeioSemTrocarModelo(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	var chamadas atomic.Int32
	var modeloRecebido atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas.Add(1)
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		modeloRecebido.Store(p["model"])
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"No endpoints found"}}`)
	}))
	defer up.Close()
	novaChave(t, app, "or", "ai", true, "openrouter/auto", up.URL)

	res := chamar(t, muxGateway(t, app), http.MethodPost, "/v1/chat/completions",
		`{"model":"openai/gpt-6.2","messages":[{"role":"user","content":"oi"}]}`,
		map[string]string{"Authorization": "Bearer kh-or"})

	if res.Code != http.StatusNotFound {
		t.Fatalf("status %d, esperado 404; corpo: %s", res.Code, res.Body.String())
	}
	if n := chamadas.Load(); n != 1 {
		t.Fatalf("%d requisições ao provedor, esperado 1 (sem segunda tentativa)", n)
	}
	if got := modeloRecebido.Load(); got != "openai/gpt-6.2" {
		t.Fatalf("provedor recebeu modelo %v, esperado o pedido pelo cliente", got)
	}
	if !strings.Contains(res.Body.String(), "No endpoints found") {
		t.Fatalf("corpo do provedor não repassado: %s", res.Body.String())
	}
	for k := range res.Header() {
		if strings.HasPrefix(strings.ToLower(k), "x-keyhybrid-fallback") {
			t.Fatalf("cabeçalho de fallback presente: %s", k)
		}
	}
}

// Listar é só leitura, com ou sem token: nada de ativar chave nem regravar default_model
func TestListarChavesNaoAlteraNada(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	novaChave(t, app, "b-ia", "ai", false, "google/gemini-2.0-flash-001", "")
	novaChave(t, app, "a-ia", "ai", false, "", "")
	novaChave(t, app, "svc", "service", false, "", "https://api.exemplo.com/v1")
	antes := dumpJSON(t, app)
	carimbos := map[string]string{}
	recs, _ := app.FindAllRecords("vault_keys")
	for _, r := range recs {
		carimbos[r.Id] = r.GetString("updated")
	}

	mux := muxGateway(t, app)
	for _, hdr := range []map[string]string{nil, {"X-KeyHybrid-Token": tokenTeste}} {
		if res := chamar(t, mux, http.MethodGet, "/api/keyhybrid/keys", "", hdr); res.Code != http.StatusOK {
			t.Fatalf("GET /keys: status %d", res.Code)
		}
	}

	if depois := dumpJSON(t, app); depois != antes {
		t.Fatalf("GET /keys alterou o cofre:\nantes:  %s\ndepois: %s", antes, depois)
	}
	recs, _ = app.FindAllRecords("vault_keys")
	for _, r := range recs {
		if r.GetString("updated") != carimbos[r.Id] {
			t.Fatalf("GET /keys regravou %s", r.GetString("alias"))
		}
	}
}

func TestCadastroDaPrimeiraChaveDeIANasceAtiva(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	res := chamar(t, muxGateway(t, app), http.MethodPost, "/api/keyhybrid/keys",
		`{"alias":"principal","provider":"openrouter","secret_key":"sk-or-1234567890"}`,
		map[string]string{"X-KeyHybrid-Token": tokenTeste})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /keys: status %d; corpo: %s", res.Code, res.Body.String())
	}
	if got := ativas(t, app); len(got) != 1 || got[0] != "principal" {
		t.Fatalf("ativas = %v, esperado [principal]", got)
	}

	// Modelo vazio vira openrouter/auto; o escolhido no cadastro fica como veio
	res = chamar(t, muxGateway(t, app), http.MethodPost, "/api/keyhybrid/keys",
		`{"alias":"gemini","provider":"openrouter","secret_key":"sk-or-0987654321","default_model":"google/gemini-2.0-flash-001"}`,
		map[string]string{"X-KeyHybrid-Token": tokenTeste})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /keys gemini: status %d; corpo: %s", res.Code, res.Body.String())
	}
	modelos := map[string]string{}
	recs, _ := app.FindAllRecords("vault_keys")
	for _, r := range recs {
		modelos[r.GetString("alias")] = r.GetString("default_model")
	}
	if modelos["principal"] != "openrouter/auto" || modelos["gemini"] != "google/gemini-2.0-flash-001" {
		t.Fatalf("default_model regravado no cadastro: %v", modelos)
	}
}

func TestExcluirAAtivaPassaParaAProximaPorAlias(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	zeta := novaChave(t, app, "zeta", "ai", true, "openrouter/auto", "")
	beta := novaChave(t, app, "beta", "ai", false, "openrouter/auto", "")
	novaChave(t, app, "alfa", "ai", false, "openrouter/auto", "")
	novaChave(t, app, "aaa-svc", "service", false, "", "https://api.exemplo.com/v1")
	mux := muxGateway(t, app)
	tok := map[string]string{"X-KeyHybrid-Token": tokenTeste}

	// Excluir uma que não é a ativa não mexe na ativa
	if res := chamar(t, mux, http.MethodDelete, "/api/keyhybrid/keys/"+beta.Id, "", tok); res.Code != http.StatusOK {
		t.Fatalf("DELETE beta: status %d", res.Code)
	}
	if got := ativas(t, app); len(got) != 1 || got[0] != "zeta" {
		t.Fatalf("ativas = %v, esperado [zeta]", got)
	}

	// Excluir a ativa: a próxima de IA por alias (alfa) assume; a de serviço nunca
	if res := chamar(t, mux, http.MethodDelete, "/api/keyhybrid/keys/"+zeta.Id, "", tok); res.Code != http.StatusOK {
		t.Fatalf("DELETE zeta: status %d", res.Code)
	}
	if got := ativas(t, app); len(got) != 1 || got[0] != "alfa" {
		t.Fatalf("ativas = %v, esperado [alfa]", got)
	}
}
