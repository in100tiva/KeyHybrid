package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/hook"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/router"
)

var (
	jobIdRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

	// version é injetada no build de release (-ldflags "-X main.version=...")
	// e serve para suporte: `./keyhybrid-server --version`.
	version = "dev"
)

type KeyCreateRequest struct {
	Alias        string `json:"alias"`
	Provider     string `json:"provider"`
	SecretKey    string `json:"secret_key"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model"`
	Kind         string `json:"kind"`
	AuthHeader   string `json:"auth_header"`
	AuthPrefix   string `json:"auth_prefix"`
}

type KeySafeView struct {
	Id           string `json:"id"`
	Alias        string `json:"alias"`
	Provider     string `json:"provider"`
	MaskedKey    string `json:"masked_key"`
	VirtualToken string `json:"virtual_token"`
	DefaultModel string `json:"default_model"`
	Active       bool   `json:"active"`
	Kind         string `json:"kind"`
	BaseURL      string `json:"base_url"`
	AuthHeader   string `json:"auth_header"`
}

// O que a listagem mostra sem token: provider, base_url e auth_header dariam
// a qualquer processo local o mapa de como usar cada chave.
type KeyPublicView struct {
	Id    string `json:"id"`
	Alias string `json:"alias"`
	Kind  string `json:"kind"`
}

func isServiceKey(rec *core.Record) bool {
	return rec.GetString("kind") == "service"
}

// Erro de controle: a rota /v1 já respondeu (401/404) e o chamador só precisa parar.
var errV1Respondido = errors.New("resposta /v1 já enviada")

// Só chave de IA: usada por /v1 e auto-ativação
func onlyAI(recs []*core.Record) []*core.Record {
	var out []*core.Record
	for _, r := range recs {
		if !isServiceKey(r) {
			out = append(out, r)
		}
	}
	return out
}

func listView(rec *core.Record, authorized bool) any {
	if !authorized {
		kind := "ai"
		if isServiceKey(rec) {
			kind = "service"
		}
		return KeyPublicView{Id: rec.Id, Alias: rec.GetString("alias"), Kind: kind}
	}
	return keyView(rec)
}

func keyView(rec *core.Record) KeySafeView {
	masked := "••••••••"
	if rec.GetString("secret_key") != "" {
		// segredo que não decifra fica só com a máscara; quem usa o segredo responde 500
		if plain, err := decryptSecretAtRest(rec.GetString("secret_key")); err == nil {
			masked = maskSecret(plain)
		}
	}
	v := KeySafeView{
		Id:         rec.Id,
		Alias:      rec.GetString("alias"),
		Provider:   rec.GetString("provider"),
		MaskedKey:  masked,
		Active:     rec.GetBool("active"),
		Kind:       "ai",
		AuthHeader: rec.GetString("auth_header"),
	}
	if isServiceKey(rec) {
		v.Kind = "service"
		v.BaseURL = rec.GetString("base_url")
		return v
	}
	v.VirtualToken = "kh-" + strings.ToLower(v.Alias)
	v.DefaultModel = rec.GetString("default_model")
	return v
}

func maskSecret(secret string) string {
	s := strings.TrimSpace(secret)
	if len(s) <= 8 {
		return "••••••••"
	}
	if len(s) <= 12 {
		return "••••••••"
	}
	return "••••••••" + s[len(s)-4:]
}

// Chave mestra local de 256 bits (AES-256-GCM em repouso no SQLite), lida uma vez e mantida em memória
var (
	chaveMestraMu sync.RWMutex
	chaveMestra   []byte
)

// carregarChaveMestra roda no boot (e ao abrir o cofre pela CLI), já com o dataDir real.
// Sem a chave e com <dataDir>/data.db no disco, recusa: chave nova deixaria o cofre inteiro ilegível.
func carregarChaveMestra(dataDir string) error {
	dir := filepath.Join(homeDir(), ".keyhybrid")
	if !chaveNoDisco(dir) && existeOuIncerto(filepath.Join(dataDir, "data.db")) {
		// o vault-apply também cai aqui: restaurar o .khvault exige tirar o cofre ilegível do caminho
		return fmt.Errorf("chave mestra ausente em %s; o cofre existente em %s não pode ser decifrado; restaure o arquivo .master.key.wrapped nessa pasta, ou mova a pasta %s para outro lugar e restaure o backup .khvault pelo app", dir, dataDir, dataDir)
	}
	_ = os.MkdirAll(dir, 0700)
	_ = os.Chmod(dir, 0700)
	k := loadOrCreateMasterKey(dir)
	chaveMestraMu.Lock()
	chaveMestra = k
	chaveMestraMu.Unlock()
	return nil
}

// Na dúvida (erro que não é "não existe"), conta como existente: melhor recusar que criar chave nova
func existeOuIncerto(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

func chaveNoDisco(dir string) bool {
	return existeOuIncerto(filepath.Join(dir, ".master.key.wrapped")) || existeOuIncerto(filepath.Join(dir, ".master.key"))
}

func chaveMestraAtual() ([]byte, error) {
	chaveMestraMu.RLock()
	defer chaveMestraMu.RUnlock()
	if chaveMestra == nil {
		return nil, errors.New("chave mestra não carregada")
	}
	return chaveMestra, nil
}

// Só gera chave nova se o arquivo NÃO existe: trocar uma chave ilegível
// deixaria todos os segredos já cifrados irrecuperáveis.
// No Windows a chave é selada por DPAPI; no Linux fica em arquivo 0600 sem selagem (#40).
func loadOrCreateMasterKey(pathOrDir string) []byte {
	dir := pathOrDir
	if fi, err := os.Stat(pathOrDir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(pathOrDir)
	} else if strings.HasSuffix(pathOrDir, ".key") || strings.HasSuffix(pathOrDir, ".wrapped") {
		dir = filepath.Dir(pathOrDir)
	}

	wrappedPath := filepath.Join(dir, ".master.key.wrapped")
	legacyPath := filepath.Join(dir, ".master.key")

	// 1. Tentar ler chave selada pelo SO
	if wrappedData, err := os.ReadFile(wrappedPath); err == nil && len(wrappedData) > 0 {
		unwrapped, uerr := osUnwrapKey(wrappedData)
		if uerr == nil && len(unwrapped) == 32 {
			_ = os.Remove(legacyPath)
			return unwrapped
		}
		log.Fatalf("[KeyHybrid] falha ao decifrar chave mestra selada pelo SO: %v", uerr)
	}

	// 2. Migração: se existir .master.key legada em claro, selar no SO e remover o arquivo em claro
	if legacyData, err := os.ReadFile(legacyPath); err == nil {
		if len(legacyData) != 32 {
			log.Fatalf("[KeyHybrid] chave mestra legada com tamanho inválido (%d bytes)", len(legacyData))
		}
		wrapped, werr := osWrapKey(legacyData)
		if werr != nil {
			log.Fatalf("[KeyHybrid] falha ao selar chave mestra legada no SO: %v", werr)
		}
		if err := os.WriteFile(wrappedPath, wrapped, 0600); err != nil {
			log.Fatalf("[KeyHybrid] falha ao salvar chave mestra selada: %v", err)
		}
		_ = os.Remove(legacyPath)
		return legacyData
	}

	// 3. Gerar nova chave e selar
	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		log.Fatal("[KeyHybrid] falha ao gerar chave mestra: ", err)
	}
	wrapped, werr := osWrapKey(newKey)
	if werr != nil {
		log.Fatal("[KeyHybrid] falha ao selar chave mestra nova: ", werr)
	}
	f, err := os.OpenFile(wrappedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateMasterKey(dir)
	}
	if err != nil {
		log.Fatal("[KeyHybrid] falha ao gravar chave mestra selada: ", err)
	}
	if _, err := f.Write(wrapped); err != nil {
		f.Close()
		os.Remove(wrappedPath)
		log.Fatal("[KeyHybrid] falha ao gravar chave mestra selada: ", err)
	}
	if err := f.Close(); err != nil {
		log.Fatal("[KeyHybrid] falha ao gravar chave mestra selada: ", err)
	}
	return newKey
}

// O cofre mora em ~/.keyhybrid/pb_data, não ao lado do binário: cada launcher
// (app, run_linux.sh, systemd) subia uma cópia diferente e abria um cofre vazio.
// --dir continua valendo por cima.
func dataDir() string { return dataDirCriando(true) }

// criar=false só resolve o caminho: quem só lê o cofre não pode criar um vazio
// (numa instalação antiga, a pasta nova vazia passaria a esconder o cofre de verdade)
func dataDirCriando(criar bool) string {
	home := homeDir()
	dir := filepath.Join(home, ".keyhybrid", "pb_data")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	// Instalação antiga: cofre ao lado do binário. Segue usando até ser movido.
	if exe, err := os.Executable(); err == nil {
		legado := filepath.Join(filepath.Dir(exe), "pb_data")
		if _, err := os.Stat(filepath.Join(legado, "data.db")); err == nil {
			log.Println("[KeyHybrid] usando cofre antigo em", legado, "- mova para", dir)
			return legado
		}
	}
	if criar {
		_ = os.MkdirAll(dir, 0700)
	}
	return dir
}

func encryptSecretAtRest(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" || strings.HasPrefix(plain, "enc:v1:") {
		return plain, nil
	}
	key, err := chaveMestraAtual()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func decryptSecretAtRestBytes(stored string) ([]byte, error) {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return []byte{}, nil
	}
	// Texto puro só existe antes da migração do boot; em runtime, sem prefixo é erro
	if !strings.HasPrefix(stored, "enc:v1:") {
		return nil, errors.New("segredo sem o prefixo enc:v1:")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, "enc:v1:"))
	if err != nil {
		return nil, err
	}
	key, err := chaveMestraAtual()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return nil, errors.New("formato de cifra inválido")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

func decryptSecretAtRest(stored string) (string, error) {
	b, err := decryptSecretAtRestBytes(stored)
	if err != nil {
		return "", err
	}
	defer wipeBytes(b)
	return string(b), nil
}

// A resposta 500 já foi escrita quando este erro volta: o handler só o devolve
var errSegredoIlegivel = errors.New("segredo não decifra")

// segredoDoRegistro decifra ou responde 500 claro; nunca entrega string vazia para ir ao provedor
func segredoDoRegistro(evt *core.RequestEvent, rec *core.Record) (string, error) {
	s, err := decryptSecretAtRest(rec.GetString("secret_key"))
	if err == nil {
		return s, nil
	}
	alias := rec.GetString("alias")
	log.Printf("[KeyHybrid] o segredo do alias %s não decifra: %v\n", alias, err)
	if jerr := evt.JSON(http.StatusInternalServerError, map[string]string{
		"error": "o segredo do alias " + alias + " não decifra (chave mestra trocada?)",
	}); jerr != nil {
		return "", jerr
	}
	return "", errSegredoIlegivel
}

// A API da Anthropic usa o id puro ("claude-…"), sem o prefixo de roteamento da OpenRouter
func anthropicModel(v any) string {
	m, _ := v.(string)
	m = strings.TrimPrefix(strings.TrimSpace(m), "anthropic/")
	if !strings.HasPrefix(m, "claude-") {
		return "claude-haiku-5-5"
	}
	return m
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
		// camada compatível com OpenAI da própria Anthropic; nunca mandar sk-ant- a terceiros
		return "https://api.anthropic.com/v1/chat/completions"
	default:
		return "https://openrouter.ai/api/v1/chat/completions"
	}
}

// Coleção "vault_keys" no bootstrap; o CLI de cofre (vault_cli.go) usa o mesmo hook
func vincularBootstrapVaultKeys(app core.App) {
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
				Name: "base_url",
			})
			collection.Fields.Add(&core.TextField{
				Name: "default_model",
			})
			collection.Fields.Add(&core.BoolField{
				Name: "active",
			})
			collection.Fields.Add(&core.TextField{
				Name: "kind",
			})
			collection.Fields.Add(&core.TextField{
				Name: "auth_header",
			})
			collection.Fields.Add(&core.TextField{
				Name: "auth_prefix",
			})

			collection.AddIndex("idx_vault_provider_alias", false, "provider, alias", "")

			if err := app.Save(collection); err != nil {
				log.Printf("[KeyHybrid] Erro ao criar coleção vault_keys: %v\n", err)
			}
		} else {
			col, err := app.FindCollectionByNameOrId("vault_keys")
			if err == nil {
				if col.Fields.GetByName("masked_key") != nil {
					col.Fields.RemoveByName("masked_key")
					_ = app.Save(col)
				}
				for _, name := range []string{"kind", "auth_header", "auth_prefix"} {
					if col.Fields.GetByName(name) == nil {
						col.Fields.Add(&core.TextField{Name: name})
						_ = app.Save(col)
					}
				}
			}
		}

		// Migrar automaticamente quaisquer chaves em texto puro no SQLite para AES-256-GCM (enc:v1:...)
		if recs, err := app.FindAllRecords("vault_keys"); err == nil {
			for _, r := range recs {
				raw := r.GetString("secret_key")
				if raw != "" && !strings.HasPrefix(raw, "enc:v1:") {
					enc, err := encryptSecretAtRest(raw)
					if err != nil {
						log.Fatalf("[KeyHybrid] erro ao cifrar chave na migração: %v", err)
					}
					r.Set("secret_key", enc)
					if err := app.Save(r); err != nil {
						log.Fatalf("[KeyHybrid] erro ao salvar chave na migração: %v", err)
					}
				}
			}
		}

		return nil
	})
}

// validarChave normaliza e valida o cadastro; o POST e o vault-apply aplicam a mesma regra
func validarChave(req *KeyCreateRequest) (baseURL, authHeader, authPrefix string, err error) {
	req.Alias = strings.TrimSpace(req.Alias)
	req.SecretKey = strings.TrimSpace(req.SecretKey)
	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	req.Kind = strings.TrimSpace(strings.ToLower(req.Kind))
	if req.Kind == "" {
		req.Kind = "ai"
	}
	if req.Kind != "ai" && req.Kind != "service" {
		return "", "", "", errors.New("Tipo de chave inválido: use ai ou service")
	}
	if req.Alias == "" || req.SecretKey == "" {
		return "", "", "", errors.New("O nome (alias) e a chave secreta são obrigatórios")
	}
	// O dump sai em claro: segredo já cifrado viria de outra chave mestra e nunca decifraria aqui
	if strings.HasPrefix(req.SecretKey, "enc:v1:") {
		return "", "", "", errors.New("a chave secreta já vem cifrada (enc:v1:); use o valor em claro")
	}
	if req.Provider == "" {
		req.Provider = "openrouter"
		if req.Kind == "service" {
			req.Provider = "outro"
		}
	}
	if req.Kind == "ai" && req.DefaultModel == "" {
		req.DefaultModel = "openrouter/auto"
	}
	if req.Kind == "service" {
		req.DefaultModel = "" // modelo só existe para IA
	}

	baseURL = req.BaseURL
	if req.Kind == "service" {
		// só pontos ("." ou "..") viraria segmento de caminho na URL do proxy
		if !aliasRe.MatchString(req.Alias) || strings.Trim(req.Alias, ".") == "" {
			return "", "", "", errors.New("Alias inválido: use letras, números, ponto, traço ou sublinhado (até 64)")
		}
		base, errURL := validateServiceBaseURL(req.BaseURL)
		if errURL != nil {
			return "", "", "", errURL
		}
		baseURL = base
		authHeader = strings.TrimSpace(req.AuthHeader)
		authPrefix = req.AuthPrefix
		if authHeader == "" {
			authHeader, authPrefix = "Authorization", "Bearer "
		} else if !isAllowedAuthHeader(authHeader) {
			return "", "", "", errors.New("Cabeçalho de autenticação proibido ou inválido")
		}
	}
	return baseURL, authHeader, authPrefix, nil
}

// O gateway usa "Authorization: Bearer kh-<alias>" (e o token local), que não é token do
// PocketBase. Desde a v0.42 um middleware global responde 401 a todo Authorization que não
// carrega um registro de auth: sem isto, todo cliente do /v1 e do kh run seria recusado.
func liberarAuthorizationProprio(r *router.Router[*core.RequestEvent]) {
	r.Unbind(apis.DefaultRejectUnknownTokenMiddlewareId)
}

// Segredos em claro só para o kh run (#44): exige o .reveal.token, nunca o .cli.token do proxy
func revealHandler(app core.App, revealToken string) func(*core.RequestEvent) error {
	return func(evt *core.RequestEvent) error {
		if evt.Request.Header.Get("Origin") != "" {
			return evt.ForbiddenError("Reveal não aceita requisição de navegador", nil)
		}
		if !revealTokenValid(evt.Request, revealToken) {
			return evt.UnauthorizedError("Token de revelação inválido", nil)
		}
		var req struct {
			Aliases []string `json:"aliases"`
		}
		if err := json.NewDecoder(io.LimitReader(evt.Request.Body, 64<<10)).Decode(&req); err != nil {
			return evt.BadRequestError("JSON inválido", err)
		}

		allRecords, _ := app.FindAllRecords("vault_keys")
		secrets := map[string]string{}
		var missing []string
		for _, alias := range req.Aliases {
			var rec *core.Record
			for _, r := range allRecords {
				if strings.EqualFold(r.GetString("alias"), alias) {
					rec = r
					break
				}
			}
			if rec == nil {
				missing = append(missing, alias)
				continue
			}
			secret, err := segredoDoRegistro(evt, rec)
			if err != nil {
				return err
			}
			secrets[alias] = secret
		}

		evt.Response.Header().Set("Cache-Control", "no-store")
		if len(missing) > 0 {
			log.Printf("[KeyHybrid] reveal: alias não cadastrado: %s\n", strings.Join(missing, ", "))
			return evt.JSON(http.StatusNotFound, map[string]any{"message": "Alias não cadastrado", "missing": missing})
		}
		log.Printf("[KeyHybrid] reveal: %s\n", strings.Join(req.Aliases, ", "))
		return evt.JSON(http.StatusOK, map[string]any{"secrets": secrets})
	}
}

// Abre a porta ANTES de rotacionar os tokens locais (#37, #44). O PocketBase só faz o net.Listen
// depois dos handlers de OnServe: sem isto, uma segunda instância que ia falhar no bind
// sobrescrevia o .cli.token da instância que está atendendo e o kh run passava a ser recusado.
func reservarPortaEToken(e *core.ServeEvent) (cliToken, revealToken string, _ error) {
	if e.Server != nil && e.Listener == nil && e.Server.Addr != "" {
		ln, err := net.Listen("tcp", e.Server.Addr)
		if err != nil {
			return "", "", fmt.Errorf("porta %s ocupada (outro KeyHybrid já está rodando?); tokens locais mantidos: %w", e.Server.Addr, err)
		}
		e.Listener = ln // o PocketBase usa este listener no lugar do próprio
	}
	return loadOrCreateCLIToken(), loadOrCreateRevealToken(), nil
}

// As APIs nativas do PocketBase (/api/collections, /api/superusers, /api/files...) e o
// painel /_/ não têm uso no gateway: tudo fora de rotaPermitida responde 404 antes de
// qualquer middleware do PocketBase (auth, logger, CORS).
func bloquearRotasNativas(r *router.Router[*core.RequestEvent]) {
	r.Bind(&hook.Handler[*core.RequestEvent]{
		Id:       "keyhybridBloquearRotasNativas",
		Priority: apis.DefaultWWWRedirectMiddlewarePriority - 1,
		Func: func(e *core.RequestEvent) error {
			if !rotaPermitida(e.Request.URL.Path) {
				return e.NotFoundError("Rota não encontrada", nil)
			}
			return e.Next()
		},
	})
}

func main() {
	desligarCoreDump() // antes de tudo: vale para o servidor e para os filhos do kh run

	// "run" é o kh run: não pode abrir pb_data ao lado do binário
	if len(os.Args) > 1 && os.Args[1] == "run" {
		os.Exit(runCmd(os.Args[2:]))
	}
	// "list" também só fala com o gateway: aliases sem segredo, para o agente descobrir o que existe
	if len(os.Args) > 1 && os.Args[1] == "list" {
		os.Exit(listCmd(os.Args[2:], os.Stdout))
	}

	// vault-dump e vault-apply: backup portátil do cofre pela CLI, sem rota HTTP
	if len(os.Args) > 1 && os.Args[1] == "vault-dump" {
		os.Exit(vaultDumpCmd(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "vault-apply" {
		os.Exit(vaultApplyCmd(os.Args[2:]))
	}

	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		log.Printf("keyhybrid-server %s", version)
		return
	}

	log.Printf("[KeyHybrid] keyhybrid-server %s", version)

	// NewWithConfig já lê o --dir: a chave sai depois, com o dataDir real, e uma chave ilegível para o boot
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir()})
	if err := carregarChaveMestra(app.DataDir()); err != nil {
		log.Fatal("[KeyHybrid] ", err)
	}

	// 1. Inicializar coleção "vault_keys" no SQLite
	vincularBootstrapVaultKeys(app)

	// 2. Rotas do Gateway e Gerenciamento Seguro de Chaves
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if e.Server != nil {
			e.Server.ReadHeaderTimeout = 5 * time.Second
			e.Server.ReadTimeout = 30 * time.Second
			e.Server.IdleTimeout = 60 * time.Second
		}
		cliToken, revealToken, err := reservarPortaEToken(e)
		if err != nil {
			return err
		}
		liberarAuthorizationProprio(e.Router)
		bloquearRotasNativas(e.Router)

		// Configurar CORS estrito do PocketBase (anti CSRF / Origin evil)
		apis.DefaultCORSConfig.AllowOrigins = []string{
			"tauri://localhost",
			"http://tauri.localhost",
			"https://tauri.localhost",
			"http://localhost:1420",
			"http://127.0.0.1:1420",
		}
		apis.DefaultCORSConfig.AllowOriginFunc = func(origin string) (bool, error) {
			return originAllowed(origin, "127.0.0.1:8090"), nil
		}

		// Guarda de entrada: Host loopback (anti DNS rebinding) e Origin só da interface local
		e.Router.BindFunc(func(evt *core.RequestEvent) error {
			if !hostAllowed(evt.Request.Host) {
				evt.Response.Header().Del("Access-Control-Allow-Origin")
				return evt.ForbiddenError("Host não permitido", nil)
			}
			origin := evt.Request.Header.Get("Origin")
			if origin != "" && !originAllowed(origin, evt.Request.Host) {
				evt.Response.Header().Del("Access-Control-Allow-Origin")
				return evt.ForbiddenError("Origem não permitida", nil)
			}

			evt.Response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")

			secFetchSite := evt.Request.Header.Get("Sec-Fetch-Site")
			if (secFetchSite == "cross-site" || secFetchSite == "same-site") &&
				evt.Request.Method != http.MethodGet && evt.Request.Method != http.MethodHead && evt.Request.Method != http.MethodOptions {
				evt.Response.Header().Del("Access-Control-Allow-Origin")
				return evt.ForbiddenError("Sec-Fetch-Site não permitido", nil)
			}

			if origin != "" {
				evt.Response.Header().Set("Access-Control-Allow-Origin", origin)
			}
			evt.Response.Header().Add("Vary", "Origin")
			evt.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			evt.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-KeyHybrid-Token")
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

		registrarRotasDeChaves(e.Router, app, cliToken)

		// Proxy de chave de serviço: o cliente chama /proxy/<alias>/... e a chave entra só no cabeçalho
		e.Router.Any("/proxy/{alias}/{path...}", func(evt *core.RequestEvent) error {
			if !tokenValid(evt.Request, cliToken) {
				return evt.UnauthorizedError("Token local inválido", nil)
			}
			alias := evt.Request.PathValue("alias")
			allRecords, _ := app.FindAllRecords("vault_keys")
			var rec *core.Record
			for _, r := range allRecords {
				if isServiceKey(r) && strings.EqualFold(r.GetString("alias"), alias) {
					rec = r
					break
				}
			}
			if rec == nil {
				return evt.NotFoundError("Chave de serviço não encontrada", nil)
			}

			parts := strings.SplitN(evt.Request.URL.EscapedPath(), "/", 4)
			rest := ""
			if len(parts) == 4 {
				rest = parts[3]
			}
			target, err := buildProxyTarget(rec.GetString("base_url"), rest, evt.Request.URL.RawQuery)
			if err != nil {
				return evt.BadRequestError("Caminho fora do destino da chave", err)
			}

			authName, authPrefix := rec.GetString("auth_header"), rec.GetString("auth_prefix")
			if authName == "" {
				authName, authPrefix = "Authorization", "Bearer "
			}
			secret, err := segredoDoRegistro(evt, rec)
			if err != nil {
				return err
			}
			status := forwardToService(evt.Response, evt.Request, target, authName, authPrefix+secret)
			log.Printf("[KeyHybrid] proxy %s %s /%s -> %d\n", alias, evt.Request.Method, rest, status)
			return nil
		})

		// Segredos em claro só para o kh run: token de revelação e nenhuma requisição vinda de navegador
		e.Router.POST("/api/keyhybrid/reveal", revealHandler(app, revealToken))

		proxyChatHandler := proxyChat(app, cliToken)

		// Expor tanto no padrão OpenAI (/v1/chat/completions) quanto na rota interna
		e.Router.POST("/v1/chat/completions", proxyChatHandler)
		e.Router.POST("/api/ai/proxy/chat", proxyChatHandler)

		// Helper para resolver a chave secreta real a partir do token virtual Bearer kh-<alias>
		resolveVaultSecret := func(evt *core.RequestEvent) (string, error) {
			allRecords, _ := app.FindAllRecords("vault_keys")
			allRecords = onlyAI(allRecords)
			requestedAlias, st, errBody := v1Auth(evt.Request, cliToken, allRecords)
			if st != 0 {
				if err := evt.JSON(st, errBody); err != nil {
					return "", err
				}
				return "", errV1Respondido // resposta já escrita: o PocketBase ignora o erro
			}
			var matchedRecord *core.Record
			if requestedAlias != "" {
				for _, r := range allRecords {
					if strings.EqualFold(r.GetString("alias"), requestedAlias) {
						matchedRecord = r
						break
					}
				}
			} else {
				for _, r := range allRecords {
					if r.GetBool("active") {
						matchedRecord = r
						break
					}
				}
			}
			if matchedRecord == nil {
				return "", evt.BadRequestError("Nenhuma chave cadastrada no KeyHybrid", nil)
			}
			secret, err := segredoDoRegistro(evt, matchedRecord)
			if err != nil {
				return "", err
			}
			if secret == "" {
				return "", evt.BadRequestError("Nenhuma chave cadastrada no KeyHybrid", nil)
			}
			return secret, nil
		}

		// Proxy Universal para Text-to-Speech (HeyGen Voice Turbo, ElevenLabs, etc.) -> POST /v1/audio/speech
		e.Router.POST("/v1/audio/speech", func(evt *core.RequestEvent) error {
			secretKey, err := resolveVaultSecret(evt)
			if err != nil {
				return err
			}
			evt.Request.Body = http.MaxBytesReader(evt.Response, evt.Request.Body, 1<<20)
			bodyBytes, err := io.ReadAll(evt.Request.Body)
			if err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					return evt.Error(http.StatusRequestEntityTooLarge, "Corpo excede limite", err)
				}
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
			secretKey, err := resolveVaultSecret(evt)
			if err != nil {
				return err
			}
			evt.Request.Body = http.MaxBytesReader(evt.Response, evt.Request.Body, 1<<20)
			bodyBytes, err := io.ReadAll(evt.Request.Body)
			if err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					return evt.Error(http.StatusRequestEntityTooLarge, "Corpo excede limite", err)
				}
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
			secretKey, err := resolveVaultSecret(evt)
			if err != nil {
				return err
			}
			jobId := evt.Request.PathValue("id")
			if !jobIdRe.MatchString(jobId) {
				return evt.BadRequestError("ID de vídeo inválido", nil)
			}
			jobId = url.PathEscape(jobId)
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
			secretKey, err := resolveVaultSecret(evt)
			if err != nil {
				return err
			}
			jobId := evt.Request.PathValue("id")
			if !jobIdRe.MatchString(jobId) {
				return evt.BadRequestError("ID de vídeo inválido", nil)
			}
			jobId = url.PathEscape(jobId)
			query := evt.Request.URL.Query().Encode()
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

		// Listagem agregada dos modelos permitidos pelos provedores habilitados -> GET /v1/models
		e.Router.GET("/v1/models", func(evt *core.RequestEvent) error {
			allRecords, _ := app.FindAllRecords("vault_keys")
			allRecords = onlyAI(allRecords)
			if _, st, errBody := v1Auth(evt.Request, cliToken, allRecords); st != 0 {
				return evt.JSON(st, errBody)
			}
			if len(allRecords) == 0 {
				return evt.JSON(http.StatusOK, ModelListResponse{
					Object: "list",
					Data:   []ModelData{},
				})
			}
			modelsResp := aggregateEnabledModels(evt.Request.Context(), allRecords)
			return evt.JSON(http.StatusOK, modelsResp)
		})

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// Rotas de cadastro, listagem, ativação e exclusão de chaves (fora do main para os testes)
func registrarRotasDeChaves(r *router.Router[*core.RequestEvent], app core.App, cliToken string) {
	// Listar chaves salvas usando FindAllRecords (100% imune a erros de coluna/filtro)
	r.GET("/api/keyhybrid/keys", func(evt *core.RequestEvent) error {
		records, err := app.FindAllRecords("vault_keys")
		if err != nil {
			log.Printf("[KeyHybrid] Erro ao listar vault_keys: %v\n", err)
			return evt.JSON(http.StatusOK, []KeySafeView{})
		}

		// Só leitura: listar nunca ativa chave nem regrava default_model (a ativação mora no cadastro e na exclusão)
		// Ordenar: chave ativa primeiro, depois por alias
		sort.SliceStable(records, func(i, j int) bool {
			ai := records[i].GetBool("active")
			aj := records[j].GetBool("active")
			if ai != aj {
				return ai
			}
			return records[i].GetString("alias") < records[j].GetString("alias")
		})

		authorized := tokenValid(evt.Request, cliToken)
		result := make([]any, 0, len(records))
		for _, rec := range records {
			result = append(result, listView(rec, authorized))
		}

		return evt.JSON(http.StatusOK, result)
	})

	// Cadastrar ou atualizar uma chave pelo Alias: "ai" (padrão) ou "service" (qualquer API via /proxy e kh run)
	// Escrita no cofre exige o token local: sem ele, qualquer processo da máquina apagava ou trocava chaves
	r.POST("/api/keyhybrid/keys", func(evt *core.RequestEvent) error {
		if !tokenValid(evt.Request, cliToken) {
			return evt.UnauthorizedError("Token local inválido", nil)
		}
		evt.Request.Body = http.MaxBytesReader(evt.Response, evt.Request.Body, 1<<20)
		var req KeyCreateRequest
		if err := evt.BindBody(&req); err != nil {
			return evt.BadRequestError("Dados inválidos", err)
		}

		baseURL, authHeader, authPrefix, err := validarChave(&req)
		if err != nil {
			return evt.BadRequestError(err.Error(), nil)
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
			} else if r.GetBool("active") && req.Kind == "ai" {
				r.Set("active", false)
				_ = app.Save(r)
			}
		}

		if record == nil {
			record = core.NewRecord(collection)
		}

		encSecret, err := encryptSecretAtRest(req.SecretKey)
		if err != nil {
			return evt.InternalServerError("Falha ao cifrar chave secreta", err)
		}

		record.Set("alias", req.Alias)
		record.Set("provider", req.Provider)
		record.Set("kind", req.Kind)
		record.Set("secret_key", encSecret)
		record.Set("base_url", baseURL)
		record.Set("auth_header", authHeader)
		record.Set("auth_prefix", authPrefix)
		record.Set("default_model", req.DefaultModel)
		record.Set("active", req.Kind == "ai") // "ativo" é só de IA

		if err := app.Save(record); err != nil {
			return evt.InternalServerError("Erro ao salvar chave no cofre", err)
		}

		return evt.JSON(http.StatusOK, keyView(record))
	})

	// Ativar uma chave específica como padrão (só IA)
	r.POST("/api/keyhybrid/keys/{id}/activate", func(evt *core.RequestEvent) error {
		if !tokenValid(evt.Request, cliToken) {
			return evt.UnauthorizedError("Token local inválido", nil)
		}
		id := evt.Request.PathValue("id")
		target, err := app.FindRecordById("vault_keys", id)
		if err != nil {
			return evt.NotFoundError("Chave não encontrada", err)
		}
		if isServiceKey(target) {
			return evt.BadRequestError("Chave de serviço não é ativada: use o alias em /proxy ou kh run", nil)
		}

		allRecords, _ := app.FindAllRecords("vault_keys")
		for _, r := range onlyAI(allRecords) {
			r.Set("active", r.Id == target.Id)
			_ = app.Save(r)
		}

		return evt.JSON(http.StatusOK, map[string]any{"status": "activated", "id": target.Id, "alias": target.GetString("alias")})
	})

	// Remover uma chave do cofre
	r.DELETE("/api/keyhybrid/keys/{id}", func(evt *core.RequestEvent) error {
		if !tokenValid(evt.Request, cliToken) {
			return evt.UnauthorizedError("Token local inválido", nil)
		}
		id := evt.Request.PathValue("id")
		target, err := app.FindRecordById("vault_keys", id)
		if err != nil {
			return evt.NotFoundError("Chave não encontrada", err)
		}

		if err := app.Delete(target); err != nil {
			return evt.InternalServerError("Falha ao excluir chave", err)
		}

		// A ativa saiu: a próxima de IA (ordem por alias) assume
		if target.GetBool("active") && !isServiceKey(target) {
			restantes, _ := app.FindAllRecords("vault_keys")
			ia := onlyAI(restantes)
			sort.SliceStable(ia, func(i, j int) bool { return ia[i].GetString("alias") < ia[j].GetString("alias") })
			if len(ia) > 0 {
				ia[0].Set("active", true)
				if err := app.Save(ia[0]); err != nil {
					return evt.InternalServerError("Chave excluída, mas falhou ao ativar a próxima", err)
				}
			}
		}

		return evt.JSON(http.StatusOK, map[string]any{"status": "deleted", "id": id})
	})
}

// Handler universal compatível com OpenAI / OpenRouter (/v1/chat/completions e /api/ai/proxy/chat)
func proxyChat(app core.App, cliToken string) func(*core.RequestEvent) error {
	return func(evt *core.RequestEvent) error {
		evt.Request.Body = http.MaxBytesReader(evt.Response, evt.Request.Body, 4<<20)
		bodyBytes, err := io.ReadAll(evt.Request.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return evt.Error(http.StatusRequestEntityTooLarge, "Corpo da requisição excede o limite permitido", err)
			}
			return evt.BadRequestError("Falha ao ler corpo da requisição", err)
		}
		bodyBytes = bytes.TrimPrefix(bodyBytes, []byte("\xef\xbb\xbf"))

		var payload map[string]any
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			return evt.BadRequestError("JSON inválido", err)
		}

		allRecords, _ := app.FindAllRecords("vault_keys")
		allRecords = onlyAI(allRecords)

		// Alias pelo Authorization: Bearer kh-<alias> ou pelo campo "alias" do JSON
		requestedAlias, st, errBody := v1Auth(evt.Request, cliToken, allRecords)
		if st != 0 {
			return evt.JSON(st, errBody)
		}
		if a, ok := payload["alias"].(string); ok && a != "" {
			requestedAlias, st, errBody = v1Alias(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "kh-"), allRecords)
			if st != 0 {
				return evt.JSON(st, errBody)
			}
			delete(payload, "alias")
		}
		headerProvider := strings.TrimSpace(evt.Request.Header.Get("X-Provider"))
		if len(allRecords) == 0 {
			return evt.BadRequestError(
				"Nenhuma chave cadastrada no KeyHybrid. Adicione sua chave na interface do KeyHybrid.",
				nil,
			)
		}

		// Obter o modelo solicitado
		requestedModel, _ := payload["model"].(string)

		matchedRecord, cleanModel, provider := resolveAIProvider(
			requestedAlias,
			headerProvider,
			requestedModel,
			allRecords,
		)

		if matchedRecord == nil {
			return evt.NotFoundError("Chave ou provedor não encontrado no cofre", nil)
		}

		secretKey, err := segredoDoRegistro(evt, matchedRecord)
		if err != nil {
			return err
		}
		baseURL := matchedRecord.GetString("base_url")
		defaultModel := matchedRecord.GetString("default_model")

		if secretKey == "" {
			return evt.BadRequestError(
				"Nenhuma chave cadastrada no KeyHybrid. Adicione sua chave na interface do KeyHybrid.",
				nil,
			)
		}

		// Modelo padrão só quando o cliente não mandou nenhum: o pedido explícito nunca é trocado
		if cleanModel == "" {
			if defaultModel != "" {
				cleanModel = defaultModel
			} else if provider == ProviderAnthropic {
				cleanModel = "claude-3-5-sonnet-20241022"
			} else {
				cleanModel = "openrouter/auto"
			}
		}

		// Remover campo interno 'provider' apenas se for string de roteamento do KeyHybrid
		if _, isStr := payload["provider"].(string); isStr {
			delete(payload, "provider")
		}

		targetURL := resolveProviderChatURL(provider, baseURL)
		isStream, _ := payload["stream"].(bool)

		// 1. Tradução específica para Anthropic (/v1/messages)
		if provider == ProviderAnthropic {
			anthropicReq, trErr := translateOpenAIToAnthropic(payload, cleanModel)
			if trErr != nil {
				return evt.BadRequestError("Falha na tradução do payload para Anthropic", trErr)
			}
			reqBytes, mErr := json.Marshal(anthropicReq)
			if mErr != nil {
				return evt.InternalServerError("Falha ao serializar requisição Anthropic", mErr)
			}

			httpReq, rErr := http.NewRequestWithContext(
				evt.Request.Context(),
				http.MethodPost,
				targetURL,
				bytes.NewReader(reqBytes),
			)
			if rErr != nil {
				return evt.InternalServerError("Falha ao criar requisição Anthropic", rErr)
			}
			httpReq.Header.Set("x-api-key", secretKey)
			httpReq.Header.Set("anthropic-version", "2023-06-01")
			httpReq.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 5 * time.Minute}
			upstreamResp, err := client.Do(httpReq)
			if err != nil {
				return evt.InternalServerError("Erro de conexão com Anthropic", err)
			}
			defer upstreamResp.Body.Close()

			if upstreamResp.StatusCode != http.StatusOK {
				for k, v := range upstreamResp.Header {
					evt.Response.Header()[k] = v
				}
				evt.Response.WriteHeader(upstreamResp.StatusCode)
				_, _ = io.Copy(evt.Response, upstreamResp.Body)
				return nil
			}

			if isStream {
				return pipeAnthropicStreamToOpenAI(evt.Request.Context(), evt.Response, upstreamResp.Body, requestedModel)
			}

			bodyResp, readErr := io.ReadAll(upstreamResp.Body)
			if readErr != nil {
				return evt.InternalServerError("Falha ao ler resposta da Anthropic", readErr)
			}
			var antResp AnthropicResponse
			if err := json.Unmarshal(bodyResp, &antResp); err != nil {
				return evt.InternalServerError("Falha ao decodificar resposta da Anthropic", err)
			}
			openaiResp := translateAnthropicToOpenAI(&antResp, requestedModel)
			return evt.JSON(http.StatusOK, openaiResp)
		}

		// 2. Provedores no padrão OpenAI (OpenAI, Groq, Gemini, OpenRouter)
		if provider == ProviderOpenRouter {
			payload["model"] = requestedModel
		} else {
			payload["model"] = cleanModel
		}

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

		// Status do provedor (404 inclusive) volta ao cliente como veio: trocar de modelo em silêncio é proibido
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
}
