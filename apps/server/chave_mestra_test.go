package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// data.db no disco e chave sumida: recusa subir e não cria chave nova (nem com --dir fora da home)
func TestChaveAusenteComCofreNaoSobe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KEYHYBRID_HOME", home)
	for _, dataDir := range []string{filepath.Join(home, ".keyhybrid", "pb_data"), t.TempDir()} {
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, "data.db"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		err := carregarChaveMestra(dataDir)
		if err == nil || !strings.Contains(err.Error(), "chave mestra ausente") {
			t.Fatalf("%s: erro = %v, esperava recusa", dataDir, err)
		}
		if chaveNoDisco(filepath.Join(home, ".keyhybrid")) {
			t.Fatalf("%s: criou chave nova com o cofre existente", dataDir)
		}
	}
}

// Instalação limpa (sem data.db) segue criando a chave
func TestChaveCriadaEmInstalacaoLimpa(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KEYHYBRID_HOME", home)
	if err := carregarChaveMestra(filepath.Join(home, ".keyhybrid", "pb_data")); err != nil {
		t.Fatalf("carregarChaveMestra: %v", err)
	}
	if !chaveNoDisco(filepath.Join(home, ".keyhybrid")) {
		t.Fatal("chave não foi criada")
	}
	if k, err := chaveMestraAtual(); err != nil || len(k) != 32 {
		t.Fatalf("chave em memória = %d bytes, %v", len(k), err)
	}
}

func chaveOuFalha(t *testing.T, dir string) []byte {
	t.Helper()
	k, err := loadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("loadOrCreateMasterKey: %v", err)
	}
	return k
}

// Arquivo vazio ou com lixo: erro claro, sem recursão e sem tocar no arquivo
func TestArquivoDaChaveVazioOuInvalidoEhErro(t *testing.T) {
	for _, conteudo := range [][]byte{{}, []byte("curto")} {
		dir := t.TempDir()
		wp := filepath.Join(dir, ".master.key.wrapped")
		if err := os.WriteFile(wp, conteudo, 0600); err != nil {
			t.Fatal(err)
		}
		if k, err := loadOrCreateMasterKey(dir); err == nil || k != nil {
			t.Fatalf("%q: chave %d bytes, erro %v; esperava erro", conteudo, len(k), err)
		}
		if got, _ := os.ReadFile(wp); string(got) != string(conteudo) {
			t.Fatalf("%q: arquivo mudou para %q", conteudo, got)
		}
	}
}

// Trava para atualizações do PocketBase: o main confia que NewWithConfig já resolveu o --dir
func TestDataDirRespeitaFlagDir(t *testing.T) {
	d := t.TempDir()
	antigo := os.Args
	t.Cleanup(func() { os.Args = antigo })
	os.Args = []string{"keyhybrid-server", "serve", "--dir", d}
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: filepath.Join(t.TempDir(), "padrao")})
	if got := app.DataDir(); got != d {
		t.Fatalf("DataDir() = %q, want %q", got, d)
	}
}

func TestSegredoSemPrefixoOuLixoEhErro(t *testing.T) {
	abrirVaultEm(t, t.TempDir())
	for _, v := range []string{"sk-texto-puro", "enc:v1:" + base64.StdEncoding.EncodeToString([]byte("lixo-nao-cifrado"))} {
		if s, err := decryptSecretAtRest(v); err == nil || s != "" {
			t.Fatalf("%q: decifrou %q, %v", v, s, err)
		}
	}
	enc, err := encryptSecretAtRest("segredo-ok-123")
	if err != nil {
		t.Fatal(err)
	}
	if s, err := decryptSecretAtRest(enc); err != nil || s != "segredo-ok-123" {
		t.Fatalf("ida e volta = %q, %v", s, err)
	}
}

// Segredo que não decifra: 500 claro e o provedor nunca recebe a requisição
func TestSegredoIlegivelNaoChegaAoProvedor(t *testing.T) {
	app := abrirVaultEm(t, t.TempDir())
	var chamadas atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas.Add(1)
	}))
	defer upstream.Close()

	col, err := app.FindCollectionByNameOrId("vault_keys")
	if err != nil {
		t.Fatal(err)
	}
	rec := core.NewRecord(col)
	rec.Set("alias", "quebrada")
	rec.Set("secret_key", "enc:v1:"+base64.StdEncoding.EncodeToString([]byte("lixo-nao-cifrado")))

	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	r.GET("/proxy", func(evt *core.RequestEvent) error {
		secret, err := segredoDoRegistro(evt, rec)
		if err != nil {
			return err
		}
		forwardToService(evt.Response, evt.Request, upstream.URL, "Authorization", "Bearer "+secret)
		return nil
	})
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/proxy", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", w.Code)
	}
	var corpo map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &corpo); err != nil || !strings.Contains(corpo["error"], "alias quebrada não decifra") {
		t.Fatalf("corpo = %q (%v)", w.Body.String(), err)
	}
	if n := chamadas.Load(); n != 0 {
		t.Fatalf("provedor recebeu %d requisição(ões)", n)
	}
}
