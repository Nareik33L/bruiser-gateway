package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	s := auth.Signer{KID: "k", MerchantID: "arsenal", Private: priv, Public: pub}
	okTok, err := s.SignExecution(lease.Execution{
		ID: "exe_1", MerchantID: "arsenal", CustomerID: "1001234",
		DomainKey: "dom", Resource: "event:ars-che", Action: "hold",
		Principal: lease.Principal{Type: "agent", ID: "a1"},
		Fence:     2, ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		panic(err)
	}
	staleTok, err := s.SignExecution(lease.Execution{
		ID: "exe_0", MerchantID: "arsenal", CustomerID: "1001234",
		DomainKey: "dom", Resource: "event:ars-che", Action: "hold",
		Principal: lease.Principal{Type: "agent", ID: "a0"},
		Fence:     1, ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		panic(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"ok":    okTok,
		"stale": staleTok,
		"jwks":  auth.JWKS(s.KID, s.Public),
	})
}
