package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// #37: a segunda instância que não consegue a porta não pode trocar o token da que está atendendo
func TestSegundaInstanciaNaoTrocaToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KEYHYBRID_HOME", home)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".keyhybrid"), 0700); err != nil {
		t.Fatal(err)
	}
	const tokenVivo = "token-da-instancia-que-esta-atendendo"
	if err := os.WriteFile(cliTokenPath(), []byte(tokenVivo), 0600); err != nil {
		t.Fatal(err)
	}

	viva, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := viva.Addr().String()

	e := &core.ServeEvent{Server: &http.Server{Addr: addr}}
	if _, err := reservarPortaEToken(e); err == nil {
		t.Fatal("porta ocupada: esperava erro")
	}
	if e.Listener != nil {
		t.Fatal("porta ocupada: não deveria haver listener")
	}
	if got, _ := os.ReadFile(cliTokenPath()); string(got) != tokenVivo {
		t.Fatalf("token trocado por uma instância que não subiu: %q", got)
	}

	// Porta livre: a instância reserva a porta e só então rotaciona o token
	_ = viva.Close()
	tok, err := reservarPortaEToken(e)
	if err != nil {
		t.Fatalf("porta livre: %v", err)
	}
	t.Cleanup(func() { _ = e.Listener.Close() })
	if e.Listener.Addr().String() != addr {
		t.Fatalf("listener em %s, esperado %s", e.Listener.Addr(), addr)
	}
	if got, _ := os.ReadFile(cliTokenPath()); tok == tokenVivo || string(got) != tok {
		t.Fatalf("token não rotacionado ou arquivo divergente")
	}
}
