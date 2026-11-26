package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// #43: o /v1 não aceita qualquer Bearer e não troca alias desconhecido pela chave ativa
func TestV1Auth(t *testing.T) {
	col := core.NewBaseCollection("vault_keys")
	or := core.NewRecord(col)
	or.Set("alias", "openrouter")
	gr := core.NewRecord(col)
	gr.Set("alias", "Groq")
	recs := []*core.Record{or, gr}
	tok := strings.Repeat("a", 64)

	casos := []struct {
		nome, auth, khTok, provider string
		status                      int
		alias                       string
	}{
		{"kh-alias existente passa sem token", "Bearer kh-openrouter", "", "", 0, "openrouter"},
		{"kh-alias sem diferenciar caixa", "bearer KH-groq", "", "", 0, "groq"},
		{"kh-default vai para a chave ativa", "Bearer kh-default", "", "", 0, ""},
		{"kh-alias inexistente dá 404", "Bearer kh-errado", "", "", http.StatusNotFound, ""},
		{"kh-alias inexistente dá 404 mesmo com token", "Bearer kh-errado", tok, "", http.StatusNotFound, ""},
		{"Bearer qualquer sem token dá 401", "Bearer x", "", "", http.StatusUnauthorized, ""},
		{"alias sem kh- sem token dá 401", "Bearer openrouter", "", "", http.StatusUnauthorized, ""},
		{"sem Authorization dá 401", "", "", "", http.StatusUnauthorized, ""},
		{"só X-Provider dá 401", "", "", "openrouter", http.StatusUnauthorized, ""},
		{"token errado dá 401", "Bearer x", strings.Repeat("b", 64), "", http.StatusUnauthorized, ""},
		{"X-KeyHybrid-Token válido usa a chave ativa", "Bearer x", tok, "", 0, ""},
		{"token no Bearer usa a chave ativa", "Bearer " + tok, "", "", 0, ""},
	}
	for _, c := range casos {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		if c.khTok != "" {
			r.Header.Set("X-KeyHybrid-Token", c.khTok)
		}
		if c.provider != "" {
			r.Header.Set("X-Provider", c.provider)
		}
		alias, st, body := v1Auth(r, tok, recs)
		if st != c.status || alias != c.alias {
			t.Errorf("%s: status=%d alias=%q, esperado status=%d alias=%q", c.nome, st, alias, c.status, c.alias)
		}
		if st == http.StatusNotFound {
			lista, _ := body["aliases"].([]string)
			if strings.Join(lista, ",") != "openrouter,Groq" {
				t.Errorf("%s: aliases=%v, esperado a lista de IA", c.nome, body["aliases"])
			}
		}
	}

	// Campo "alias" do JSON passa pela mesma conferência
	if _, st, _ := v1Alias("errado", recs); st != http.StatusNotFound {
		t.Errorf("alias do payload inexistente: status=%d, esperado 404", st)
	}
	if a, st, _ := v1Alias("OpenRouter", recs); st != 0 || a != "openrouter" {
		t.Errorf("alias do payload existente: status=%d alias=%q", st, a)
	}
}
