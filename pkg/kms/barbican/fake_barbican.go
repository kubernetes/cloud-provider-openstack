package barbican

import "encoding/hex"

type FakeBarbican struct {
}

func (client *FakeBarbican) GetSecret(keyID string) ([]byte, error) {
	return hex.DecodeString("6368616e676520746869732070617373")
}
