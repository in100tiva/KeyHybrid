package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

var (
	aliasRe      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	authHeaderRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	envNameRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func homeDir() string {
	if h := os.Getenv("KEYHYBRID_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if h := os.Getenv("USERPROFILE"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "."
}

func isAllowedAuthHeader(h string) bool {
	if !authHeaderRe.MatchString(h) {
		return false
	}
	lh := strings.ToLower(strings.TrimSpace(h))
	switch lh {
	case "host", "content-length", "transfer-encoding", "connection", "te", "trailer",
		"upgrade", "content-encoding", "expect", "forwarded":
		return false
	}
	if strings.HasPrefix(lh, "proxy-") || strings.HasPrefix(lh, "x-forwarded-") {
		return false
	}
	return true
}

// Origens do WebView (Tauri) e da interface local; qualquer outra leva 403
var allowedOrigins = map[string]bool{
	"tauri://localhost":       true,
	"http://tauri.localhost":  true,
	"https://tauri.localhost": true,
	"http://localhost:1420":   true,
	"http://127.0.0.1:1420":   true,
}

// rotaPermitida diz se o caminho é do gateway: /api/keyhybrid/*, /v1/*, /proxy/*, o chat
// legado /api/ai/proxy/chat e o /api/health do healthcheck do container.
func rotaPermitida(p string) bool {
	p = path.Clean("/" + p)
	switch p {
	case "/v1", "/api/health", "/api/ai/proxy/chat":
		return true
	}
	for _, prefixo := range []string{"/api/keyhybrid/", "/v1/", "/proxy/"} {
		if strings.HasPrefix(p, prefixo) {
			return true
		}
	}
	return false
}

// Barra DNS rebinding: o Host precisa ser loopback
func hostAllowed(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	return name == "127.0.0.1" || name == "localhost" || name == "::1"
}

func originAllowed(origin, host string) bool {
	return allowedOrigins[origin] || origin == "http://"+host
}

func cliTokenPath() string {
	return filepath.Join(homeDir(), ".keyhybrid", ".cli.token")
}

// Token só do /reveal, do kh run e do vault-dump/vault-apply (#44): o .cli.token do proxy não revela segredo
func revealTokenPath() string {
	return filepath.Join(homeDir(), ".keyhybrid", ".reveal.token")
}

// Token local de 32 bytes em hex: rotacionado a cada boot do gateway (KH-12).
// Quem não lê ~/.keyhybrid não usa o cofre por HTTP.
func loadOrCreateCLIToken() string {
	path := cliTokenPath()
	if force := os.Getenv("KEYHYBRID_FORCE_TOKEN"); len(force) == 64 {
		_ = os.MkdirAll(filepath.Dir(path), 0700)
		_ = os.WriteFile(path, []byte(force), 0600)
		return force
	}
	return rotacionarToken(path)
}

// Rotacionado no mesmo boot que o .cli.token, sempre aleatório
func loadOrCreateRevealToken() string {
	return rotacionarToken(revealTokenPath())
}

func rotacionarToken(path string) string {
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		log.Fatal("[KeyHybrid] falha ao gerar token local: ", err) // nunca semente fixa
	}
	tok := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(tok), 0600); err != nil {
		log.Fatal("[KeyHybrid] falha ao gravar token local: ", err)
	}
	return tok
}

// Só o cabeçalho próprio, em tempo constante; Authorization e X-KeyHybrid-Token não valem aqui
func revealTokenValid(r *http.Request, token string) bool {
	got := r.Header.Get("X-KeyHybrid-Reveal-Token")
	return token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func tokenValid(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	got := r.Header.Get("X-KeyHybrid-Token")
	if subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		bearerTok := strings.TrimSpace(auth[7:])
		if subtle.ConstantTimeCompare([]byte(bearerTok), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

// Autoriza uma chamada /v1/*. Devolve o alias pedido ("" = chave ativa) ou status+corpo do erro.
// "Bearer kh-<alias>" de alias de IA existente (ou kh-default) dispensa o token local, por
// compatibilidade com clientes OpenAI; kh-<alias> inexistente dá 404 com a lista; qualquer
// outro caso (outro Bearer, sem Authorization, só X-Provider) exige o token local.
func v1Auth(r *http.Request, token string, aiRecs []*core.Record) (alias string, status int, body map[string]any) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		bearer := strings.ToLower(strings.TrimSpace(auth[7:]))
		if a, ok := strings.CutPrefix(bearer, "kh-"); ok {
			return v1Alias(a, aiRecs)
		}
	}
	if !tokenValid(r, token) {
		return "", http.StatusUnauthorized, map[string]any{
			"message": "Informe Authorization: Bearer kh-<alias> de uma chave cadastrada ou o X-KeyHybrid-Token",
		}
	}
	return "", 0, nil
}

// Confere um alias explícito: "default" é a chave ativa; alias de IA inexistente dá 404 com a lista.
func v1Alias(a string, aiRecs []*core.Record) (string, int, map[string]any) {
	a = strings.ToLower(strings.TrimSpace(a))
	if a == "default" {
		return "", 0, nil
	}
	aliases := []string{}
	for _, rec := range aiRecs {
		if strings.EqualFold(rec.GetString("alias"), a) {
			return a, 0, nil
		}
		aliases = append(aliases, rec.GetString("alias"))
	}
	return "", http.StatusNotFound, map[string]any{
		"error":   "alias " + a + " não cadastrado no KeyHybrid",
		"aliases": aliases,
	}
}

// Base de chave de serviço: só https, sem usuário, query nem fragmento; barra final removida
func validateServiceBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", errors.New("URL base deve começar com https:// e ter um host")
	}
	if u.User != nil || strings.ContainsAny(base, "?#") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("URL base não pode ter usuário, senha, query ou fragmento")
	}
	lowPath := strings.ToLower(u.EscapedPath())
	if strings.Contains(lowPath, "%2f") || strings.Contains(lowPath, "%2e") ||
		strings.Contains(lowPath, "%5c") || strings.Contains(lowPath, "%00") {
		return "", errors.New("URL base não pode conter caracteres codificados no path")
	}
	if u.Path != "" {
		cleaned := path.Clean(u.Path)
		if cleaned != u.Path && cleaned != u.Path+"/" {
			return "", errors.New("URL base possui caminho inválido ou não canônico")
		}
	}
	return base, nil
}
