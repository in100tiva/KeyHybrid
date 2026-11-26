package main

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"
)

// Último uso por alias (#55): só em memória, zera quando o gateway reinicia.
type aliasUsage struct {
	at     time.Time
	status int
}

var (
	usageMu sync.Mutex
	usage   = map[string]aliasUsage{}
)

func recordUsage(alias string, status int) {
	if alias == "" || status == 0 {
		return
	}
	usageMu.Lock()
	usage[strings.ToLower(alias)] = aliasUsage{at: time.Now(), status: status}
	usageMu.Unlock()
}

func lastUsage(alias string) (aliasUsage, bool) {
	usageMu.Lock()
	defer usageMu.Unlock()
	u, ok := usage[strings.ToLower(alias)]
	return u, ok
}

// Status de uma requisição que terminou em erro do próprio handler (ApiError vira resposta depois).
func errStatus(err error) int {
	var apiErr *router.ApiError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	if err != nil {
		return 500
	}
	return 0
}

// Só o campo "model" da resposta; qualquer outra coisa (inclusive corpo inválido) vira "-".
func modelFromBody(body []byte) string {
	var r struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &r) != nil || r.Model == "" {
		return "-"
	}
	return r.Model
}

// Uma linha por requisição do /v1: nunca corpo, cabeçalho ou segredo.
func finishV1(alias, requested, returned string, status int, start time.Time) {
	recordUsage(alias, status)
	if alias == "" {
		alias = "-" // requisição recusada antes de achar a chave
	}
	if requested == "" {
		requested = "-"
	}
	if returned == "" {
		returned = "-"
	}
	log.Printf("[KeyHybrid] v1 alias=%q modelo=%q devolvido=%q status=%d duracao=%s\n",
		alias, requested, returned, status, time.Since(start).Round(time.Millisecond))
}
