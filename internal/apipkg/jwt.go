package apipkg

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash"
	"math/big"
	"time"
)

// signJWT builds and signs the token a JWTInject describes.
func signJWT(j *JWTInject, sc Scope) (string, error) {
	secret, err := renderText(j.Secret, sc)
	if err != nil {
		return "", fmt.Errorf("secret: %w", err)
	}
	if secret == "" {
		return "", fmt.Errorf("the secret is empty")
	}
	header := map[string]any{"alg": j.Alg, "typ": "JWT"}
	if j.Kid != "" {
		kid, err := renderText(j.Kid, sc)
		if err != nil {
			return "", fmt.Errorf("kid: %w", err)
		}
		if kid != "" {
			header["kid"] = kid
		}
	}
	claims := map[string]any{}
	for k, v := range j.Claims {
		r, err := renderValue(v, sc)
		if err != nil {
			return "", fmt.Errorf("claims.%s: %w", k, err)
		}
		if !isEmpty(r) {
			claims[k] = r
		}
	}
	ttl := j.TTLs
	if ttl <= 0 {
		ttl = 300
	}
	now := time.Now().Unix()
	claims["iat"] = now
	claims["exp"] = now + int64(ttl)
	hb, _ := json.Marshal(header)
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(hb) + "." + enc.EncodeToString(cb)
	sig, err := jwtSign(j.Alg, j.SecretEncoding, secret, []byte(input))
	if err != nil {
		return "", err
	}
	return input + "." + enc.EncodeToString(sig), nil
}

func jwtSign(alg, encoding, secret string, input []byte) ([]byte, error) {
	switch alg {
	case "HS256", "HS384", "HS512":
		key := []byte(secret)
		switch encoding {
		case "hex":
			b, err := hex.DecodeString(secret)
			if err != nil {
				return nil, fmt.Errorf("secret is not hex: %w", err)
			}
			key = b
		case "base64":
			b, err := base64.StdEncoding.DecodeString(secret)
			if err != nil {
				return nil, fmt.Errorf("secret is not base64: %w", err)
			}
			key = b
		}
		var h func() hash.Hash
		switch alg {
		case "HS256":
			h = sha256.New
		case "HS384":
			h = sha512.New384
		default:
			h = sha512.New
		}
		m := hmac.New(h, key)
		m.Write(input)
		return m.Sum(nil), nil
	case "RS256", "ES256":
		block, _ := pem.Decode([]byte(secret))
		if block == nil {
			return nil, fmt.Errorf("%s needs a PEM private key", alg)
		}
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			if rk, err2 := x509.ParsePKCS1PrivateKey(block.Bytes); err2 == nil {
				k = rk
			} else if ek, err3 := x509.ParseECPrivateKey(block.Bytes); err3 == nil {
				k = ek
			} else {
				return nil, fmt.Errorf("parse private key: %w", err)
			}
		}
		sum := sha256.Sum256(input)
		switch key := k.(type) {
		case *rsa.PrivateKey:
			if alg != "RS256" {
				return nil, fmt.Errorf("an RSA key can't sign %s", alg)
			}
			return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		case *ecdsa.PrivateKey:
			if alg != "ES256" {
				return nil, fmt.Errorf("an EC key can't sign %s", alg)
			}
			r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
			if err != nil {
				return nil, err
			}
			out := make([]byte, 64)
			fill := func(n *big.Int, dst []byte) { b := n.Bytes(); copy(dst[len(dst)-len(b):], b) }
			fill(r, out[:32])
			fill(s, out[32:])
			return out, nil
		}
		return nil, fmt.Errorf("unsupported key type %T", k)
	}
	return nil, fmt.Errorf("unsupported alg %q (HS256/384/512, RS256, ES256)", alg)
}
