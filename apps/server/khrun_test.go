package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Mensagens do kh run dizem o próximo passo e nunca carregam o token nem segredo
func TestRevealSecretsMensagens(t *testing.T) {
	const token = "tok-local-segredo-123456"
	const segredo = "sk-segredo-que-nao-pode-vazar"
	casos := []struct {
		nome    string
		handler http.HandlerFunc
		espera  []string
	}{
		{"alias ausente lista os cadastrados", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/keyhybrid/keys" {
				if r.Header.Get("X-KeyHybrid-Token") != "" {
					t.Error("listagem de aliases não deve levar o token")
				}
				_, _ = w.Write([]byte(`[{"alias":"openrouter","masked_key":"` + segredo + `"},{"alias":"supabase"}]`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"missing":["supabse"]}`))
		}, []string{"alias não cadastrado: supabse", "cadastrados: openrouter, supabase", "cadastre no app KeyHybrid"}},
		{"alias ausente com cofre vazio", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/keyhybrid/keys" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"missing":["x"]}`))
		}, []string{"nenhum alias cadastrado"}},
		{"token recusado", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}, []string{"o token local mudou", "systemctl --user restart keyhybrid.service"}},
		{"outro status", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(segredo))
		}, []string{"HTTP 500", "journalctl --user -u keyhybrid.service"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()
			var buf bytes.Buffer
			if _, code := revealSecrets(&buf, srv.URL, token, []string{"supabse"}); code != 1 {
				t.Fatalf("código = %d, esperado 1", code)
			}
			conferirMensagem(t, buf.String(), c.espera, token, segredo)
		})
	}

	t.Run("gateway fora", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		gw := srv.URL
		srv.Close() // porta fechada: conexão recusada
		var buf bytes.Buffer
		if _, code := revealSecrets(&buf, gw, token, []string{"a"}); code != 1 {
			t.Fatalf("código = %d, esperado 1", code)
		}
		conferirMensagem(t, buf.String(), []string{"não está respondendo em " + gw, "systemctl --user start keyhybrid.service", "abra o app KeyHybrid"}, token, segredo)
	})
}

func conferirMensagem(t *testing.T, msg string, espera []string, proibidos ...string) {
	t.Helper()
	for _, e := range espera {
		if !strings.Contains(msg, e) {
			t.Errorf("mensagem sem %q:\n%s", e, msg)
		}
	}
	for _, p := range proibidos {
		if strings.Contains(msg, p) {
			t.Errorf("mensagem vazou %q:\n%s", p, msg)
		}
	}
}
