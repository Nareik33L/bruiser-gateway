package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func New(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b[:]))
}

func Execution() string { return New("exe") }
func Session() string   { return New("ses") }
func Request() string   { return New("req") }
func Key() string       { return New("kid") }
