//go:build !windows

package main

func osWrapKey(key []byte) ([]byte, error) {
	out := make([]byte, len(key))
	copy(out, key)
	return out, nil
}

func osUnwrapKey(wrapped []byte) ([]byte, error) {
	out := make([]byte, len(wrapped))
	copy(out, wrapped)
	return out, nil
}
