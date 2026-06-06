package signingkeys

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

type fakeSM struct {
	store map[string]string
}

func (f *fakeSM) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if v, ok := f.store[*in.SecretId]; ok {
		return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(v)}, nil
	}
	return nil, &smtypes.ResourceNotFoundException{}
}

func (f *fakeSM) CreateSecret(_ context.Context, in *secretsmanager.CreateSecretInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
	f.store[*in.Name] = *in.SecretString
	return &secretsmanager.CreateSecretOutput{}, nil
}

func TestEnsure_GeneratesThenReuses(t *testing.T) {
	sm := &fakeSM{store: map[string]string{}}
	ks1, err := Ensure(context.Background(), sm, "sig")
	if err != nil {
		t.Fatalf("ensure 1: %v", err)
	}
	if len(ks1.Keys) != 1 || ks1.Current == "" || ks1.Current != ks1.Keys[0].Kid {
		t.Fatalf("unexpected key set: %+v", ks1)
	}
	// Second call returns the SAME stored key (no regeneration).
	ks2, err := Ensure(context.Background(), sm, "sig")
	if err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	if ks2.Current != ks1.Current || ks2.Keys[0].PrivatePEM != ks1.Keys[0].PrivatePEM {
		t.Fatalf("ensure not idempotent")
	}
}

func TestCurrentPrivate(t *testing.T) {
	sm := &fakeSM{store: map[string]string{}}
	ks, _ := Ensure(context.Background(), sm, "sig")
	pemStr, kid, err := ks.CurrentPrivate()
	if err != nil || kid != ks.Current || pemStr == "" {
		t.Fatalf("CurrentPrivate = %q,%q,%v", pemStr, kid, err)
	}
}

func TestJWKS_MatchesPrivateKey(t *testing.T) {
	sm := &fakeSM{store: map[string]string{}}
	ks, _ := Ensure(context.Background(), sm, "sig")
	jwks, err := ks.JWKS()
	if err != nil {
		t.Fatalf("jwks: %v", err)
	}
	keys := jwks["keys"].([]map[string]any)
	if len(keys) != 1 {
		t.Fatalf("want 1 jwk, got %d", len(keys))
	}
	jwk := keys[0]
	if jwk["kid"] != ks.Current || jwk["kty"] != "RSA" || jwk["alg"] != "RS256" {
		t.Errorf("unexpected jwk header: %+v", jwk)
	}

	// The jwk n/e must reconstruct the private key's public part.
	block, _ := pem.Decode([]byte(ks.Keys[0].PrivatePEM))
	priv, _ := x509.ParsePKCS1PrivateKey(block.Bytes)
	nBytes, _ := base64.RawURLEncoding.DecodeString(jwk["n"].(string))
	if new(big.Int).SetBytes(nBytes).Cmp(priv.PublicKey.N) != 0 {
		t.Errorf("jwk modulus does not match private key")
	}
}
