package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const segredoTeste = "segredo-de-teste-123456"

func TestHostAllowed(t *testing.T) {
	ok := []string{"127.0.0.1:8090", "localhost:8090", "[::1]:8090", "localhost"}
	bad := []string{"evil.example:8090", "127.0.0.1.evil.example", "localhost.evil.example:8090", ""}
	for _, h := range ok {
		if !hostAllowed(h) {
			t.Errorf("hostAllowed(%q) = false, want true", h)
		}
	}
	for _, h := range bad {
		if hostAllowed(h) {
			t.Errorf("hostAllowed(%q) = true, want false", h)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	host := "127.0.0.1:8090"
	ok := []string{"tauri://localhost", "http://tauri.localhost", "https://tauri.localhost",
		"http://localhost:1420", "http://127.0.0.1:1420", "http://127.0.0.1:8090"}
	bad := []string{"https://evil.example", "http://localhost:3000", "null", "http://127.0.0.1:8091"}
	for _, o := range ok {
		if !originAllowed(o, host) {
			t.Errorf("originAllowed(%q) = false, want true", o)
		}
	}
	for _, o := range bad {
		if originAllowed(o, host) {
			t.Errorf("originAllowed(%q) = true, want false", o)
		}
	}
}

func TestServiceBaseURL(t *testing.T) {
	got, err := validateServiceBaseURL("https://api.vercel.com/")
	if err != nil || got != "https://api.vercel.com" {
		t.Fatalf("validateServiceBaseURL barra final = %q, %v", got, err)
	}
	bad := []string{"http://api.vercel.com", "https://user:senha@api.vercel.com",
		"https://api.vercel.com?x=1", "https://api.vercel.com#f", "https://", "api.vercel.com"}
	for _, b := range bad {
		if _, err := validateServiceBaseURL(b); err == nil {
			t.Errorf("validateServiceBaseURL(%q) aceitou, deveria recusar", b)
		}
	}
}

func TestBuildProxyTarget(t *testing.T) {
	base := "https://api.vercel.com"
	got, err := buildProxyTarget(base, "v9/projects", "x=1")
	if err != nil || got != "https://api.vercel.com/v9/projects?x=1" {
		t.Fatalf("buildProxyTarget = %q, %v", got, err)
	}
	bad := []string{"../x", "/evil.com", "v9/../../x", "%2F%2Fevil.com", "v9/%2e%2e/x", "v9//x", "v9/@evil.com", "v9\\x"}
	for _, rest := range bad {
		if _, err := buildProxyTarget(base, rest, ""); err == nil {
			t.Errorf("buildProxyTarget(%q) aceitou, deveria recusar", rest)
		}
	}
}

func TestForwardToService(t *testing.T) {
	var visitados []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visitados = append(visitados, r.URL.Path)
		if got := r.Header.Get("apikey"); got != segredoTeste {
			t.Errorf("apikey = %q, want segredo", got)
		}
		for _, h := range []string{"Authorization", "Cookie", "X-KeyHybrid-Token"} {
			if r.Header.Get(h) != "" {
				t.Errorf("cabeçalho %s vazou para o upstream", h)
			}
		}
		if r.URL.Path == "/redir" {
			w.Header().Set("Set-Cookie", "sessao=1")
			w.Header().Set("Location", "/final")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	in := httptest.NewRequest(http.MethodGet, "/proxy/svc/redir", nil)
	in.Header.Set("Authorization", "Bearer do-cliente")
	in.Header.Set("Cookie", "a=b")
	in.Header.Set("X-KeyHybrid-Token", "tok")
	rec := httptest.NewRecorder()

	status := forwardToService(rec, in, up.URL+"/redir", "apikey", segredoTeste)
	if status != http.StatusFound || rec.Code != http.StatusFound {
		t.Fatalf("status = %d / %d, want 302", status, rec.Code)
	}
	if len(visitados) != 1 || visitados[0] != "/redir" {
		t.Errorf("upstream visitou %v, want só /redir", visitados)
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("Set-Cookie do upstream voltou ao cliente")
	}
}

func TestMaskWriter(t *testing.T) {
	stream := "valor=" + segredoTeste + " fim\n"
	want := "valor=**** fim\n"
	for _, n := range []int{1, 5, 13, len(stream)} {
		var buf bytes.Buffer
		mw := &maskWriter{dst: &buf, secrets: [][]byte{[]byte(segredoTeste)}}
		for i := 0; i < len(stream); i += n {
			end := min(i+n, len(stream))
			if _, err := mw.Write([]byte(stream[i:end])); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Flush(); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != want {
			t.Errorf("pedaços de %d: saída %q, want %q", n, got, want)
		}
	}
}

func TestDataDirFicaNoHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".keyhybrid", "pb_data")
	if got := dataDir(); got != want {
		t.Fatalf("dataDir() = %q, want %q", got, want)
	}
	if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
		t.Fatalf("dataDir() não criou %q: %v", want, err)
	}
}
