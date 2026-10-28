//go:build windows

package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func osWrapKey(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("chave vazia")
	}
	inBlob := windows.DataBlob{
		Size: uint32(len(plain)),
		Data: &plain[0],
	}
	var outBlob windows.DataBlob
	if err := windows.CryptProtectData(&inBlob, nil, nil, 0, nil, 0, &outBlob); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))
	out := make([]byte, outBlob.Size)
	copy(out, unsafe.Slice(outBlob.Data, outBlob.Size))
	return out, nil
}

func osUnwrapKey(wrapped []byte) ([]byte, error) {
	if len(wrapped) == 0 {
		return nil, errors.New("chave vazia")
	}
	inBlob := windows.DataBlob{
		Size: uint32(len(wrapped)),
		Data: &wrapped[0],
	}
	var outBlob windows.DataBlob
	if err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, 0, &outBlob); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))
	out := make([]byte, outBlob.Size)
	copy(out, unsafe.Slice(outBlob.Data, outBlob.Size))
	return out, nil
}
