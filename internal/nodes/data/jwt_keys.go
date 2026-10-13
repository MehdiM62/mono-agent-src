package data

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
)

// JWS algorithms for data.jwt: HMAC (HS*), RSA PKCS#1 v1.5 (RS*), RSA-PSS
// (PS*, salt length = digest length) and ECDSA with fixed-size r||s (ES*).

type jwtAlg struct {
	family string // hmac | rsa | pss | ecdsa
	hash   hashType
	curve  int // ECDSA coordinate size in bytes
}

var jwtAlgs = map[string]jwtAlg{
	"HS256": {"hmac", hashTypes["SHA256"], 0},
	"HS384": {"hmac", hashTypes["SHA384"], 0},
	"HS512": {"hmac", hashTypes["SHA512"], 0},
	"RS256": {"rsa", hashTypes["SHA256"], 0},
	"RS384": {"rsa", hashTypes["SHA384"], 0},
	"RS512": {"rsa", hashTypes["SHA512"], 0},
	"PS256": {"pss", hashTypes["SHA256"], 0},
	"PS384": {"pss", hashTypes["SHA384"], 0},
	"PS512": {"pss", hashTypes["SHA512"], 0},
	"ES256": {"ecdsa", hashTypes["SHA256"], 32},
	"ES384": {"ecdsa", hashTypes["SHA384"], 48},
	"ES512": {"ecdsa", hashTypes["SHA512"], 66},
}

type jwtKeys struct {
	alg     jwtAlg
	secret  []byte
	private crypto.Signer
	public  crypto.PublicKey
}

func loadJWTKeys(config map[string]interface{}, alg, op string) (jwtKeys, error) {
	a, ok := jwtAlgs[alg]
	if !ok {
		return jwtKeys{}, fmt.Errorf("algorithm: unsupported value %q", alg)
	}
	k := jwtKeys{alg: a}
	keyType := cfgString(config, "key_type", "passphrase")
	if keyType == "pemKey" {
		keyType = "pem_key"
	}
	if a.family == "hmac" {
		if keyType != "passphrase" {
			return k, fmt.Errorf("algorithm %s needs key_type passphrase", alg)
		}
		secret := cfgString(config, "secret", "")
		if secret == "" {
			return k, fmt.Errorf("secret is required for %s", alg)
		}
		k.secret = []byte(secret)
		return k, nil
	}
	if keyType != "pem_key" {
		return k, fmt.Errorf("algorithm %s needs key_type pem_key", alg)
	}
	if op == "sign" {
		priv, err := parsePrivateKey(cfgString(config, "private_key", ""))
		if err != nil {
			return k, fmt.Errorf("private_key: %w", err)
		}
		k.private = priv
		return k, checkJWTKeyType(a, priv.Public())
	}
	pub, err := parsePublicKey(cfgString(config, "public_key", ""))
	if err != nil {
		return k, fmt.Errorf("public_key: %w", err)
	}
	k.public = pub
	return k, checkJWTKeyType(a, pub)
}

func checkJWTKeyType(a jwtAlg, pub crypto.PublicKey) error {
	switch pub.(type) {
	case *rsa.PublicKey:
		if a.family == "rsa" || a.family == "pss" {
			return nil
		}
	case *ecdsa.PublicKey:
		if a.family == "ecdsa" {
			return nil
		}
	}
	return fmt.Errorf("key type %T does not match the algorithm", pub)
}

func encodeJWT(header, payload map[string]interface{}, alg string, keys jwtKeys) (string, error) {
	hb, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("header: %w", err)
	}
	pb, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("payload: %w", err)
	}
	signing := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(pb)
	sig, err := jwtSign(keys, []byte(signing))
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func jwtDigest(a jwtAlg, data []byte) []byte {
	h := a.hash.newHash()
	h.Write(data)
	return h.Sum(nil)
}

func jwtSign(k jwtKeys, data []byte) ([]byte, error) {
	switch k.alg.family {
	case "hmac":
		m := hmac.New(k.alg.hash.newHash, k.secret)
		m.Write(data)
		return m.Sum(nil), nil
	case "rsa":
		return k.private.Sign(rand.Reader, jwtDigest(k.alg, data), k.alg.hash.id)
	case "pss":
		rk, ok := k.private.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PS algorithms need an RSA private key")
		}
		return rsa.SignPSS(rand.Reader, rk, k.alg.hash.id, jwtDigest(k.alg, data), &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	case "ecdsa":
		ek, ok := k.private.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("ES algorithms need an EC private key")
		}
		r, s, err := ecdsa.Sign(rand.Reader, ek, jwtDigest(k.alg, data))
		if err != nil {
			return nil, err
		}
		out := make([]byte, 2*k.alg.curve)
		r.FillBytes(out[:k.alg.curve])
		s.FillBytes(out[k.alg.curve:])
		return out, nil
	}
	return nil, fmt.Errorf("unsupported algorithm family %q", k.alg.family)
}

func verifyJWTSignature(alg string, k jwtKeys, data, sig []byte) error {
	switch k.alg.family {
	case "hmac":
		m := hmac.New(k.alg.hash.newHash, k.secret)
		m.Write(data)
		if !hmac.Equal(m.Sum(nil), sig) {
			return fmt.Errorf("invalid signature")
		}
		return nil
	case "rsa", "pss":
		pub, ok := k.public.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%s needs an RSA public key", alg)
		}
		if k.alg.family == "rsa" {
			return rsa.VerifyPKCS1v15(pub, k.alg.hash.id, jwtDigest(k.alg, data), sig)
		}
		return rsa.VerifyPSS(pub, k.alg.hash.id, jwtDigest(k.alg, data), sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto})
	case "ecdsa":
		pub, ok := k.public.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%s needs an EC public key", alg)
		}
		if len(sig) != 2*k.alg.curve {
			return fmt.Errorf("invalid signature length")
		}
		r := new(big.Int).SetBytes(sig[:k.alg.curve])
		s := new(big.Int).SetBytes(sig[k.alg.curve:])
		if !ecdsa.Verify(pub, jwtDigest(k.alg, data), r, s) {
			return fmt.Errorf("invalid signature")
		}
		return nil
	}
	return fmt.Errorf("unsupported algorithm %q", alg)
}
