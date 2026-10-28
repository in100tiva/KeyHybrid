//go:build windows

package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Entropia fixa do KeyHybrid para selagem via DPAPI (KH-05 / N4).
// Exige que chamadas ao CryptUnprotectData forneçam a entropia da aplicação.
var dpapiEntropy = []byte("KeyHybrid::Vault::MasterKey::v1::LocalEntropy")

func osWrapKey(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("chave vazia")
	}
	inBlob := windows.DataBlob{
		Size: uint32(len(plain)),
		Data: &plain[0],
	}
	entropyBlob := windows.DataBlob{
		Size: uint32(len(dpapiEntropy)),
		Data: &dpapiEntropy[0],
	}
	var outBlob windows.DataBlob
	if err := windows.CryptProtectData(&inBlob, nil, &entropyBlob, 0, nil, 0, &outBlob); err != nil {
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
	entropyBlob := windows.DataBlob{
		Size: uint32(len(dpapiEntropy)),
		Data: &dpapiEntropy[0],
	}
	var outBlob windows.DataBlob
	if err := windows.CryptUnprotectData(&inBlob, nil, &entropyBlob, 0, nil, 0, &outBlob); err != nil {
		// Fallback transparente para chaves seladas anteriormente sem o parâmetro de entropia
		if errLegacy := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, 0, &outBlob); errLegacy == nil {
			defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))
			out := make([]byte, outBlob.Size)
			copy(out, unsafe.Slice(outBlob.Data, outBlob.Size))
			return out, nil
		}
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))
	out := make([]byte, outBlob.Size)
	copy(out, unsafe.Slice(outBlob.Data, outBlob.Size))
	return out, nil
}
