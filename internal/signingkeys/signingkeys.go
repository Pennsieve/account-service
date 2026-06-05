// Package signingkeys owns the interactive-session broker keypair(s) in Secrets
// Manager and derives the public JWKS. workflow-service (the signer) reads the
// current private key from the same secret; the auth-proxy sidecars verify
// against the JWKS this serves. Rotation = append a new key and point Current at
// it; old keys stay in the JWKS until removed.
package signingkeys

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

// StoredKey is one keypair (private PEM kept in Secrets Manager only).
type StoredKey struct {
	Kid        string `json:"kid"`
	PrivatePEM string `json:"privatePem"`
	CreatedAt  string `json:"createdAt"`
}

// KeySet is the JSON stored in the signing secret.
type KeySet struct {
	Current string      `json:"current"` // kid to sign with
	Keys    []StoredKey `json:"keys"`
}

// SecretsAPI is the subset of the Secrets Manager client used here.
type SecretsAPI interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	CreateSecret(ctx context.Context, in *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error)
}

// Ensure returns the key set, generating + storing a new RSA key if the secret
// doesn't exist yet. Idempotent; safe against a concurrent first-create (re-reads
// on CreateSecret conflict).
func Ensure(ctx context.Context, sm SecretsAPI, secretName string) (*KeySet, error) {
	if ks, ok, err := read(ctx, sm, secretName); err != nil {
		return nil, err
	} else if ok {
		return ks, nil
	}

	ks, err := newKeySet()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(ks)
	if _, err := sm.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(secretName),
		SecretString: aws.String(string(body)),
	}); err != nil {
		// Likely created concurrently — re-read.
		if existing, ok, rerr := read(ctx, sm, secretName); rerr == nil && ok {
			return existing, nil
		}
		return nil, fmt.Errorf("create signing secret: %w", err)
	}
	return ks, nil
}

func read(ctx context.Context, sm SecretsAPI, secretName string) (*KeySet, bool, error) {
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(secretName)})
	if err != nil {
		var notFound *smtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get signing secret: %w", err)
	}
	if out.SecretString == nil {
		return nil, false, nil
	}
	var ks KeySet
	if err := json.Unmarshal([]byte(*out.SecretString), &ks); err != nil {
		return nil, false, fmt.Errorf("unmarshal key set: %w", err)
	}
	if len(ks.Keys) == 0 {
		return nil, false, nil
	}
	return &ks, true, nil
}

func newKeySet() (*KeySet, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	kid, err := randomKid()
	if err != nil {
		return nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return &KeySet{
		Current: kid,
		Keys:    []StoredKey{{Kid: kid, PrivatePEM: string(pemBytes), CreatedAt: time.Now().UTC().Format(time.RFC3339)}},
	}, nil
}

func randomKid() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CurrentPrivate returns the PEM + kid the signer should use.
func (ks *KeySet) CurrentPrivate() (privatePEM, kid string, err error) {
	for _, k := range ks.Keys {
		if k.Kid == ks.Current {
			return k.PrivatePEM, k.Kid, nil
		}
	}
	if len(ks.Keys) > 0 {
		return ks.Keys[0].PrivatePEM, ks.Keys[0].Kid, nil
	}
	return "", "", errors.New("key set has no keys")
}

// JWKS builds the public JWKS (RFC 7517) for all keys in the set.
func (ks *KeySet) JWKS() (map[string]any, error) {
	keys := make([]map[string]any, 0, len(ks.Keys))
	for _, k := range ks.Keys {
		block, _ := pem.Decode([]byte(k.PrivatePEM))
		if block == nil {
			return nil, fmt.Errorf("kid %q: invalid PEM", k.Kid)
		}
		priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("kid %q: %w", k.Kid, err)
		}
		pub := &priv.PublicKey
		keys = append(keys, map[string]any{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": k.Kid,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	return map[string]any{"keys": keys}, nil
}
