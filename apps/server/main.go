package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

type KeyCreateRequest struct {
	Alias        string `json:"alias"`
	Provider     string `json:"provider"`
	SecretKey    string `json:"secret_key"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model"`
}

type KeySafeView struct {
	Id           string `json:"id"`
	Alias        string `json:"alias"`
	Provider     string `json:"provider"`
	MaskedKey    string `json:"masked_key"`
	VirtualToken string `json:"virtual_token"`
	DefaultModel string `json:"default_model"`
	Active       bool   `json:"active"`
}

func maskSecret(secret string) string {
	s := strings.TrimSpace(secret)
	if len(s) <= 8 {
		return "••••••••"
	}
	return s[:6] + "••••" + s[len(s)-4:]
}

// Chave mestra local de 256 bits para criptografia AES-256-GCM em repouso no SQLite (Windows & Linux)
func getOrCreateMasterKey() []byte {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".keyhybrid")
	_ = os.MkdirAll(dir, 0700)
	keyPath := filepath.Join(dir, ".master.key")

	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data
	}
	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		sum := md5.Sum([]byte("keyhybrid-fallback-seed"))
		return append(sum[:], sum[:]...)
	}
	_ = os.WriteFile(keyPath, newKey, 0600)
	return newKey
}

func encryptSecretAtRest(plain string) string {
	plain = strings.TrimSpace(plain)
	if plain == "" || strings.HasPrefix(plain, "enc:v1:") {
		return plain
	}
	key := getOrCreateMasterKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return plain
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plain
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return plain
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(sealed)
}

func decryptSecretAtRest(stored string) string {
	stored = strings.TrimSpace(stored)
	if !strings.HasPrefix(stored, "enc:v1:") {
		return stored
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, "enc:v1:"))
	if err != nil {
		return ""
	}
	key := getOrCreateMasterKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return ""
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func loadLocalXiaomiToken() string {
	if envTok := strings.TrimSpace(os.Getenv("XIAOMI_BULB_TOKEN")); len(envTok) == 32 {
		return envTok
	}
	if b, err := os.ReadFile(".xiaomi_token"); err == nil {
		if tok := strings.TrimSpace(string(b)); len(tok) == 32 {
			return tok
		}
	}
	return ""
}

func resolveProviderURL(provider string, customURL string) string {
	if customURL != "" {
		if !strings.HasSuffix(customURL, "/chat/completions") {
			return strings.TrimRight(customURL, "/") + "/chat/completions"
		}
		return customURL
	}
	switch strings.ToLower(provider) {
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	case "anthropic":
		return "https://openrouter.ai/api/v1/chat/completions"
	default:
		return "https://openrouter.ai/api/v1/chat/completions"
	}
}

func main() {
	app := pocketbase.New()

	// 1. Inicializar coleção "vault_keys" no SQLite
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}

		_, err := app.FindCollectionByNameOrId("vault_keys")
		if err != nil {
			log.Println("[KeyHybrid] Criando coleção 'vault_keys'...")
			collection := core.NewBaseCollection("vault_keys")

			collection.Fields.Add(&core.TextField{
				Name:     "provider",
				Required: true,
			})
			collection.Fields.Add(&core.TextField{
				Name:     "alias",
				Required: true,
			})
			collection.Fields.Add(&core.TextField{
				Name:     "secret_key",
				Required: true,
				Hidden:   true, // Jamais exposto na API padrão
			})
			collection.Fields.Add(&core.TextField{
				Name: "masked_key",
			})
			collection.Fields.Add(&core.TextField{
				Name: "base_url",
			})
			collection.Fields.Add(&core.TextField{
				Name: "default_model",
			})
			collection.Fields.Add(&core.BoolField{
				Name: "active",
			})

			collection.AddIndex("idx_vault_provider_alias", false, "provider, alias", "")

			if err := app.Save(collection); err != nil {
				log.Printf("[KeyHybrid] Erro ao criar coleção vault_keys: %v\n", err)
			}
		} else {
			col, err := app.FindCollectionByNameOrId("vault_keys")
			if err == nil && col.Fields.GetByName("masked_key") == nil {
				col.Fields.Add(&core.TextField{Name: "masked_key"})
				_ = app.Save(col)
			}
		}

		// Migrar automaticamente quaisquer chaves em texto puro no SQLite para AES-256-GCM (enc:v1:...)
		if recs, err := app.FindAllRecords("vault_keys"); err == nil {
			for _, r := range recs {
				raw := r.GetString("secret_key")
				if raw != "" && !strings.HasPrefix(raw, "enc:v1:") {
					if r.GetString("masked_key") == "" {
						r.Set("masked_key", maskSecret(raw))
					}
					r.Set("secret_key", encryptSecretAtRest(raw))
					_ = app.Save(r)
				}
			}
		}

		return nil
	})

	// 2. Rotas do Gateway e Gerenciamento Seguro de Chaves
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Permitir chamadas diretas do WebView local (CORS)
		e.Router.BindFunc(func(evt *core.RequestEvent) error {
			evt.Response.Header().Set("Access-Control-Allow-Origin", "*")
			evt.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			evt.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if evt.Request.Method == http.MethodOptions {
				return evt.NoContent(http.StatusNoContent)
			}
			return evt.Next()
		})

		// Health Check
		e.Router.GET("/api/keyhybrid/health", func(evt *core.RequestEvent) error {
			return evt.JSON(http.StatusOK, map[string]any{
				"status":    "healthy",
				"app":       "KeyHybrid Gateway",
				"timestamp": time.Now().Format(time.RFC3339),
			})
		})

		// Listar chaves salvas usando FindAllRecords (100% imune a erros de coluna/filtro)
		e.Router.GET("/api/keyhybrid/keys", func(evt *core.RequestEvent) error {
			records, err := app.FindAllRecords("vault_keys")
			if err != nil {
				log.Printf("[KeyHybrid] Erro ao listar vault_keys: %v\n", err)
				return evt.JSON(http.StatusOK, []KeySafeView{})
			}

			// Se houver chaves salvas e nenhuma estiver marcada como ativa, ativar a primeira automaticamente
			hasActive := false
			for _, rec := range records {
				if rec.GetBool("active") {
					hasActive = true
					break
				}
			}
			if !hasActive && len(records) > 0 {
				records[0].Set("active", true)
				_ = app.Save(records[0])
			}

			// Ordenar: chave ativa primeiro, depois por alias
			sort.SliceStable(records, func(i, j int) bool {
				ai := records[i].GetBool("active")
				aj := records[j].GetBool("active")
				if ai != aj {
					return ai
				}
				return records[i].GetString("alias") < records[j].GetString("alias")
			})

			result := make([]KeySafeView, 0, len(records))
			for _, rec := range records {
				alias := rec.GetString("alias")
				masked := rec.GetString("masked_key")
				if masked == "" {
					masked = maskSecret(decryptSecretAtRest(rec.GetString("secret_key")))
				}
				defaultModel := rec.GetString("default_model")
				if defaultModel == "" || defaultModel == "google/gemini-2.0-flash-001" {
					defaultModel = "openrouter/auto"
					rec.Set("default_model", defaultModel)
					_ = app.Save(rec)
				}
				result = append(result, KeySafeView{
					Id:           rec.Id,
					Alias:        alias,
					Provider:     rec.GetString("provider"),
					MaskedKey:    masked,
					VirtualToken: "kh-" + strings.ToLower(alias),
					DefaultModel: defaultModel,
					Active:       rec.GetBool("active"),
				})
			}

			return evt.JSON(http.StatusOK, result)
		})

		// Cadastrar ou atualizar uma chave pelo Alias
		e.Router.POST("/api/keyhybrid/keys", func(evt *core.RequestEvent) error {
			var req KeyCreateRequest
			if err := evt.BindBody(&req); err != nil {
				return evt.BadRequestError("Dados inválidos", err)
			}

			req.Alias = strings.TrimSpace(req.Alias)
			req.SecretKey = strings.TrimSpace(req.SecretKey)
			req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))

			if req.Alias == "" || req.SecretKey == "" {
				return evt.BadRequestError("O nome (alias) e a chave secreta são obrigatórios", nil)
			}
			if req.Provider == "" {
				req.Provider = "openrouter"
			}
			if req.DefaultModel == "" || req.DefaultModel == "google/gemini-2.0-flash-001" {
				req.DefaultModel = "openrouter/auto"
			}

			collection, err := app.FindCollectionByNameOrId("vault_keys")
			if err != nil {
				return evt.InternalServerError("Coleção vault_keys não encontrada", err)
			}

			allRecords, _ := app.FindAllRecords("vault_keys")
			var record *core.Record
			for _, r := range allRecords {
				if strings.EqualFold(r.GetString("alias"), req.Alias) {
					record = r
				} else if r.GetBool("active") {
					r.Set("active", false)
					_ = app.Save(r)
				}
			}

			if record == nil {
				record = core.NewRecord(collection)
			}

			record.Set("alias", req.Alias)
			record.Set("provider", req.Provider)
			record.Set("secret_key", encryptSecretAtRest(req.SecretKey))
			record.Set("masked_key", maskSecret(req.SecretKey))
			record.Set("base_url", req.BaseURL)
			record.Set("default_model", req.DefaultModel)
			record.Set("active", true)

			if err := app.Save(record); err != nil {
				return evt.InternalServerError("Erro ao salvar chave no cofre", err)
			}

			return evt.JSON(http.StatusOK, KeySafeView{
				Id:           record.Id,
				Alias:        req.Alias,
				Provider:     req.Provider,
				MaskedKey:    maskSecret(req.SecretKey),
				VirtualToken: "kh-" + strings.ToLower(req.Alias),
				DefaultModel: req.DefaultModel,
				Active:       true,
			})
		})

		// Ativar uma chave específica como padrão
		e.Router.POST("/api/keyhybrid/keys/{id}/activate", func(evt *core.RequestEvent) error {
			id := evt.Request.PathValue("id")
			target, err := app.FindRecordById("vault_keys", id)
			if err != nil {
				return evt.NotFoundError("Chave não encontrada", err)
			}

			allRecords, _ := app.FindAllRecords("vault_keys")
			for _, r := range allRecords {
				r.Set("active", r.Id == target.Id)
				_ = app.Save(r)
			}

			return evt.JSON(http.StatusOK, map[string]any{"status": "activated", "id": target.Id, "alias": target.GetString("alias")})
		})

		// Remover uma chave do cofre
		e.Router.DELETE("/api/keyhybrid/keys/{id}", func(evt *core.RequestEvent) error {
			id := evt.Request.PathValue("id")
			target, err := app.FindRecordById("vault_keys", id)
			if err != nil {
				return evt.NotFoundError("Chave não encontrada", err)
			}

			if err := app.Delete(target); err != nil {
				return evt.InternalServerError("Falha ao excluir chave", err)
			}

			return evt.JSON(http.StatusOK, map[string]any{"status": "deleted", "id": id})
		})

		// Handler Universal compatível com OpenAI / OpenRouter (/v1/chat/completions e /api/ai/proxy/chat)
		proxyChatHandler := func(evt *core.RequestEvent) error {
			bodyBytes, err := io.ReadAll(evt.Request.Body)
			if err != nil {
				return evt.BadRequestError("Falha ao ler corpo da requisição", err)
			}
			bodyBytes = bytes.TrimPrefix(bodyBytes, []byte("\xef\xbb\xbf"))

			var payload map[string]any
			if err := json.Unmarshal(bodyBytes, &payload); err != nil {
				return evt.BadRequestError("JSON inválido", err)
			}

			// Extrair alias do Header Authorization: Bearer <alias_ou_kh-alias> ou do próprio JSON
			authHeader := strings.TrimSpace(evt.Request.Header.Get("Authorization"))
			requestedAlias := ""
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				requestedAlias = strings.TrimSpace(authHeader[7:])
				requestedAlias = strings.TrimPrefix(strings.ToLower(requestedAlias), "kh-")
			}
			if a, ok := payload["alias"].(string); ok && a != "" {
				requestedAlias = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "kh-")
				delete(payload, "alias")
			}

			allRecords, _ := app.FindAllRecords("vault_keys")
			var matchedRecord *core.Record

			// 1. Tentar encontrar pelo alias solicitado
			if requestedAlias != "" {
				for _, r := range allRecords {
					if strings.EqualFold(r.GetString("alias"), requestedAlias) {
						matchedRecord = r
						break
					}
				}
			}

			// 2. Se não especificou alias, buscar a chave marcada como active = true
			if matchedRecord == nil {
				for _, r := range allRecords {
					if r.GetBool("active") {
						matchedRecord = r
						break
					}
				}
			}

			// 3. Fallback: pegar a primeira chave disponível no banco
			if matchedRecord == nil && len(allRecords) > 0 {
				matchedRecord = allRecords[0]
			}

			var secretKey string
			var provider string
			var baseURL string
			var defaultModel string

			if matchedRecord != nil {
				secretKey = decryptSecretAtRest(matchedRecord.GetString("secret_key"))
				provider = matchedRecord.GetString("provider")
				baseURL = matchedRecord.GetString("base_url")
				defaultModel = matchedRecord.GetString("default_model")
			} else {
				secretKey = os.Getenv("OPENROUTER_API_KEY")
				provider = "openrouter"
			}

			if secretKey == "" {
				return evt.BadRequestError(
					"Nenhuma chave cadastrada no KeyHybrid. Adicione sua chave na interface do KeyHybrid.",
					nil,
				)
			}

			// Garantir modelo padrão caso o cliente não tenha enviado
			if modelVal, ok := payload["model"].(string); !ok || strings.TrimSpace(modelVal) == "" || modelVal == "google/gemini-2.0-flash-001" {
				if defaultModel != "" && defaultModel != "google/gemini-2.0-flash-001" {
					payload["model"] = defaultModel
				} else {
					payload["model"] = "openrouter/auto"
				}
			}

			// Remover campo interno 'provider' apenas se for string de roteamento do KeyHybrid
			if _, isStr := payload["provider"].(string); isStr {
				delete(payload, "provider")
			}

			targetURL := resolveProviderURL(provider, baseURL)
			isStream, _ := payload["stream"].(bool)

			doUpstream := func(bodyMap map[string]any) (*http.Response, error) {
				forwardBytes, mErr := json.Marshal(bodyMap)
				if mErr != nil {
					return nil, mErr
				}
				httpReq, rErr := http.NewRequestWithContext(
					evt.Request.Context(),
					http.MethodPost,
					targetURL,
					bytes.NewReader(forwardBytes),
				)
				if rErr != nil {
					return nil, rErr
				}
				// Injeta a chave real de forma invisível para o terminal/cliente
				httpReq.Header.Set("Authorization", "Bearer "+secretKey)
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("HTTP-Referer", "https://keyhybrid.local")
				httpReq.Header.Set("X-Title", "KeyHybrid Vault Gateway")

				client := &http.Client{Timeout: 5 * time.Minute}
				return client.Do(httpReq)
			}

			upstreamResp, err := doUpstream(payload)
			if err != nil {
				return evt.InternalServerError("Erro de conexão com o provedor de IA", err)
			}

			// Se o modelo solicitado for bloqueado por Guardrail da OpenRouter (404), faz fallback inteligente para openrouter/auto
			if upstreamResp.StatusCode == http.StatusNotFound && (provider == "openrouter" || provider == "") && payload["model"] != "openrouter/auto" {
				_ = upstreamResp.Body.Close()
				requestedModel, _ := payload["model"].(string)
				payload["model"] = "openrouter/auto"
				// Remove modalidades incompatíveis caso o fallback vá para modelo de texto/código permitido pelo guardrail
				delete(payload, "modalities")
				delete(payload, "audio")
				upstreamResp, err = doUpstream(payload)
				if err != nil {
					return evt.InternalServerError("Erro de conexão no fallback da OpenRouter", err)
				}
				evt.Response.Header().Set("X-KeyHybrid-Fallback-From", requestedModel)
				evt.Response.Header().Set("X-KeyHybrid-Fallback-To", "openrouter/auto")
			}
			defer upstreamResp.Body.Close()

			for k, v := range upstreamResp.Header {
				evt.Response.Header()[k] = v
			}
			evt.Response.WriteHeader(upstreamResp.StatusCode)

			if isStream {
				flusher, isFlusher := evt.Response.(http.Flusher)
				buf := make([]byte, 1024)
				for {
					n, readErr := upstreamResp.Body.Read(buf)
					if n > 0 {
						if _, writeErr := evt.Response.Write(buf[:n]); writeErr != nil {
							break
						}
						if isFlusher {
							flusher.Flush()
						}
					}
					if readErr != nil {
						break
					}
				}
				return nil
			}

			_, err = io.Copy(evt.Response, upstreamResp.Body)
			return err
		}

		// Expor tanto no padrão OpenAI (/v1/chat/completions) quanto na rota interna
		e.Router.POST("/v1/chat/completions", proxyChatHandler)
		e.Router.POST("/api/ai/proxy/chat", proxyChatHandler)

		// Helper para resolver a chave secreta real a partir do token virtual Bearer kh-<alias>
		resolveVaultSecret := func(evt *core.RequestEvent) (string, error) {
			authHeader := strings.TrimSpace(evt.Request.Header.Get("Authorization"))
			requestedAlias := ""
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				requestedAlias = strings.TrimSpace(authHeader[7:])
				requestedAlias = strings.TrimPrefix(strings.ToLower(requestedAlias), "kh-")
			}
			allRecords, _ := app.FindAllRecords("vault_keys")
			var matchedRecord *core.Record
			if requestedAlias != "" {
				for _, r := range allRecords {
					if strings.EqualFold(r.GetString("alias"), requestedAlias) {
						matchedRecord = r
						break
					}
				}
			}
			if matchedRecord == nil {
				for _, r := range allRecords {
					if r.GetBool("active") {
						matchedRecord = r
						break
					}
				}
			}
			if matchedRecord == nil && len(allRecords) > 0 {
				matchedRecord = allRecords[0]
			}
			if matchedRecord != nil {
				return decryptSecretAtRest(matchedRecord.GetString("secret_key")), nil
			}
			return os.Getenv("OPENROUTER_API_KEY"), nil
		}

		// Proxy Universal para Text-to-Speech (HeyGen Voice Turbo, ElevenLabs, etc.) -> POST /v1/audio/speech
		e.Router.POST("/v1/audio/speech", func(evt *core.RequestEvent) error {
			secretKey, _ := resolveVaultSecret(evt)
			if secretKey == "" {
				return evt.BadRequestError("Nenhuma chave cadastrada no KeyHybrid", nil)
			}
			bodyBytes, err := io.ReadAll(evt.Request.Body)
			if err != nil {
				return evt.BadRequestError("Falha ao ler corpo", err)
			}
			bodyBytes = bytes.TrimPrefix(bodyBytes, []byte("\xef\xbb\xbf"))

			httpReq, err := http.NewRequestWithContext(
				evt.Request.Context(),
				http.MethodPost,
				"https://openrouter.ai/api/v1/audio/speech",
				bytes.NewReader(bodyBytes),
			)
			if err != nil {
				return evt.InternalServerError("Erro ao criar requisição de áudio", err)
			}
			httpReq.Header.Set("Authorization", "Bearer "+secretKey)
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("HTTP-Referer", "https://keyhybrid.local")
			httpReq.Header.Set("X-Title", "KeyHybrid Vault Gateway")

			client := &http.Client{Timeout: 5 * time.Minute}
			resp, err := client.Do(httpReq)
			if err != nil {
				return evt.InternalServerError("Erro na chamada de áudio da OpenRouter", err)
			}
			defer resp.Body.Close()

			for k, v := range resp.Header {
				evt.Response.Header()[k] = v
			}
			evt.Response.WriteHeader(resp.StatusCode)
			_, err = io.Copy(evt.Response, resp.Body)
			return err
		})

		// Proxy Universal para Geração de Vídeo Assíncrona (HeyGen Video, Veo, Seedance, Wan) -> POST /v1/videos
		e.Router.POST("/v1/videos", func(evt *core.RequestEvent) error {
			secretKey, _ := resolveVaultSecret(evt)
			if secretKey == "" {
				return evt.BadRequestError("Nenhuma chave cadastrada no KeyHybrid", nil)
			}
			bodyBytes, err := io.ReadAll(evt.Request.Body)
			if err != nil {
				return evt.BadRequestError("Falha ao ler corpo", err)
			}
			bodyBytes = bytes.TrimPrefix(bodyBytes, []byte("\xef\xbb\xbf"))

			httpReq, err := http.NewRequestWithContext(
				evt.Request.Context(),
				http.MethodPost,
				"https://openrouter.ai/api/v1/videos",
				bytes.NewReader(bodyBytes),
			)
			if err != nil {
				return evt.InternalServerError("Erro ao criar requisição de vídeo", err)
			}
			httpReq.Header.Set("Authorization", "Bearer "+secretKey)
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("HTTP-Referer", "https://keyhybrid.local")
			httpReq.Header.Set("X-Title", "KeyHybrid Vault Gateway")

			client := &http.Client{Timeout: 5 * time.Minute}
			resp, err := client.Do(httpReq)
			if err != nil {
				return evt.InternalServerError("Erro na chamada de vídeo da OpenRouter", err)
			}
			defer resp.Body.Close()

			for k, v := range resp.Header {
				evt.Response.Header()[k] = v
			}
			evt.Response.WriteHeader(resp.StatusCode)
			_, err = io.Copy(evt.Response, resp.Body)
			return err
		})

		// Consulta de Status de Vídeo Assíncrono -> GET /v1/videos/{id}
		e.Router.GET("/v1/videos/{id}", func(evt *core.RequestEvent) error {
			secretKey, _ := resolveVaultSecret(evt)
			if secretKey == "" {
				return evt.BadRequestError("Nenhuma chave cadastrada no KeyHybrid", nil)
			}
			jobId := evt.Request.PathValue("id")
			httpReq, err := http.NewRequestWithContext(
				evt.Request.Context(),
				http.MethodGet,
				"https://openrouter.ai/api/v1/videos/"+jobId,
				nil,
			)
			if err != nil {
				return evt.InternalServerError("Erro ao consultar vídeo", err)
			}
			httpReq.Header.Set("Authorization", "Bearer "+secretKey)
			httpReq.Header.Set("HTTP-Referer", "https://keyhybrid.local")
			httpReq.Header.Set("X-Title", "KeyHybrid Vault Gateway")

			client := &http.Client{Timeout: 2 * time.Minute}
			resp, err := client.Do(httpReq)
			if err != nil {
				return evt.InternalServerError("Erro ao consultar status do vídeo na OpenRouter", err)
			}
			defer resp.Body.Close()

			for k, v := range resp.Header {
				evt.Response.Header()[k] = v
			}
			evt.Response.WriteHeader(resp.StatusCode)
			_, err = io.Copy(evt.Response, resp.Body)
			return err
		})

		// Download do arquivo binário de vídeo gerado (.mp4) -> GET /v1/videos/{id}/content
		e.Router.GET("/v1/videos/{id}/content", func(evt *core.RequestEvent) error {
			secretKey, _ := resolveVaultSecret(evt)
			if secretKey == "" {
				return evt.BadRequestError("Nenhuma chave cadastrada no KeyHybrid", nil)
			}
			jobId := evt.Request.PathValue("id")
			query := evt.Request.URL.RawQuery
			target := "https://openrouter.ai/api/v1/videos/" + jobId + "/content"
			if query != "" {
				target += "?" + query
			}
			httpReq, err := http.NewRequestWithContext(
				evt.Request.Context(),
				http.MethodGet,
				target,
				nil,
			)
			if err != nil {
				return evt.InternalServerError("Erro ao baixar vídeo", err)
			}
			httpReq.Header.Set("Authorization", "Bearer "+secretKey)
			httpReq.Header.Set("HTTP-Referer", "https://keyhybrid.local")
			httpReq.Header.Set("X-Title", "KeyHybrid Vault Gateway")

			client := &http.Client{Timeout: 5 * time.Minute}
			resp, err := client.Do(httpReq)
			if err != nil {
				return evt.InternalServerError("Erro ao baixar vídeo da OpenRouter", err)
			}
			defer resp.Body.Close()

			for k, v := range resp.Header {
				evt.Response.Header()[k] = v
			}
			evt.Response.WriteHeader(resp.StatusCode)
			_, err = io.Copy(evt.Response, resp.Body)
			return err
		})

		// Listagem dos modelos permitidos pela chave do usuário -> GET /v1/models
		e.Router.GET("/v1/models", func(evt *core.RequestEvent) error {
			secretKey, _ := resolveVaultSecret(evt)
			if secretKey == "" {
				return evt.BadRequestError("Nenhuma chave cadastrada no KeyHybrid", nil)
			}
			httpReq, err := http.NewRequestWithContext(
				evt.Request.Context(),
				http.MethodGet,
				"https://openrouter.ai/api/v1/models/user",
				nil,
			)
			if err != nil {
				return evt.InternalServerError("Erro ao listar modelos", err)
			}
			httpReq.Header.Set("Authorization", "Bearer "+secretKey)
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(httpReq)
			if err != nil {
				return evt.InternalServerError("Erro na consulta de modelos", err)
			}
			defer resp.Body.Close()
			for k, v := range resp.Header {
				evt.Response.Header()[k] = v
			}
			evt.Response.WriteHeader(resp.StatusCode)
			_, err = io.Copy(evt.Response, resp.Body)
			return err
		})

		// Helper para descobrir lâmpada Xiaomi / Yeelight na rede local via SSDP (UDP 1982)
		discoverXiaomiBulbIP := func() string {
			conn, err := net.ListenPacket("udp4", ":0")
			if err != nil {
				return ""
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(1200 * time.Millisecond))
			dst, err := net.ResolveUDPAddr("udp4", "239.255.255.250:1982")
			if err != nil {
				return ""
			}
			msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1982\r\nMAN: \"ssdp:discover\"\r\nST: wifi_bulb\r\n\r\n"
			_, _ = conn.WriteTo([]byte(msg), dst)
			buf := make([]byte, 2048)
			n, addr, err := conn.ReadFrom(buf)
			if err == nil && n > 0 && addr != nil {
				if udpAddr, ok := addr.(*net.UDPAddr); ok {
					return udpAddr.IP.String()
				}
			}
			return ""
		}

		// Envia comando UDP 54321 criptografado (AES-128-CBC) no protocolo nativo Xiaomi MiIO / MIoT
		sendXiaomiMiioUDP := func(ip string, tokenHex string, method string, params any) error {
			tokenBytes, err := hex.DecodeString(strings.TrimSpace(tokenHex))
			if err != nil || len(tokenBytes) != 16 {
				return fmt.Errorf("token Xiaomi inválido (deve ter 32 caracteres hexadecimais)")
			}

			udpAddr, err := net.ResolveUDPAddr("udp4", ip+":54321")
			if err != nil {
				return err
			}
			conn, err := net.DialUDP("udp4", nil, udpAddr)
			if err != nil {
				return err
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

			// 1. Handshake Hello Packet (32 bytes)
			hello := make([]byte, 32)
			hello[0] = 0x21
			hello[1] = 0x31
			hello[2] = 0x00
			hello[3] = 0x20
			for i := 4; i < 32; i++ {
				hello[i] = 0xff
			}
			if _, err := conn.Write(hello); err != nil {
				return err
			}

			resp := make([]byte, 1024)
			n, err := conn.Read(resp)
			if err != nil || n < 32 {
				return fmt.Errorf("sem resposta UDP 54321 da lâmpada em %s", ip)
			}
			deviceID := resp[8:12]
			stamp := binary.BigEndian.Uint32(resp[12:16])

			// 2. Derivar Key = MD5(token) e IV = MD5(Key + token)
			keySum := md5.Sum(tokenBytes)
			key := keySum[:]
			ivInput := append(append([]byte{}, key...), tokenBytes...)
			ivSum := md5.Sum(ivInput)
			iv := ivSum[:]

			// 3. Montar JSON RPC e aplicar PKCS#7 padding
			payloadMap := map[string]any{
				"id":     int(time.Now().UnixNano()/1e5)%800000 + 100,
				"method": method,
				"params": params,
			}
			plain, _ := json.Marshal(payloadMap)
			plain = append(plain, 0x00)
			padLen := aes.BlockSize - (len(plain) % aes.BlockSize)
			for i := 0; i < padLen; i++ {
				plain = append(plain, byte(padLen))
			}

			block, err := aes.NewCipher(key)
			if err != nil {
				return err
			}
			ciphertext := make([]byte, len(plain))
			cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plain)

			// 4. Montar cabeçalho MiIO + MD5 Checksum
			totalLen := uint16(32 + len(ciphertext))
			pkt := make([]byte, 32+len(ciphertext))
			pkt[0] = 0x21
			pkt[1] = 0x31
			binary.BigEndian.PutUint16(pkt[2:4], totalLen)
			copy(pkt[8:12], deviceID)
			binary.BigEndian.PutUint32(pkt[12:16], stamp+1)
			copy(pkt[16:32], tokenBytes)
			copy(pkt[32:], ciphertext)

			chk := md5.Sum(pkt)
			copy(pkt[16:32], chk[:])

			if _, err = conn.Write(pkt); err != nil {
				return err
			}
			_, _ = conn.Read(resp)
			return nil
		}
		sendXiaomiLanCmd := func(ip string, method string, params []any) error {
			if ip == "" {
				return fmt.Errorf("IP da lâmpada Xiaomi não informado")
			}
			target := ip
			if !strings.Contains(target, ":") {
				target = target + ":55443"
			}
			conn, err := net.DialTimeout("tcp", target, 1800*time.Millisecond)
			if err != nil {
				return err
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			cmd := map[string]any{
				"id":     1,
				"method": method,
				"params": params,
			}
			b, _ := json.Marshal(cmd)
			b = append(b, '\r', '\n')
			_, err = conn.Write(b)
			return err
		}

		// Sintetiza a voz oficial da Assistente Sexta-Feira (F.R.I.D.A.Y. -> elevenlabs/eleven-v4-turbo, voice: alice) com cache em memória
		ttsCache := map[string]string{}
		synthesizeFridayVoice := func(ctx *core.RequestEvent, secretKey string, text string) string {
			cacheKey := strings.TrimSpace(text)
			if cached, ok := ttsCache[cacheKey]; ok && cached != "" {
				return cached
			}
			ttsBody, _ := json.Marshal(map[string]any{
				"model":           "elevenlabs/eleven-v4-turbo",
				"input":           cacheKey,
				"voice":           "alice",
				"response_format": "mp3",
			})
			req, err := http.NewRequestWithContext(
				ctx.Request.Context(),
				http.MethodPost,
				"https://openrouter.ai/api/v1/audio/speech",
				bytes.NewReader(ttsBody),
			)
			if err != nil {
				return ""
			}
			req.Header.Set("Authorization", "Bearer "+secretKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("HTTP-Referer", "https://keyhybrid.local")
			req.Header.Set("X-Title", "KeyHybrid Friday Assistant")

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return ""
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return ""
			}
			audioBytes, err := io.ReadAll(resp.Body)
			if err != nil || len(audioBytes) == 0 {
				return ""
			}
			b64 := base64.StdEncoding.EncodeToString(audioBytes)
			ttsCache[cacheKey] = b64
			return b64
		}

		// Estado em memória da Assistente Sexta-Feira (sincronizado entre o microfone nativo e a UI Tauri)
		fridayEnabledState := true
		var fridayMuteUntil time.Time
		lastFridayEvent := map[string]any{
			"event_id":     int64(0),
			"text":         "",
			"reply":        "",
			"action":       "none",
			"light_status": "standby",
			"audio_base64": "",
		}

		e.Router.GET("/api/keyhybrid/friday/state", func(evt *core.RequestEvent) error {
			secretKey, _ := resolveVaultSecret(evt)
			return evt.JSON(http.StatusOK, map[string]any{
				"enabled":        fridayEnabledState && secretKey != "",
				"muted":          time.Now().Before(fridayMuteUntil),
				"has_openrouter": secretKey != "",
				"event":          lastFridayEvent,
			})
		})

		e.Router.POST("/api/keyhybrid/friday/state", func(evt *core.RequestEvent) error {
			var body struct {
				Enabled *bool `json:"enabled"`
				MuteMs  int   `json:"mute_ms"`
			}
			b, _ := io.ReadAll(evt.Request.Body)
			_ = json.Unmarshal(b, &body)
			if body.Enabled != nil {
				fridayEnabledState = *body.Enabled
			}
			if body.MuteMs > 0 {
				fridayMuteUntil = time.Now().Add(time.Duration(body.MuteMs) * time.Millisecond)
			}
			return evt.JSON(http.StatusOK, map[string]any{
				"enabled": fridayEnabledState,
				"muted":   time.Now().Before(fridayMuteUntil),
			})
		})

		// Rota da Assistente Padrão "Sexta-Feira" (F.R.I.D.A.Y.) integrada ao KeyHybrid + Smart Home Xiaomi
		e.Router.POST("/api/keyhybrid/friday/assist", func(evt *core.RequestEvent) error {
			secretKey, _ := resolveVaultSecret(evt)
			if secretKey == "" {
				return evt.BadRequestError("A Sexta-Feira requer uma chave da OpenRouter cadastrada no KeyHybrid.", nil)
			}

			var req struct {
				Text     string `json:"text"`
				BulbIP   string `json:"bulb_ip"`
				Greeting bool   `json:"greeting"`
			}
			bodyBytes, _ := io.ReadAll(evt.Request.Body)
			bodyBytes = bytes.TrimPrefix(bodyBytes, []byte("\xef\xbb\xbf"))
			_ = json.Unmarshal(bodyBytes, &req)

			// Caso seja apenas a ativação da Sexta-Feira na interface
			if req.Greeting {
				fridayEnabledState = true
				greetText := "Sexta-Feira online, chefe. Estou na escuta. Quando quiser, basta me chamar pelo nome."
				audioB64 := synthesizeFridayVoice(evt, secretKey, greetText)
				fridayMuteUntil = time.Now().Add(5 * time.Second)
				return evt.JSON(http.StatusOK, map[string]any{
					"reply":        greetText,
					"audio_base64": audioB64,
					"light_status": "standby",
				})
			}

			userText := strings.TrimSpace(req.Text)
			if userText == "" {
				return evt.BadRequestError("Comando de voz vazio", nil)
			}

			var parsed struct {
				Reply      string `json:"reply"`
				Action     string `json:"action"`
				Brightness int    `json:"brightness"`
				RGB        int    `json:"rgb"`
			}

			// Fast-path determinístico instantâneo para comandos de iluminação (executa em <10ms sem esperar LLM)
			lowerCmd := strings.ToLower(userText)
			fastMatched := true
			if strings.Contains(lowerCmd, "apag") || strings.Contains(lowerCmd, "deslig") || strings.Contains(lowerCmd, "escur") {
				parsed.Action = "power_off"
				parsed.Reply = "Luz apagada, chefe. Modo furtivo ativado."
			} else if strings.Contains(lowerCmd, "azul") {
				parsed.Action = "set_color"
				parsed.RGB = 255
				parsed.Reply = "Azul ajustado na lâmpada, chefe."
			} else if strings.Contains(lowerCmd, "vermelh") {
				parsed.Action = "set_color"
				parsed.RGB = 16711680
				parsed.Reply = " Luz vermelha ativada, chefe. Clima de alerta máximo."
			} else if strings.Contains(lowerCmd, "verd") {
				parsed.Action = "set_color"
				parsed.RGB = 65280
				parsed.Reply = "Luz verde na área, chefe."
			} else if strings.Contains(lowerCmd, "rox") || strings.Contains(lowerCmd, "violet") {
				parsed.Action = "set_color"
				parsed.RGB = 9055202
				parsed.Reply = "Tom roxo aplicado na iluminação, chefe."
			} else if strings.Contains(lowerCmd, "amarel") || strings.Contains(lowerCmd, "quent") || strings.Contains(lowerCmd, "dourad") {
				parsed.Action = "set_color"
				parsed.RGB = 16761095
				parsed.Reply = "Luz quente ajustada, chefe."
			} else if strings.Contains(lowerCmd, "cian") {
				parsed.Action = "set_color"
				parsed.RGB = 65535
				parsed.Reply = "Luz ciano ativada, chefe."
			} else if strings.Contains(lowerCmd, "branc") || strings.Contains(lowerCmd, "normal") || strings.Contains(lowerCmd, "clara") {
				parsed.Action = "set_color"
				parsed.RGB = 16777215
				parsed.Reply = "Luz branca restaurada no máximo, chefe."
			} else if strings.Contains(lowerCmd, "acend") || strings.Contains(lowerCmd, "ligar") || strings.Contains(lowerCmd, "ligue") || strings.Contains(lowerCmd, "liga") {
				parsed.Action = "power_on"
				parsed.Reply = "Luz acesa, chefe."
			} else {
				fastMatched = false
			}

			if !fastMatched {
				systemPrompt := `Você é a "Sexta-Feira" (F.R.I.D.A.Y.), a assistente de inteligência artificial tática oficial integrada ao cofre KeyHybrid do usuário (trate-o como "chefe").
Você controla dispositivos inteligentes da casa dele, especialmente a Lâmpada Inteligente da Xiaomi.
Responda SEMPRE APENAS um objeto JSON válido (sem blocos markdown) no seguinte formato exato:
{
  "reply": "Sua resposta curta, natural e afiada em português como a Sexta-Feira do Homem de Ferro (1 a 2 frases)",
  "action": "none" | "power_on" | "power_off" | "set_color" | "set_brightness",
  "brightness": 100,
  "rgb": 16777215
}
Regras para "rgb" (inteiro decimal de 0xRRGGBB):
- Vermelho: 16711680 | Verde: 65280 | Azul: 255 | Roxo/Violeta: 9055202 | Amarelo/Quente: 16761095 | Ciano: 65535 | Branco: 16777215.`

				chatBody, _ := json.Marshal(map[string]any{
					"model": "openrouter/auto",
					"messages": []map[string]string{
						{"role": "system", "content": systemPrompt},
						{"role": "user", "content": userText},
					},
					"max_tokens": 250,
				})

				httpReq, err := http.NewRequestWithContext(
					evt.Request.Context(),
					http.MethodPost,
					"https://openrouter.ai/api/v1/chat/completions",
					bytes.NewReader(chatBody),
				)
				if err == nil {
					httpReq.Header.Set("Authorization", "Bearer "+secretKey)
					httpReq.Header.Set("Content-Type", "application/json")
					httpReq.Header.Set("HTTP-Referer", "https://keyhybrid.local")
					httpReq.Header.Set("X-Title", "KeyHybrid Friday Assistant")

					client := &http.Client{Timeout: 30 * time.Second}
					if resp, err := client.Do(httpReq); err == nil {
						defer resp.Body.Close()
						var orResp struct {
							Choices []struct {
								Message struct {
									Content string `json:"content"`
								} `json:"message"`
							} `json:"choices"`
						}
						_ = json.NewDecoder(resp.Body).Decode(&orResp)
						rawContent := ""
						if len(orResp.Choices) > 0 {
							rawContent = strings.TrimSpace(orResp.Choices[0].Message.Content)
							rawContent = strings.TrimPrefix(rawContent, "```json")
							rawContent = strings.TrimPrefix(rawContent, "```")
							rawContent = strings.TrimSuffix(rawContent, "```")
							rawContent = strings.TrimSpace(rawContent)
						}
						if err := json.Unmarshal([]byte(rawContent), &parsed); err != nil || parsed.Reply == "" {
							parsed.Reply = rawContent
							if parsed.Reply == "" {
								parsed.Reply = "Na escuta, chefe. Comando processado."
							}
							parsed.Action = "none"
						}
					}
				}
			}

			lightStatus := "no_action"
			bulbIP := strings.TrimSpace(req.BulbIP)
			xiaomiToken := ""

			// Verificar se já existe o token de 32 caracteres da lâmpada Xiaomi salvo no cofre (alias: xiaomi)
			allRecs, _ := app.FindAllRecords("vault_keys")
			for _, r := range allRecs {
				if strings.EqualFold(r.GetString("alias"), "xiaomi") || strings.EqualFold(r.GetString("alias"), "lampada") {
					xiaomiToken = strings.TrimSpace(decryptSecretAtRest(r.GetString("secret_key")))
					if r.GetString("base_url") != "" && bulbIP == "" {
						bulbIP = r.GetString("base_url")
					}
					break
				}
			}

			// Se o usuário digitou o token de 32 caracteres direto no campo IP
			if len(bulbIP) == 32 && !strings.Contains(bulbIP, ".") {
				xiaomiToken = bulbIP
				bulbIP = "192.168.100.22"
			}
			if xiaomiToken == "" {
				xiaomiToken = loadLocalXiaomiToken()
			}
			if bulbIP == "" {
				bulbIP = discoverXiaomiBulbIP()
				if bulbIP == "" {
					bulbIP = "192.168.100.22"
				}
			}

			if parsed.Action != "" && parsed.Action != "none" {
				if xiaomiToken != "" {
					var cmdErr error
					switch parsed.Action {
					case "power_on":
						cmdErr = sendXiaomiMiioUDP(bulbIP, xiaomiToken, "set_power", []any{"on", "smooth", 400})
					case "power_off":
						cmdErr = sendXiaomiMiioUDP(bulbIP, xiaomiToken, "set_power", []any{"off", "smooth", 400})
					case "set_brightness":
						b := parsed.Brightness
						if b < 1 {
							b = 50
						}
						if b > 100 {
							b = 100
						}
						_ = sendXiaomiMiioUDP(bulbIP, xiaomiToken, "set_power", []any{"on", "smooth", 300})
						cmdErr = sendXiaomiMiioUDP(bulbIP, xiaomiToken, "set_bright", []any{b, "smooth", 400})
					case "set_color":
						rgb := parsed.RGB
						if rgb <= 0 {
							rgb = 255
						}
						_ = sendXiaomiMiioUDP(bulbIP, xiaomiToken, "set_power", []any{"on", "smooth", 300})
						if rgb == 16777215 {
							cmdErr = sendXiaomiMiioUDP(bulbIP, xiaomiToken, "set_ct_abx", []any{6500, "smooth", 400})
						} else {
							cmdErr = sendXiaomiMiioUDP(bulbIP, xiaomiToken, "set_rgb", []any{rgb, "smooth", 400})
						}
					}
					if cmdErr == nil {
						lightStatus = "miio_udp_executed_on_" + bulbIP
					} else {
						lightStatus = "miio_udp_error: " + cmdErr.Error()
					}
				} else {
					var cmdErr error
					switch parsed.Action {
					case "power_on":
						cmdErr = sendXiaomiLanCmd(bulbIP, "set_power", []any{"on", "smooth", 400})
					case "power_off":
						cmdErr = sendXiaomiLanCmd(bulbIP, "set_power", []any{"off", "smooth", 400})
					case "set_brightness":
						b := parsed.Brightness
						if b < 1 {
							b = 50
						}
						if b > 100 {
							b = 100
						}
						_ = sendXiaomiLanCmd(bulbIP, "set_power", []any{"on", "smooth", 300})
						cmdErr = sendXiaomiLanCmd(bulbIP, "set_bright", []any{b, "smooth", 400})
					case "set_color":
						rgb := parsed.RGB
						if rgb <= 0 {
							rgb = 255
						}
						_ = sendXiaomiLanCmd(bulbIP, "set_power", []any{"on", "smooth", 300})
						cmdErr = sendXiaomiLanCmd(bulbIP, "set_rgb", []any{rgb, "smooth", 400})
					}
					if cmdErr == nil {
						lightStatus = "executed_on_" + bulbIP
					} else {
						lightStatus = "needs_xiaomi_token_for_" + bulbIP
					}
				}
			}

			audioB64 := synthesizeFridayVoice(evt, secretKey, parsed.Reply)

			evID := time.Now().UnixMilli()
			lastFridayEvent = map[string]any{
				"event_id":     evID,
				"text":         userText,
				"reply":        parsed.Reply,
				"action":       parsed.Action,
				"brightness":   parsed.Brightness,
				"rgb":          parsed.RGB,
				"bulb_ip":      bulbIP,
				"light_status": lightStatus,
				"audio_base64": audioB64,
			}

			return evt.JSON(http.StatusOK, lastFridayEvent)
		})

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
