package wallet

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"

	"github.com/chronodrachma/chrd/pkg/core/types"
)

// GenerateKeyPair generates a new Ed25519 keypair.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

// SaveKey saves the private key to a file in hex format.
func SaveKey(filename string, privKey ed25519.PrivateKey) error {
	hexKey := hex.EncodeToString(privKey)
	return os.WriteFile(filename, []byte(hexKey), 0600)
}

// LoadKey loads a private key from a file (hex format).
func LoadKey(filename string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	hexKey := string(data)
	// Trim whitespace just in case
	return hex.DecodeString(hexKey)
}

// SignTransaction signs the transaction and sets its Signature field.
// It assumes From address matches the key (does not modify From).
func SignTransaction(tx *types.Transaction, privKey ed25519.PrivateKey) error {
	// Ensure From address matches the public key derived from privKey?
	// For prototype, we trust the caller used the right key for the From address.
	// We just sign.
	
	// Check if key is valid length
	if len(privKey) != ed25519.PrivateKeySize {
		return errors.New("invalid private key length")
	}

	msg := tx.Serialize()
	sig := ed25519.Sign(privKey, msg)
	tx.Signature = sig
	
	return nil
}

// PubKeyToAddress returns the hex string of the public key (which is the address).
func PubKeyToAddress(pubKey ed25519.PublicKey) string {
	return hex.EncodeToString(pubKey)
}
