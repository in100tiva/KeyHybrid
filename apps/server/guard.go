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

// Token local de 32 bytes em hex: quem não lê ~/.keyhybrid não usa o cofre por HTTP
func loadOrCreateCLIToken() string {
	path := cliTokenPath()
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); len(tok) == 64 {
			return tok
		}
	}
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

func tokenValid(r *http.Request, token string) bool {
	got := r.Header.Get("X-KeyHybrid-Token")
	return token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
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
