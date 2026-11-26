package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O gateway devolve tudo o que o KeySafeView tem; o kh list só pode repassar alias, tipo, provedor e modelo
func TestKhListSemSegredo(t *testing.T) {
	const token = "token-local-de-teste-abcdef"
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/keyhybrid/keys" || r.Header.Get("X-KeyHybrid-Token") != token {
			http.Error(w, "não", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]KeySafeView{
			{Id: "id-sigiloso", Alias: "openrouter", Provider: "openrouter", MaskedKey: segredoTeste, VirtualToken: "kh-openrouter", DefaultModel: "openrouter/auto", Active: true, Kind: "ai", AuthHeader: "cabecalho-sigiloso"},
			{Id: "id-sigiloso", Alias: "supabase", Provider: "supabase", MaskedKey: segredoTeste, Kind: "service", BaseURL: "https://url-sigilosa.example", AuthHeader: "cabecalho-sigiloso"},
		})
	}))
	defer gw.Close()

	home := t.TempDir()
	t.Setenv("KEYHYBRID_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("KH_GATEWAY", gw.URL)
	if err := os.MkdirAll(filepath.Join(home, ".keyhybrid"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cliTokenPath(), []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	proibidos := []string{segredoTeste, token, "id-sigiloso", "kh-openrouter", "cabecalho-sigiloso", "url-sigilosa"}
	for _, args := range [][]string{nil, {"--json"}} {
		var out bytes.Buffer
		if code := listCmd(args, &out); code != 0 {
			t.Fatalf("%v: código %d", args, code)
		}
		s := out.String()
		for _, p := range proibidos {
			if strings.Contains(s, p) {
				t.Fatalf("%v: saída vazou %q:\n%s", args, p, s)
			}
		}
		for _, p := range []string{"openrouter", "supabase", "openrouter/auto"} {
			if !strings.Contains(s, p) {
				t.Fatalf("%v: faltou %q:\n%s", args, p, s)
			}
		}
		if args == nil && (!strings.Contains(s, "IA") || !strings.Contains(s, "serviço")) {
			t.Fatalf("tabela sem tipo:\n%s", s)
		}
	}

	var out bytes.Buffer
	_ = listCmd([]string{"--json"}, &out)
	var items []map[string]any
	if err := json.Unmarshal(out.Bytes(), &items); err != nil || len(items) != 2 {
		t.Fatalf("JSON inválido: %v\n%s", err, out.String())
	}
	if items[1]["kind"] != "service" || len(items[0]) != 4 {
		t.Fatalf("campos inesperados: %v", items)
	}

	if code := listCmd([]string{"--xml"}, &out); code != 2 {
		t.Fatalf("opção desconhecida deveria dar 2, deu %d", code)
	}
}
