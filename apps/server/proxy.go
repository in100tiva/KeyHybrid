package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var hopByHopHeaders = map[string]bool{
	"connection": true, "keep-alive": true, "proxy-authenticate": true, "proxy-authorization": true,
	"te": true, "trailer": true, "transfer-encoding": true, "upgrade": true,
}

// Cabeçalhos do cliente que nunca seguem para a API externa
var stripRequestHeaders = map[string]bool{
	"authorization": true, "cookie": true, "host": true, "origin": true,
	"referer": true, "x-keyhybrid-token": true, "content-length": true,
}

// Monta o destino; recusa caminho que saia do prefixo do base_url (o ServeMux já limpa "..", isto é a segunda trava)
func buildProxyTarget(base, rest, rawQuery string) (string, error) {
	bu, err := url.Parse(base)
	if err != nil || bu.Scheme != "https" || bu.Hostname() == "" || bu.User != nil || bu.RawQuery != "" || bu.Fragment != "" {
		return "", errors.New("base_url inválida")
	}
	low := strings.ToLower(rest)
	if strings.HasPrefix(rest, "/") || strings.Contains(rest, "//") || strings.ContainsAny(rest, "@\\") ||
		strings.Contains(low, "%2f") || strings.Contains(low, "%2e") || strings.Contains(low, "%5c") {
		return "", errors.New("caminho fora do destino")
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "." || seg == ".." {
			return "", errors.New("caminho fora do destino")
		}
	}
	target := strings.TrimRight(base, "/") + "/" + rest
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	tu, err := url.Parse(target)
	if err != nil || tu.Scheme != bu.Scheme || tu.Host != bu.Host || tu.Fragment != "" || !strings.HasPrefix(tu.EscapedPath(), bu.EscapedPath()) {
		return "", errors.New("caminho fora do destino")
	}
	return target, nil
}

// Repassa a requisição injetando o segredo; não segue redirect (cabeçalho como apikey vazaria para outro host)
func forwardToService(w http.ResponseWriter, r *http.Request, target, authName, authValue string) int {
	if !isAllowedAuthHeader(authName) {
		http.Error(w, "cabeçalho de autenticação proibido", http.StatusBadRequest)
		return http.StatusBadRequest
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		http.Error(w, "destino inválido", http.StatusBadRequest)
		return http.StatusBadRequest
	}
	req.ContentLength = r.ContentLength
	for k, vs := range r.Header {
		lk := strings.ToLower(k)
		if stripRequestHeaders[lk] || hopByHopHeaders[lk] || strings.HasPrefix(lk, "proxy-") || strings.EqualFold(k, authName) {
			continue
		}
		req.Header[k] = vs
	}
	req.Header.Set(authName, authValue)

	client := &http.Client{
		Timeout:       120 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "falha ao falar com o serviço", http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		lk := strings.ToLower(k)
		if lk == "set-cookie" || hopByHopHeaders[lk] || strings.HasPrefix(lk, "access-control-") {
			continue
		}
		w.Header()[k] = vs
	}
	w.WriteHeader(resp.StatusCode)
	copyStream(w, resp.Body)
	return resp.StatusCode
}

// Corpo em streaming: cada leitura sai na hora (SSE, download grande)
func copyStream(w http.ResponseWriter, src io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
