package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"
)

// Chama o handler do /reveal com os cabeçalhos dados; devolve o status (do erro ou da resposta)
func chamarReveal(t *testing.T, app core.App, revealTok string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/keyhybrid/reveal", strings.NewReader(`{"aliases":["ia"]}`))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	ev := &core.RequestEvent{App: app}
	ev.Request, ev.Response = req, rec
	if err := revealHandler(app, revealTok)(ev); err != nil {
		var apiErr *router.ApiError
		if !errors.As(err, &apiErr) {
			t.Fatalf("erro inesperado: %v", err)
		}
		return apiErr.Status, ""
	}
	return rec.Code, rec.Body.String()
}

func TestRevealSoAceitaRevealToken(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("ia", segredoIA, true))
	cli := loadOrCreateCLIToken()
	rev := loadOrCreateRevealToken()
	if cli == rev || len(rev) != 64 {
		t.Fatalf("tokens iguais ou reveal token malformado")
	}

	for nome, h := range map[string]map[string]string{
		"sem token":              {},
		"cli no header":          {"X-KeyHybrid-Token": cli},
		"cli como bearer":        {"Authorization": "Bearer " + cli},
		"cli no header novo":     {"X-KeyHybrid-Reveal-Token": cli},
		"reveal no header velho": {"X-KeyHybrid-Token": rev},
	} {
		if st, _ := chamarReveal(t, app, rev, h); st != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", nome, st)
		}
	}
	if st, _ := chamarReveal(t, app, rev, map[string]string{"X-KeyHybrid-Reveal-Token": rev, "Origin": "http://127.0.0.1:8090"}); st != http.StatusForbidden {
		t.Errorf("com Origin: status %d, want 403", st)
	}
	st, body := chamarReveal(t, app, rev, map[string]string{"X-KeyHybrid-Reveal-Token": rev})
	if st != http.StatusOK || !strings.Contains(body, segredoIA) {
		t.Fatalf("reveal token: status %d", st)
	}
}

// O kh run manda só o cabeçalho do reveal token, nunca o do proxy
func TestKhRunMandaRevealToken(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-KeyHybrid-Reveal-Token") != "tok-rev" || r.Header.Get("X-KeyHybrid-Token") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"secrets":{"ia":"valor"}}`)
	}))
	defer gw.Close()
	got, code := revealSecrets(io.Discard, gw.URL, "tok-rev", []string{"ia"})
	if code != 0 || got["ia"] != "valor" {
		t.Fatalf("revealSecrets = %v, %d", got, code)
	}

	// Ponta a ponta: o runCmd lê o .reveal.token (não o .cli.token) e entrega a variável ao filho
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KEYHYBRID_HOME", home)
	t.Setenv("KH_GATEWAY", gw.URL)
	_ = os.MkdirAll(home+"/.keyhybrid", 0700)
	_ = os.WriteFile(cliTokenPath(), []byte("tok-rev"), 0600)
	filho := []string{"--env", "X=ia", "--", "sh", "-c", `test "$X" = valor`}
	if c := runCmd(filho); c != 1 {
		t.Fatalf("kh run sem .reveal.token: code %d, want 1", c)
	}
	_ = os.WriteFile(revealTokenPath(), []byte("tok-rev\n"), 0600)
	if c := runCmd(filho); c != 0 {
		t.Fatalf("kh run com .reveal.token: code %d", c)
	}
}

// Roda um subcomando com stdin/stdout trocados por pipes
func rodarCmd(t *testing.T, cmd func([]string) int, args []string, entrada string) (int, string) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	defer func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = inR.Close()
		_ = outR.Close()
	}()
	go func() { _, _ = io.WriteString(inW, entrada); _ = inW.Close() }()
	var out bytes.Buffer
	pronto := make(chan struct{})
	go func() { _, _ = io.Copy(&out, outR); close(pronto) }()
	code := cmd(args)
	_ = outW.Close()
	<-pronto
	return code, out.String()
}

func TestVaultCLIExigeRevealToken(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	aplicar(t, app, "replace", entradaIA("ia", segredoIA, true))
	cli := loadOrCreateCLIToken()

	// Sem o arquivo do reveal token, nada passa
	if code, out := rodarCmd(t, vaultDumpCmd, nil, cli+"\n"); code != 2 || out != "" {
		t.Fatalf("dump sem .reveal.token: code %d, saída %d bytes", code, len(out))
	}
	rev := loadOrCreateRevealToken()
	for nome, entrada := range map[string]string{"vazio": "", "cli": cli + "\n", "lixo": "abc\n"} {
		if code, out := rodarCmd(t, vaultDumpCmd, nil, entrada); code != 2 || out != "" {
			t.Errorf("dump com stdin %s: code %d, saída %d bytes", nome, code, len(out))
		}
	}
	code, out := rodarCmd(t, vaultDumpCmd, nil, rev+"\n")
	if code != 0 || !strings.Contains(out, segredoIA) {
		t.Fatalf("dump com reveal token: code %d", code)
	}

	doc, _ := json.Marshal(vaultDoc{Format: vaultFormat, Version: vaultVersion,
		Entries: []vaultEntry{entradaIA("nova", "segredo-novo-123456789", false)}})
	if code, _ := rodarCmd(t, vaultApplyCmd, []string{"--mode", "merge"}, cli+"\n"+string(doc)); code != 2 {
		t.Fatalf("apply com cli token: code %d, want 2", code)
	}
	if code, _ := rodarCmd(t, vaultApplyCmd, []string{"--mode", "merge"}, string(doc)); code != 2 {
		t.Fatalf("apply sem token: code %d, want 2", code)
	}
	if _, ok := porAlias(t, app)["nova"]; ok {
		t.Fatal("apply recusado gravou no cofre")
	}
	if code, _ := rodarCmd(t, vaultApplyCmd, []string{"--mode", "merge"}, rev+"\n"+string(doc)); code != 0 {
		t.Fatalf("apply com reveal token: code %d", code)
	}
	if _, ok := porAlias(t, app)["nova"]; !ok {
		t.Fatal("apply com reveal token não gravou")
	}
}
