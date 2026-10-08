package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/azure-local/azure-local/internal/kernel"
)

// KeyStore holds the RSA signing key and its derived JWK thumbprint.
type KeyStore struct {
	Key *rsa.PrivateKey
	Kid string
}

const signingKeyFilename = "signing-key.pkcs8"

// LoadOrGenerate returns the persisted RSA key, generating one on first run.
// The key survives restarts, so tokens issued before a restart remain valid.
func LoadOrGenerate(persist *kernel.PersistenceManager) (*KeyStore, error) {
	dir, err := persist.ServiceDir("identity")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, signingKeyFilename)

	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("identity: %s is not valid PEM", path)
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("identity: parse key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("identity: key is not RSA")
		}
		return &KeyStore{Key: rsaKey, Kid: kidOf(&rsaKey.PublicKey)}, nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, err
	}
	return &KeyStore{Key: key, Kid: kidOf(&key.PublicKey)}, nil
}

// kidOf computes an RFC 7638 JWK thumbprint over the public key.
func kidOf(pub *rsa.PublicKey) string {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	// Canonical JSON: lexicographically ordered keys, no whitespace.
	canonical := fmt.Sprintf(`{"e":"%s","kty":"RSA","n":"%s"}`, e, n)
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// JWK returns the public JWK for the current signing key.
func (ks *KeyStore) JWK() JWK {
	pub := &ks.Key.PublicKey
	return JWK{
		Kty: "RSA",
		Use: "sig",
		Kid: ks.Kid,
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}
