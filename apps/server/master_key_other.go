//go:build !windows

package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/zalando/go-keyring"
)

// No Linux a chave mestra mora no chaveiro do sistema (Secret Service) e o
// .master.key.wrapped guarda só este marcador, sem nada secreto (#40).
// O arquivo continua existindo para a regra da #41 (sem arquivo + data.db = recusa).
var marcadorChaveiro = []byte("keyhybrid:secret-service:v1\n")

const servicoChaveiro = "KeyHybrid"

// Ponto de troca dos testes: o TestMain põe um chaveiro em memória e nenhum teste fala com o real
var (
	chaveiroGet       = keyring.Get
	chaveiroSet       = keyring.Set
	chaveiroExiste    = secretServiceRegistrado
	esperaChaveiro    = 60 * time.Second
	intervaloChaveiro = 2 * time.Second
)

// Um item por pasta da chave: um KEYHYBRID_HOME de teste nunca pisa no item do cofre real
func usuarioChaveiro(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return "master-key:" + dir
}

// Chave nova (ou a .master.key antiga): no chaveiro, com leitura de volta conferida.
// Sem Secret Service (container, sem sessão D-Bus) fica em claro, como antes, e avisa.
func selarChave(dir string, key []byte) ([]byte, error) {
	if !chaveiroExiste() {
		log.Printf("[KeyHybrid] AVISO: sem Secret Service nesta sessão (sem D-Bus, ex. container): a chave mestra fica EM CLARO em %s", filepath.Join(dir, ".master.key.wrapped"))
		return bytes.Clone(key), nil
	}
	if err := tentarChaveiro("gravar a chave mestra", func() error { return gravarEConferir(dir, key) }); err != nil {
		return nil, fmt.Errorf("não foi possível gravar a chave mestra no chaveiro do sistema: %w", err)
	}
	return bytes.Clone(marcadorChaveiro), nil
}

// Marcador: lê do chaveiro, esperando ele destravar; nunca cria chave nova nem volta para arquivo.
// 32 bytes crus: arquivo legado, que é migrado para o chaveiro quando der.
func abrirChaveSelada(dir string, data []byte) ([]byte, error) {
	caminho := filepath.Join(dir, ".master.key.wrapped")
	switch {
	case bytes.Equal(data, marcadorChaveiro):
		var k []byte
		err := tentarChaveiro("ler a chave mestra", func() error {
			var e error
			k, e = lerDoChaveiro(dir)
			return e
		})
		if err != nil {
			return nil, fmt.Errorf("a chave mestra está no chaveiro do sistema (Secret Service), mas não foi lida em %s: %w; destrave o chaveiro (entre na sessão do usuário) e suba de novo; se o chaveiro foi recriado, mova este arquivo e a pasta pb_data para outro lugar e restaure o backup .khvault pelo app", esperaChaveiro, err)
		}
		return k, nil
	case len(data) == 32:
		k := bytes.Clone(data)
		if err := migrarParaChaveiro(dir, caminho, k); err != nil {
			log.Printf("[KeyHybrid] AVISO: chave mestra ainda EM CLARO em %s: chaveiro indisponível (%v)", caminho, err)
		}
		return k, nil
	default:
		return nil, fmt.Errorf("formato inválido (%d bytes): esperava o marcador do chaveiro ou 32 bytes", len(data))
	}
}

// Só troca o arquivo pelo marcador depois de gravar, ler de volta e comparar; qualquer falha deixa o legado intacto
func migrarParaChaveiro(dir, caminho string, k []byte) error {
	if !chaveiroExiste() {
		return errors.New("sem Secret Service nesta sessão (sem D-Bus, ex. container)")
	}
	if err := tentarChaveiro("migrar a chave mestra", func() error { return gravarEConferir(dir, k) }); err != nil {
		return err
	}
	if err := gravarAtomico(caminho, marcadorChaveiro); err != nil {
		return fmt.Errorf("item gravado no chaveiro, mas o arquivo não foi trocado pelo marcador: %w", err)
	}
	log.Printf("[KeyHybrid] chave mestra migrada para o chaveiro do sistema; %s guarda só o marcador", caminho)
	return nil
}

func gravarEConferir(dir string, key []byte) error {
	// simplificação: a string base64 não dá para zerar (Go não deixa); some com o GC
	if err := chaveiroSet(servicoChaveiro, usuarioChaveiro(dir), base64.StdEncoding.EncodeToString(key)); err != nil {
		return err
	}
	lida, err := lerDoChaveiro(dir)
	if err != nil {
		return fmt.Errorf("leitura de volta: %w", err)
	}
	defer clear(lida)
	if subtle.ConstantTimeCompare(lida, key) != 1 {
		return errors.New("o chaveiro devolveu outra chave na leitura de volta")
	}
	return nil
}

func lerDoChaveiro(dir string) ([]byte, error) {
	v, err := chaveiroGet(servicoChaveiro, usuarioChaveiro(dir))
	if err != nil {
		return nil, err
	}
	k, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(k) != 32 {
		clear(k)
		return nil, errors.New("item do chaveiro com formato inválido")
	}
	return k, nil
}

// Repete f a cada intervaloChaveiro até dar certo ou vencer esperaChaveiro. Uma chamada presa
// (pedido de destravar aberto na tela) não abre outra: espera ela até o prazo.
func tentarChaveiro(oQue string, f func() error) error {
	limite := time.Now().Add(esperaChaveiro)
	for {
		feito := make(chan error, 1)
		go func() { feito <- f() }()
		var err error
		select {
		case err = <-feito:
		case <-time.After(time.Until(limite)):
			return fmt.Errorf("o chaveiro não respondeu em %s (%s)", esperaChaveiro, oQue)
		}
		if err == nil {
			return nil
		}
		if time.Until(limite) < intervaloChaveiro {
			return err
		}
		log.Printf("[KeyHybrid] aguardando o chaveiro do sistema para %s: %v; nova tentativa em %s", oQue, err, intervaloChaveiro)
		time.Sleep(intervaloChaveiro)
	}
}

// Temporário 0600 + fsync + rename: o arquivo nunca fica pela metade
func gravarAtomico(caminho string, conteudo []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(caminho), ".master.key.wrapped.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // depois do rename não há o que remover
	if _, err := tmp.Write(conteudo); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), caminho); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(caminho)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// "Inexistente" (sem sessão D-Bus ou ninguém atende org.freedesktop.secrets) é diferente de
// "trancado/erro": o primeiro é o modo container; o segundo espera e falha.
// Fora do Linux (macOS) segue o modo arquivo de antes.
func secretServiceRegistrado() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return false
	}
	defer conn.Close()
	if conn.Auth(nil) != nil || conn.Hello() != nil {
		return false
	}
	var temDono bool
	if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.freedesktop.secrets").Store(&temDono) == nil && temDono {
		return true
	}
	var ativaveis []string
	return conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&ativaveis) == nil &&
		slices.Contains(ativaveis, "org.freedesktop.secrets")
}
