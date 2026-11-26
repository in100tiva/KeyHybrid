//go:build !windows

package main

import (
	"syscall"
	"testing"
)

func TestDesligarCoreDumpZeraRLIMITCORE(t *testing.T) {
	desligarCoreDump()
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_CORE, &lim); err != nil {
		t.Fatal(err)
	}
	if lim.Cur != 0 || lim.Max != 0 {
		t.Fatalf("RLIMIT_CORE = %d/%d, quero 0/0", lim.Cur, lim.Max)
	}
}
