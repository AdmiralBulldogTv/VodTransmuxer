package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
)

func CreateKey() ([]byte, error) {
	genkey := make([]byte, 16)
	_, err := rand.Read(genkey)
	if err != nil {
		return nil, err
	}
	return genkey, nil
}

func Encrypt(data, key, iv []byte) (ciphertext []byte, err error) {
	// Create a new AES block cipher.
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return
	}
	length := len(data)
	paddingRequired := block.BlockSize() - (length % block.BlockSize())
	ciphertext = make([]byte, length+paddingRequired)
	copy(ciphertext, data)
	if _, err = rand.Read(ciphertext[length:]); err != nil {
		return
	}
	enc := cipher.NewCBCEncrypter(block, iv)
	enc.CryptBlocks(ciphertext, ciphertext)
	return
}
