package main

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"
)

func TestUsageRegistraStatusPorAlias(t *testing.T) {
	recordUsage("OpenRouter", 401)
	recordUsage("groq", 200)
	recordUsage("", 200)    // sem alias não registra
	recordUsage("vazio", 0) // sem status não registra
	u, ok := lastUsage("openrouter")
	if !ok || u.status != 401 || time.Since(u.at) > time.Minute {
		t.Fatalf("openrouter = %+v, %v; want 401 recente", u, ok)
	}
	recordUsage("openrouter", 200)
	if u, _ := lastUsage("OPENROUTER"); u.status != 200 {
		t.Fatalf("último status = %d, want 200", u.status)
	}
	if u, _ := lastUsage("groq"); u.status != 200 {
		t.Fatalf("groq = %d, want 200", u.status)
	}
	if _, ok := lastUsage("vazio"); ok {
		t.Fatal("status 0 não deveria registrar")
	}
}

func TestErrStatus(t *testing.T) {
	if got := errStatus(router.NewNotFoundError("x", nil)); got != http.StatusNotFound {
		t.Errorf("ApiError = %d, want 404", got)
	}
	if got := errStatus(errors.New("x")); got != 500 {
		t.Errorf("erro comum = %d, want 500", got)
	}
	if got := errStatus(nil); got != 0 {
		t.Errorf("nil = %d, want 0", got)
	}
}

func TestLogV1SemSegredo(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// O corpo da resposta traz o segredo em outro campo: só o "model" pode sair
	body := []byte(`{"model":"openai/gpt-4o","choices":[{"message":{"content":"` + segredoTeste + `"}}]}`)
	finishV1("openrouter", "openrouter/auto", modelFromBody(body), 401, time.Now())

	out := buf.String()
	for _, want := range []string{`alias="openrouter"`, `modelo="openrouter/auto"`, `devolvido="openai/gpt-4o"`, "status=401"} {
		if !strings.Contains(out, want) {
			t.Errorf("log sem %q: %s", want, out)
		}
	}
	if strings.Contains(out, segredoTeste) {
		t.Fatalf("log vazou o segredo: %s", out)
	}
	if modelFromBody([]byte("data: {}")) != "-" {
		t.Error("corpo que não é JSON deveria virar -")
	}
}

func TestLogV1SemChaveNaoRegistraUso(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	finishV1("", "", "", http.StatusNotFound, time.Now())
	if out := buf.String(); !strings.Contains(out, `alias="-"`) || !strings.Contains(out, "status=404") {
		t.Fatalf("log = %s", out)
	}
	if _, ok := lastUsage(""); ok {
		t.Fatal("requisição sem chave não deveria registrar uso")
	}
}
