package barbican

import (
	"context"
	"encoding/hex"
)

type FakeBarbican struct {
}

func (client *FakeBarbican) GetSecret(_ context.Context, keyID string) ([]byte, error) {
	return hex.DecodeString("6368616e676520746869732070617373")
}
