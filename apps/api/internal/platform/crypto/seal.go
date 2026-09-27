package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

type Sealer struct{ key []byte }

func NewSealer(encoded string) (Sealer, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return Sealer{}, errors.New("INSTANCE_SECRET must be base64 encoding of 32 bytes")
	}
	return Sealer{key: key}, nil
}
func (s Sealer) Seal(plain string) ([]byte, error) {
	b, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, []byte(plain), nil), nil
}
func (s Sealer) Open(ciphertext []byte) (string, error) {
	b, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < g.NonceSize() {
		return "", errors.New("invalid ciphertext")
	}
	plain, err := g.Open(nil, ciphertext[:g.NonceSize()], ciphertext[g.NonceSize():], nil)
	return string(plain), err
}
