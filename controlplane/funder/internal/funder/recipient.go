package funder

import "github.com/gagliardetto/solana-go"

type Recipient struct {
	Name   string
	PubKey solana.PublicKey
}

func NewRecipient(name string, pubKey solana.PublicKey) Recipient {
	return Recipient{
		Name:   name,
		PubKey: pubKey,
	}
}
