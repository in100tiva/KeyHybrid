//go:build windows

package main

// desligarCoreDump não faz nada no Windows: não há RLIMIT_CORE.
func desligarCoreDump() {}
