//go:build !windows

package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// Chaveiro em memória no lugar do Secret Service real (#40)
type chaveiroFalso struct {
	mu     sync.Mutex
	itens  map[string]string
	errSet error
	gets   int
	sets   int
	lixo   bool // Get devolve outra chave (leitura de volta não confere)
}

func instalarChaveiroFalso(existe bool) *chaveiroFalso {
	c := &chaveiroFalso{itens: map[string]string{}}
	chaveiroExiste = func() bool { return existe }
	chaveiroGet = func(servico, usuario string) (string, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.gets++
		v, ok := c.itens[servico+"|"+usuario]
		if !ok {
			return "", keyring.ErrNotFound
		}
		if c.lixo {
			return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), nil
		}
		return v, nil
	}
	chaveiroSet = func(servico, usuario, valor string) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.sets++
		if c.errSet != nil {
			return c.errSet
		}
		c.itens[servico+"|"+usuario] = valor
		return nil
	}
	return c
}

// Todo teste do pacote roda com o chaveiro falso: nada de D-Bus real
func TestMain(m *testing.M) {
	instalarChaveiroFalso(true)
	esperaChaveiro = 200 * time.Millisecond
	intervaloChaveiro = 20 * time.Millisecond
	os.Exit(m.Run())
}

func chaveiroDoTeste(t *testing.T, existe bool) *chaveiroFalso {
	t.Cleanup(func() { instalarChaveiroFalso(true) })
	return instalarChaveiroFalso(existe)
}

func gravarLegado(t *testing.T, dir string) []byte {
	t.Helper()
	k := bytes.Repeat([]byte{42}, 32)
	if err := os.WriteFile(filepath.Join(dir, ".master.key.wrapped"), k, 0600); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestMigraLegadoParaChaveiroComLeituraDeVolta(t *testing.T) {
	c := chaveiroDoTeste(t, true)
	dir := t.TempDir()
	legado := gravarLegado(t, dir)
	wp := filepath.Join(dir, ".master.key.wrapped")

	if k := chaveOuFalha(t, dir); !bytes.Equal(k, legado) {
		t.Fatal("migração devolveu outra chave")
	}
	if got, _ := os.ReadFile(wp); !bytes.Equal(got, marcadorChaveiro) {
		t.Fatalf("arquivo = %q, esperava o marcador", got)
	}
	if fi, _ := os.Stat(wp); fi.Mode().Perm() != 0600 {
		t.Fatalf("permissão %v, want 0600", fi.Mode().Perm())
	}
	if v := c.itens["KeyHybrid|master-key:"+dir]; v != base64.StdEncoding.EncodeToString(legado) {
		t.Fatalf("item do chaveiro = %q", v)
	}
	// Próximo boot: lê do chaveiro, sem gravar de novo
	sets := c.sets
	if k := chaveOuFalha(t, dir); !bytes.Equal(k, legado) {
		t.Fatal("leitura pelo marcador devolveu outra chave")
	}
	if c.sets != sets {
		t.Fatal("gravou no chaveiro de novo")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp-*")); len(m) != 0 {
		t.Fatalf("sobrou temporário: %v", m)
	}
}

// Falha ao gravar, leitura de volta diferente ou sem Secret Service: o arquivo legado fica intacto
func TestMigracaoQueFalhaDeixaLegadoIntacto(t *testing.T) {
	casos := map[string]func(c *chaveiroFalso){
		"set falha":           func(c *chaveiroFalso) { c.errSet = errors.New("trancado") },
		"leitura não confere": func(c *chaveiroFalso) { c.lixo = true },
		"sem Secret Service":  nil,
	}
	for nome, estragar := range casos {
		t.Run(nome, func(t *testing.T) {
			c := chaveiroDoTeste(t, estragar != nil)
			if estragar != nil {
				estragar(c)
			}
			dir := t.TempDir()
			legado := gravarLegado(t, dir)
			if k := chaveOuFalha(t, dir); !bytes.Equal(k, legado) {
				t.Fatal("devolveu outra chave")
			}
			if got, _ := os.ReadFile(filepath.Join(dir, ".master.key.wrapped")); !bytes.Equal(got, legado) {
				t.Fatalf("arquivo legado mudou para %q", got)
			}
		})
	}
}

// Marcador e chaveiro sem o item: tenta até o prazo e falha alto, sem chave nova e sem arquivo
func TestMarcadorSemItemFalhaSemCriarChave(t *testing.T) {
	c := chaveiroDoTeste(t, true)
	dir := t.TempDir()
	wp := filepath.Join(dir, ".master.key.wrapped")
	if err := os.WriteFile(wp, marcadorChaveiro, 0600); err != nil {
		t.Fatal(err)
	}
	k, err := loadOrCreateMasterKey(dir)
	if err == nil || k != nil || !strings.Contains(err.Error(), ".khvault") {
		t.Fatalf("chave %d bytes, erro %v; esperava falha com orientação do .khvault", len(k), err)
	}
	if c.gets < 2 {
		t.Fatalf("tentou %d vez(es), esperava repetir até o prazo", c.gets)
	}
	if c.sets != 0 {
		t.Fatal("gravou chave nova no chaveiro")
	}
	if got, _ := os.ReadFile(wp); !bytes.Equal(got, marcadorChaveiro) {
		t.Fatalf("arquivo mudou para %q", got)
	}
}

// Chaveiro preso (pedido de destravar aberto): uma chamada só, até o prazo
func TestChaveiroPresoNaoAbreOutroPedido(t *testing.T) {
	c := chaveiroDoTeste(t, true)
	solta := make(chan struct{})
	t.Cleanup(func() { close(solta) })
	chaveiroGet = func(string, string) (string, error) {
		c.mu.Lock()
		c.gets++
		c.mu.Unlock()
		<-solta
		return "", keyring.ErrNotFound
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".master.key.wrapped"), marcadorChaveiro, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateMasterKey(dir); err == nil || !strings.Contains(err.Error(), "não respondeu") {
		t.Fatalf("erro = %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gets != 1 {
		t.Fatalf("%d chamadas, esperava 1", c.gets)
	}
}

func TestInstalacaoNovaComSecretService(t *testing.T) {
	c := chaveiroDoTeste(t, true)
	dir := t.TempDir()
	k := chaveOuFalha(t, dir)
	if got, _ := os.ReadFile(filepath.Join(dir, ".master.key.wrapped")); !bytes.Equal(got, marcadorChaveiro) {
		t.Fatalf("arquivo = %q, esperava o marcador", got)
	}
	if v := c.itens["KeyHybrid|master-key:"+dir]; v != base64.StdEncoding.EncodeToString(k) {
		t.Fatal("chave nova não está no chaveiro")
	}
}

// Sem D-Bus (container): arquivo cru como antes, sem tocar no chaveiro
func TestInstalacaoNovaSemSecretService(t *testing.T) {
	c := chaveiroDoTeste(t, false)
	dir := t.TempDir()
	k := chaveOuFalha(t, dir)
	if got, _ := os.ReadFile(filepath.Join(dir, ".master.key.wrapped")); !bytes.Equal(got, k) {
		t.Fatal("arquivo não tem a chave crua")
	}
	if c.sets != 0 || c.gets != 0 {
		t.Fatal("falou com o chaveiro sem Secret Service")
	}
}

// Chaveiro existe mas não grava: falha alto e não deixa arquivo (nem vazio) para trás
func TestInstalacaoNovaComChaveiroQuebradoFalha(t *testing.T) {
	c := chaveiroDoTeste(t, true)
	c.errSet = errors.New("trancado")
	dir := t.TempDir()
	if k, err := loadOrCreateMasterKey(dir); err == nil || k != nil {
		t.Fatalf("chave %d bytes, erro %v; esperava falha", len(k), err)
	}
	if chaveNoDisco(dir) {
		t.Fatal("deixou arquivo de chave no disco")
	}
}
