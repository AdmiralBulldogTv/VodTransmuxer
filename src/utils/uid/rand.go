package uid

import (
	"encoding/base64"
	"math/rand"

	"github.com/gofrs/uuid"
)

var letterRunes = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func NewId() string {
	id, _ := uuid.NewV4()
	b64 := base64.URLEncoding.EncodeToString(id.Bytes()[:12])
	return b64
}
