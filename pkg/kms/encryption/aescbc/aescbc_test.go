package aescbc

import (
	"bytes"
	"crypto/rand"
	"testing"
)

var key []byte

func init() {
	// genereate key for encrypt decrypt operation
	genKey()
}

func TestEncryptDecrypt(t *testing.T) {
	data := []byte("mypassword")
	cipher, _ := Encrypt((data), key)
	plain, _ := Decrypt(cipher, key)
	if !bytes.Equal((data), plain) {
		t.FailNow()
	}
}

// testKeyerror
func TestEncryptDecryptInvalidData(t *testing.T) {
	data := []byte("mypassword")
	cipher, err := Encrypt(data, key)
	_, err = Decrypt(cipher[1:], key)
	if err == nil {
		t.FailNow()
	}
	t.Log(err)
}

func genKey() {
	key = make([]byte, 32)
	_, _ = rand.Read(key)

}
