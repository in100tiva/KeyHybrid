//go:build windows

package main

// No Windows a selagem segue a DPAPI de master_key_windows.go; a pasta não entra
func selarChave(_ string, key []byte) ([]byte, error) { return osWrapKey(key) }

func abrirChaveSelada(_ string, data []byte) ([]byte, error) { return osUnwrapKey(data) }
