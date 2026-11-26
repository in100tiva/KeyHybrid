package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

const segredoIA = "segredo-ia-123456789"

// Abre o cofre de uma home isolada pelo mesmo caminho da CLI
func abrirVaultEm(t *testing.T, home string) core.App {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("KEYHYBRID_HOME", home)
	app, err := abrirVault(true)
	if err != nil {
		t.Fatalf("abrirVault: %v", err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	return app
}

func dumpJSON(t *testing.T, app core.App) string {
	t.Helper()
	doc, err := dumpVault(app)
	if err != nil {
		t.Fatalf("dumpVault: %v", err)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

func porAlias(t *testing.T, app core.App) map[string]vaultEntry {
	t.Helper()
	doc, err := dumpVault(app)
	if err != nil {
		t.Fatalf("dumpVault: %v", err)
	}
	m := map[string]vaultEntry{}
	for _, e := range doc.Entries {
		m[e.Alias] = e
	}
	return m
}

func aplicar(t *testing.T, app core.App, mode string, entradas ...vaultEntry) applyReport {
	t.Helper()
	doc := vaultDoc{Format: vaultFormat, Version: vaultVersion, Entries: entradas}
	rep, err := applyVault(app, doc, mode, false)
	if err != nil {
		t.Fatalf("applyVault %s: %v", mode, err)
	}
	return rep
}

func entradaIA(alias, secret string, ativa bool) vaultEntry {
	return vaultEntry{Alias: alias, Provider: "openrouter", Kind: "ai", Secret: secret, DefaultModel: "openrouter/auto", Active: ativa}
}

func entradaServico(alias, secret string) vaultEntry {
	return vaultEntry{Alias: alias, Provider: "outro", Kind: "service", Secret: secret, BaseURL: "https://api.exemplo.com/v1", AuthHeader: "X-Chave"}
}

// Transporte que falha o teste se qualquer requisição sair da máquina
type rtBloqueado struct{ t *testing.T }

func (r rtBloqueado) RoundTrip(req *http.Request) (*http.Response, error) {
	r.t.Errorf("tentativa de rede durante vault-dump/vault-apply: método %s", req.Method)
	return nil, errors.New("rede bloqueada no teste")
}

func TestVaultIdaEVolta(t *testing.T) {
	appA := abrirVaultEm(t, t.TempDir())
	aplicar(t, appA, "replace", entradaIA("Minha-IA", segredoIA, true), entradaServico("API-Clientes", "segredo-svc-123456"))
	docA, err := dumpVault(appA)
	if err != nil {
		t.Fatalf("dumpVault A: %v", err)
	}
	jsonA := dumpJSON(t, appA)

	appB := abrirVaultEm(t, t.TempDir())
	aplicar(t, appB, "replace", docA.Entries...)
	if jsonB := dumpJSON(t, appB); jsonB != jsonA {
		t.Fatalf("dump B difere do dump A:\nA=%s\nB=%s", jsonA, jsonB)
	}
}

func TestVaultMerge(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("Minha-IA", "segredo-antigo-111", true))
	rep := aplicar(t, app, "merge", entradaIA("minha-ia", "segredo-novo-222222", false), entradaIA("nova-ia", "segredo-nova-333333", true))

	if !reflect.DeepEqual(rep.Conflicts, []string{"minha-ia"}) {
		t.Fatalf("conflicts = %v", rep.Conflicts)
	}
	if !reflect.DeepEqual(rep.Added, []string{"nova-ia"}) {
		t.Fatalf("added = %v", rep.Added)
	}
	m := porAlias(t, app)
	if m["Minha-IA"].Secret != "segredo-antigo-111" {
		t.Fatal("merge sobrescreveu segredo existente")
	}
	if _, ok := m["minha-ia"]; ok {
		t.Fatal("alias existente foi trocado de caixa")
	}
	if m["nova-ia"].Active {
		t.Fatal("nova IA ficou ativa mesmo com IA ativa existente")
	}
}

func TestVaultReplace(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("ia-antiga", segredoIA, true), entradaServico("svc-1", "segredo-svc-1-123"), entradaServico("svc-2", "segredo-svc-2-123"))
	rep := aplicar(t, app, "replace", entradaIA("unica", "segredo-unica-123456", false))
	if rep.Removed != 3 || rep.TotalAfter != 1 {
		t.Fatalf("removed=%d total_after=%d", rep.Removed, rep.TotalAfter)
	}
	m := porAlias(t, app)
	if len(m) != 1 || m["unica"].Secret != "segredo-unica-123456" {
		t.Fatalf("cofre após replace = %v", m)
	}
}

func TestVaultRegrasDeAtiva(t *testing.T) {
	s := "segredo-regra-123456"
	app := abrirVaultEm(t, t.TempDir())

	// IA ativa já existe: novas IAs ficam inativas e serviço nunca fica ativo
	aplicar(t, app, "replace", entradaIA("ia-ativa", s, true), entradaServico("svc", s))
	sv := entradaServico("svc-novo", s)
	sv.Active = true
	aplicar(t, app, "merge", entradaIA("nova-1", s, true), entradaIA("nova-2", s, false), sv)
	m := porAlias(t, app)
	if !m["ia-ativa"].Active || m["nova-1"].Active || m["nova-2"].Active || m["svc-novo"].Active || m["svc"].Active {
		t.Fatalf("regra com IA ativa existente violada: %+v", m)
	}

	// Sem IA ativa: a primeira IA com active=true vira ativa, as demais não
	aplicar(t, app, "replace", entradaServico("svc", s))
	aplicar(t, app, "merge", entradaIA("ia-1", s, true), entradaIA("ia-2", s, true))
	m = porAlias(t, app)
	if !m["ia-1"].Active || m["ia-2"].Active {
		t.Fatalf("regra sem IA ativa violada: %+v", m)
	}

	// Replace: primeira com active=true entre as inseridas
	aplicar(t, app, "replace", entradaIA("ia-a", s, false), entradaIA("ia-b", s, true), entradaIA("ia-c", s, true))
	m = porAlias(t, app)
	if m["ia-a"].Active || !m["ia-b"].Active || m["ia-c"].Active {
		t.Fatalf("regra do replace violada: %+v", m)
	}
}

func TestVaultValidacaoNaoGravaNada(t *testing.T) {
	s := "segredo-valido-123456"
	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("base", segredoIA, true))
	antes := dumpJSON(t, app)

	casos := []struct {
		nome     string
		entradas []vaultEntry
	}{
		{"serviço com http", []vaultEntry{
			entradaIA("ok", s, false),
			{Alias: "svc-http", Kind: "service", Secret: "segredo-sensivel-999", BaseURL: "http://api.exemplo.com/v1"},
		}},
		{"alias duplicado", []vaultEntry{entradaIA("Dup", s, false), entradaIA("dup", s, false)}},
		{"segredo já cifrado", []vaultEntry{entradaIA("ok", s, false), entradaIA("cifrada", "enc:v1:segredo-sensivel-999", false)}},
	}
	for _, c := range casos {
		_, err := applyVault(app, vaultDoc{Format: vaultFormat, Version: vaultVersion, Entries: c.entradas}, "merge", false)
		if err == nil {
			t.Fatalf("%s: esperava erro", c.nome)
		}
		if strings.Contains(err.Error(), "segredo-sensivel-999") {
			t.Fatalf("%s: mensagem de erro expôs o segredo", c.nome)
		}
		if depois := dumpJSON(t, app); depois != antes {
			t.Fatalf("%s: cofre mudou após erro de validação", c.nome)
		}
	}
}

func TestVaultRollbackNaoGravaParcial(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("base", segredoIA, true))
	antes := dumpJSON(t, app)

	criados := 0
	app.OnRecordCreate("vault_keys").BindFunc(func(e *core.RecordEvent) error {
		criados++
		if criados == 2 {
			return errors.New("falha simulada no segundo insert")
		}
		return e.Next()
	})
	doc := vaultDoc{Format: vaultFormat, Version: vaultVersion, Entries: []vaultEntry{
		entradaIA("a", "segredo-a-123456", true),
		entradaIA("b", "segredo-b-123456", false),
		entradaIA("c", "segredo-c-123456", false),
	}}
	if _, err := applyVault(app, doc, "replace", false); err == nil {
		t.Fatal("esperava erro no segundo insert")
	}
	if depois := dumpJSON(t, app); depois != antes {
		t.Fatal("cofre mudou após rollback")
	}
}

func TestVaultDryRunNaoEscreve(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("antiga", segredoIA, true))
	antes := dumpJSON(t, app)

	doc := vaultDoc{Format: vaultFormat, Version: vaultVersion, Entries: []vaultEntry{
		entradaIA("nova-a", "segredo-nova-a-1234", true),
		entradaServico("svc-b", "segredo-svc-b-1234"),
	}}
	rep, err := applyVault(app, doc, "replace", true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !rep.DryRun || rep.Removed != 1 || rep.TotalAfter != 2 || !reflect.DeepEqual(rep.Added, []string{"nova-a", "svc-b"}) {
		t.Fatalf("relatório do dry-run = %+v", rep)
	}
	if depois := dumpJSON(t, app); depois != antes {
		t.Fatal("dry-run alterou o cofre")
	}
}

func TestVaultSemRede(t *testing.T) {
	orig := http.DefaultTransport
	http.DefaultTransport = rtBloqueado{t: t}
	t.Cleanup(func() { http.DefaultTransport = orig })

	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("ia", segredoIA, true), entradaServico("svc", "segredo-svc-123456"))
	_ = dumpJSON(t, app)
	aplicar(t, app, "merge", entradaIA("ia-2", "segredo-ia-2-123456", false))
}

func TestVaultSemRotaHTTP(t *testing.T) {
	arquivos, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	rotaRe := regexp.MustCompile(`\.(GET|POST|PUT|PATCH|DELETE|Any)\("([^"]*)"`)
	proibidoRe := regexp.MustCompile(`(?i)backup|export|import|dump|restore|vault-apply`)
	achadas := 0
	for _, a := range arquivos {
		nome := a.Name()
		if a.IsDir() || !strings.HasSuffix(nome, ".go") || strings.HasSuffix(nome, "_test.go") {
			continue
		}
		conteudo, err := os.ReadFile(nome)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", nome, err)
		}
		for _, m := range rotaRe.FindAllStringSubmatch(string(conteudo), -1) {
			achadas++
			if proibidoRe.MatchString(m[2]) {
				t.Errorf("rota proibida em %s: %q", nome, m[2])
			}
		}
	}
	if achadas == 0 {
		t.Fatal("nenhuma rota encontrada; a regex do teste está desatualizada")
	}
}

func TestVaultDumpFalhaSemVazarSegredo(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	col, err := app.FindCollectionByNameOrId("vault_keys")
	if err != nil {
		t.Fatalf("FindCollectionByNameOrId: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("alias", "quebrada")
	rec.Set("provider", "openrouter")
	rec.Set("kind", "ai")
	rec.Set("secret_key", "enc:v1:"+base64.StdEncoding.EncodeToString([]byte("lixo-nao-cifrado")))
	rec.Set("default_model", "openrouter/auto")
	rec.Set("active", false)
	if err := app.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, err = dumpVault(app)
	if err == nil || !strings.Contains(err.Error(), "não foi possível decifrar a chave quebrada") {
		t.Fatalf("erro = %v", err)
	}
}

// Dump ou prévia num cofre que não existe falha alto e não cria nada
func TestVaultSemCofreNaoCria(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KEYHYBRID_HOME", home)
	if _, err := abrirVault(false); !errors.Is(err, errCofreAusente) {
		t.Fatalf("abrirVault(false) = %v, want errCofreAusente", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".keyhybrid")); !os.IsNotExist(err) {
		t.Fatalf(".keyhybrid foi criado: %v", err)
	}
	rep, err := applyVault(nil, vaultDoc{Format: vaultFormat, Version: vaultVersion,
		Entries: []vaultEntry{entradaIA("or-teste", segredoIA, true)}}, "merge", true)
	if err != nil || !reflect.DeepEqual(rep.Added, []string{"or-teste"}) || rep.TotalAfter != 1 {
		t.Fatalf("prévia sem cofre = %+v, %v", rep, err)
	}
}
