//go:build !windows

package main

import (
	"log"
	"syscall"
)

// desligarCoreDump zera RLIMIT_CORE (soft e hard): um core do servidor ou de um
// filho do kh run levaria as chaves da memória e do ambiente. O limite é herdado
// pelos filhos, e o hard em 0 impede que eles o subam de volta.
func desligarCoreDump() {
	if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0}); err != nil {
		log.Printf("[KeyHybrid] aviso: não foi possível desligar o core dump: %v", err)
	}
}
