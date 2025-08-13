package funder

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gagliardetto/solana-go"
)

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

func LoadRecipientsFromJSONFile(path string) (map[string]solana.PublicKey, error) {
	jsonFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()

	var recipients map[string]string
	decoder := json.NewDecoder(jsonFile)
	err = decoder.Decode(&recipients)
	if err != nil {
		return nil, err
	}

	recipientsMap := make(map[string]solana.PublicKey)
	for name, pubKey := range recipients {
		pubKey, err := solana.PublicKeyFromBase58(pubKey)
		if err != nil {
			return nil, fmt.Errorf("invalid public key: %w", err)
		}
		recipientsMap[name] = pubKey
	}

	return recipientsMap, nil
}
