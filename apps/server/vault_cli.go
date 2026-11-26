package main

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

const (
	vaultFormat   = "keyhybrid-vault"
	vaultVersion  = 1
	vaultMaxInput = 16 << 20
)

// Contrato fixo com o Rust (backup.rs): não mudar nomes nem ordem dos campos
type vaultDoc struct {
	Format  string       `json:"format"`
	Version int          `json:"version"`
	Entries []vaultEntry `json:"entries"`
}

type vaultEntry struct {
	Alias        string `json:"alias"`
	Provider     string `json:"provider"`
	Kind         string `json:"kind"`
	Secret       string `json:"secret"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model"`
	Active       bool   `json:"active"`
	AuthHeader   string `json:"auth_header"`
	AuthPrefix   string `json:"auth_prefix"`
}

type applyReport struct {
	Mode       string   `json:"mode"`
	DryRun     bool     `json:"dry_run"`
	Added      []string `json:"added"`
	Conflicts  []string `json:"conflicts"`
	Removed    int      `json:"removed"`
	TotalAfter int      `json:"total_after"`
}

// Entrada já validada pelas regras do POST /api/keyhybrid/keys
type vaultItem struct {
	alias, provider, kind, secret, baseURL, defaultModel, authHeader, authPrefix string
	ativa                                                                        bool
}

type vaultPlano struct {
	apagar     []*core.Record
	inserir    []vaultItem
	added      []string
	conflicts  []string
	totalAntes int
}

var errCofreAusente = errors.New("cofre não encontrado")

// Abre o mesmo cofre do serve, com o mesmo hook de bootstrap, sem subir HTTP.
// criar=false falha alto se o cofre não existe, sem criar pasta, banco nem chave mestra.
func abrirVault(criar bool) (core.App, error) {
	dir := dataDirCriando(criar)
	if !criar {
		if _, err := os.Stat(filepath.Join(dir, "data.db")); err != nil {
			return nil, fmt.Errorf("%w em %s", errCofreAusente, dir)
		}
	}
	if err := carregarChaveMestra(dir); err != nil {
		return nil, err
	}
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	vincularBootstrapVaultKeys(app)
	if err := app.Bootstrap(); err != nil {
		return nil, fmt.Errorf("não foi possível abrir o cofre: %w", err)
	}
	return app, nil
}

// Lê todas as chaves e decifra; qualquer falha para tudo sem expor o segredo
func dumpVault(app core.App) (vaultDoc, error) {
	recs, err := app.FindAllRecords("vault_keys")
	if err != nil {
		return vaultDoc{}, fmt.Errorf("vault-dump: não foi possível ler o cofre: %w", err)
	}
	entries := make([]vaultEntry, 0, len(recs))
	for _, r := range recs {
		alias := r.GetString("alias")
		plain, err := decryptSecretAtRestBytes(r.GetString("secret_key"))
		if err != nil {
			return vaultDoc{}, fmt.Errorf("vault-dump: não foi possível decifrar a chave %s", alias)
		}
		secret := string(plain)
		wipeBytes(plain)
		entries = append(entries, vaultEntry{
			Alias:        alias,
			Provider:     r.GetString("provider"),
			Kind:         r.GetString("kind"),
			Secret:       secret,
			BaseURL:      r.GetString("base_url"),
			DefaultModel: r.GetString("default_model"),
			Active:       r.GetBool("active"),
			AuthHeader:   r.GetString("auth_header"),
			AuthPrefix:   r.GetString("auth_prefix"),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Alias) < strings.ToLower(entries[j].Alias)
	})
	return vaultDoc{Format: vaultFormat, Version: vaultVersion, Entries: entries}, nil
}

// Valida tudo antes de qualquer escrita: um erro deixa o cofre como estava
func validarVaultEntries(entries []vaultEntry) ([]vaultItem, error) {
	itens := make([]vaultItem, 0, len(entries))
	vistos := map[string]bool{}
	for _, e := range entries {
		req := KeyCreateRequest{
			Alias:        e.Alias,
			Provider:     e.Provider,
			SecretKey:    e.Secret,
			BaseURL:      e.BaseURL,
			DefaultModel: e.DefaultModel,
			Kind:         e.Kind,
			AuthHeader:   e.AuthHeader,
			AuthPrefix:   e.AuthPrefix,
		}
		baseURL, authHeader, authPrefix, err := validarChave(&req)
		if err != nil {
			return nil, fmt.Errorf("vault-apply: chave %q inválida: %v", e.Alias, err)
		}
		chave := strings.ToLower(req.Alias)
		if vistos[chave] {
			return nil, fmt.Errorf("vault-apply: alias duplicado no documento: %q", req.Alias)
		}
		vistos[chave] = true
		itens = append(itens, vaultItem{
			alias:        req.Alias,
			provider:     req.Provider,
			kind:         req.Kind,
			secret:       req.SecretKey,
			baseURL:      baseURL,
			defaultModel: req.DefaultModel,
			authHeader:   authHeader,
			authPrefix:   authPrefix,
			ativa:        e.Active,
		})
	}
	return itens, nil
}

func existeAlias(recs []*core.Record, alias string) bool {
	for _, r := range recs {
		if strings.EqualFold(r.GetString("alias"), alias) {
			return true
		}
	}
	return false
}

// Regra de ativa: no máximo uma IA ativa; serviço nunca ativo
func planejarVault(recs []*core.Record, itens []vaultItem, mode string) vaultPlano {
	p := vaultPlano{added: []string{}, conflicts: []string{}, totalAntes: len(recs)}
	existentes := recs
	if mode == "replace" {
		p.apagar = recs
		existentes = nil
	}
	iaAtiva := false
	for _, r := range existentes {
		if !isServiceKey(r) && r.GetBool("active") {
			iaAtiva = true
		}
	}
	for _, it := range itens {
		if mode == "merge" && existeAlias(existentes, it.alias) {
			p.conflicts = append(p.conflicts, it.alias)
			continue
		}
		if it.kind == "ai" && it.ativa && !iaAtiva {
			it.ativa = true
			iaAtiva = true
		} else {
			it.ativa = false
		}
		p.inserir = append(p.inserir, it)
		p.added = append(p.added, it.alias)
	}
	return p
}

func (p vaultPlano) relatorio(mode string, dryRun bool) applyReport {
	return applyReport{
		Mode:       mode,
		DryRun:     dryRun,
		Added:      p.added,
		Conflicts:  p.conflicts,
		Removed:    len(p.apagar),
		TotalAfter: p.totalAntes - len(p.apagar) + len(p.inserir),
	}
}

// Dry-run só lê; a aplicação inteira roda numa transação (rollback em qualquer erro)
func applyVault(app core.App, doc vaultDoc, mode string, dryRun bool) (applyReport, error) {
	if mode != "merge" && mode != "replace" {
		return applyReport{}, errors.New("vault-apply: modo inválido: use merge ou replace")
	}
	itens, err := validarVaultEntries(doc.Entries)
	if err != nil {
		return applyReport{}, err
	}

	if dryRun {
		var recs []*core.Record // app nil = cofre ainda não existe: prévia contra cofre vazio
		if app != nil {
			if recs, err = app.FindAllRecords("vault_keys"); err != nil {
				return applyReport{}, fmt.Errorf("vault-apply: não foi possível ler o cofre: %w", err)
			}
		}
		return planejarVault(recs, itens, mode).relatorio(mode, true), nil
	}

	var rep applyReport
	err = app.RunInTransaction(func(tx core.App) error {
		recs, err := tx.FindAllRecords("vault_keys")
		if err != nil {
			return err
		}
		col, err := tx.FindCollectionByNameOrId("vault_keys")
		if err != nil {
			return err
		}
		plano := planejarVault(recs, itens, mode)
		for _, r := range plano.apagar {
			if err := tx.Delete(r); err != nil {
				return err
			}
		}
		for _, it := range plano.inserir {
			enc, err := encryptSecretAtRest(it.secret)
			if err != nil {
				return err
			}
			rec := core.NewRecord(col)
			rec.Set("alias", it.alias)
			rec.Set("provider", it.provider)
			rec.Set("kind", it.kind)
			rec.Set("secret_key", enc)
			rec.Set("base_url", it.baseURL)
			rec.Set("auth_header", it.authHeader)
			rec.Set("auth_prefix", it.authPrefix)
			rec.Set("default_model", it.defaultModel)
			rec.Set("active", it.ativa)
			if err := tx.Save(rec); err != nil {
				return err
			}
		}
		rep = plano.relatorio(mode, false)
		return nil
	})
	if err != nil {
		return applyReport{}, fmt.Errorf("vault-apply: %w", err)
	}
	return rep, nil
}

// A primeira linha do stdin precisa ser o .reveal.token (#44): sem ele, o comando não toca no cofre
func exigirRevealToken(r *bufio.Reader) error {
	arq, err := os.ReadFile(revealTokenPath())
	esperado := strings.TrimSpace(string(arq))
	if err != nil || esperado == "" {
		return errors.New("token de revelação ausente; abra o KeyHybrid uma vez")
	}
	linha, _ := r.ReadSlice('\n')
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(string(linha))), []byte(esperado)) != 1 {
		return errors.New("token de revelação inválido na primeira linha do stdin")
	}
	return nil
}

// kh vault-dump: despeja o cofre em claro no stdout (só para o app desktop, via pipe)
func vaultDumpCmd(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "vault-dump: não aceita argumentos")
		fmt.Fprintln(os.Stderr, "uso: keyhybrid-server vault-dump")
		return 2
	}
	if isTerminal(os.Stdout) {
		fmt.Fprintln(os.Stderr, "vault-dump só roda pelo app desktop (stdout é um terminal)")
		return 2
	}
	if err := exigirRevealToken(bufio.NewReader(os.Stdin)); err != nil {
		fmt.Fprintln(os.Stderr, "vault-dump:", err)
		return 2
	}
	app, err := abrirVault(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vault-dump:", err)
		return 1
	}
	defer func() { _ = app.ResetBootstrapState() }()

	doc, err := dumpVault(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out, err := json.Marshal(doc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vault-dump: falha ao montar a saída")
		return 1
	}
	defer wipeBytes(out)
	if _, err := os.Stdout.Write(append(out, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "vault-dump: falha ao escrever a saída")
		return 1
	}
	return 0
}

func usoVaultApply(motivo string) int {
	fmt.Fprintln(os.Stderr, "vault-apply:", motivo)
	fmt.Fprintln(os.Stderr, "uso: keyhybrid-server vault-apply --mode merge|replace [--dry-run] (stdin: token de revelação na 1ª linha, depois o JSON)")
	return 2
}

// kh vault-apply: aplica o JSON do stdin ao cofre, sem rota HTTP
func vaultApplyCmd(args []string) int {
	mode := ""
	dryRun := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--mode":
			if i+1 >= len(args) {
				return usoVaultApply("falta valor para --mode")
			}
			i++
			mode = args[i]
		case strings.HasPrefix(a, "--mode="):
			mode = strings.TrimPrefix(a, "--mode=")
		case a == "--dry-run":
			dryRun = true
		default:
			return usoVaultApply("opção desconhecida: " + a)
		}
	}
	if mode != "merge" && mode != "replace" {
		return usoVaultApply("use --mode merge ou --mode replace")
	}
	if isTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr, "vault-apply: a entrada é um terminal; envie o JSON pelo pipe")
		return 2
	}
	entrada := bufio.NewReader(os.Stdin)
	if err := exigirRevealToken(entrada); err != nil {
		fmt.Fprintln(os.Stderr, "vault-apply:", err)
		return 2
	}

	raw, err := io.ReadAll(io.LimitReader(entrada, vaultMaxInput+1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "vault-apply: falha ao ler a entrada")
		return 1
	}
	defer wipeBytes(raw)
	if len(raw) > vaultMaxInput {
		fmt.Fprintln(os.Stderr, "vault-apply: entrada maior que 16 MiB")
		return 1
	}

	var doc vaultDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		fmt.Fprintln(os.Stderr, "vault-apply: JSON inválido")
		return 1
	}
	if doc.Format != vaultFormat || doc.Version != vaultVersion {
		fmt.Fprintln(os.Stderr, "vault-apply: versão de dump não suportada")
		return 3
	}

	// Só a aplicação de verdade cria o cofre (restauração numa instalação limpa)
	app, err := abrirVault(!dryRun)
	switch {
	case dryRun && errors.Is(err, errCofreAusente):
		app = nil
	case err != nil:
		fmt.Fprintln(os.Stderr, "vault-apply:", err)
		return 1
	default:
		defer func() { _ = app.ResetBootstrapState() }()
	}

	rep, err := applyVault(app, doc, mode, dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out, err := json.Marshal(rep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vault-apply: falha ao montar a saída")
		return 1
	}
	fmt.Fprintln(os.Stdout, string(out))
	return 0
}
