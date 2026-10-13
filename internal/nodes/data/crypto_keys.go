package data

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // OAEP-SHA1 offered for compatibility
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"hash"
	"regexp"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/scrypt"
)

// Key material for data.crypto and data.jwt.
//
// Symmetric encryption uses the same payload layout as n8n's Crypto node so
// values encrypted there decrypt here and vice versa:
//
//	base64( 0x01 | salt[16] | iv[12] | tag[16] | ciphertext )
//
// with the key derived by scrypt(passphrase, salt, N=65536, r=8, p=1).

const (
	symFormatVersion = 0x01
	symSaltLen       = 16
	symIVLen         = 12
	symTagLen        = 16
)

var symKeyLens = map[string]int{
	"aes-128-gcm": 16, "aes-192-gcm": 24, "aes-256-gcm": 32, "chacha20-poly1305": 32,
}

type cryptoKeys struct {
	secret     string
	signer     crypto.Signer
	mode       string
	passphrase string
	publicKey  *rsa.PublicKey
	privateKey *rsa.PrivateKey
}

func (k *cryptoKeys) loadCipherKeys(config map[string]interface{}, action string) error {
	k.mode = cfgString(config, "mode", "symmetric")
	switch k.mode {
	case "symmetric":
		k.passphrase = cfgString(config, "passphrase", "")
		if k.passphrase == "" {
			return fmt.Errorf("passphrase is required for symmetric mode")
		}
	case "asymmetric":
		if action == "encrypt" {
			pub, err := parsePublicKey(cfgString(config, "public_key", ""))
			if err != nil {
				return fmt.Errorf("public_key: %w", err)
			}
			rpub, ok := pub.(*rsa.PublicKey)
			if !ok {
				return fmt.Errorf("public_key: asymmetric encryption needs an RSA key, got %T", pub)
			}
			k.publicKey = rpub
		} else {
			priv, err := parsePrivateKey(cfgString(config, "private_key", ""))
			if err != nil {
				return fmt.Errorf("private_key: %w", err)
			}
			rpriv, ok := priv.(*rsa.PrivateKey)
			if !ok {
				return fmt.Errorf("private_key: asymmetric decryption needs an RSA key, got %T", priv)
			}
			k.privateKey = rpriv
		}
	default:
		return fmt.Errorf("mode: unknown value %q (want symmetric or asymmetric)", k.mode)
	}
	return nil
}

func oaepHash(config map[string]interface{}) (hash.Hash, error) {
	switch v := cfgString(config, "oaep_hash", "sha256"); v {
	case "sha256":
		return sha256.New(), nil
	case "sha1":
		return sha1.New(), nil //nolint:gosec
	default:
		return nil, fmt.Errorf("oaep_hash: unknown value %q (want sha256 or sha1)", v)
	}
}

func (k *cryptoKeys) encrypt(config map[string]interface{}, plaintext string) (string, error) {
	if k.mode == "asymmetric" {
		h, err := oaepHash(config)
		if err != nil {
			return "", err
		}
		ct, err := rsa.EncryptOAEP(h, rand.Reader, k.publicKey, []byte(plaintext), nil)
		if err != nil {
			return "", fmt.Errorf("plaintext is too large for the RSA key (use symmetric mode for larger data): %w", err)
		}
		return base64.StdEncoding.EncodeToString(ct), nil
	}
	name := cfgString(config, "cipher", "aes-256-gcm")
	salt := make([]byte, symSaltLen)
	iv := make([]byte, symIVLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	aead, err := symAEAD(name, k.passphrase, salt)
	if err != nil {
		return "", err
	}
	sealed := aead.Seal(nil, iv, []byte(plaintext), nil)
	ct, tag := sealed[:len(sealed)-symTagLen], sealed[len(sealed)-symTagLen:]
	payload := make([]byte, 0, 1+symSaltLen+symIVLen+symTagLen+len(ct))
	payload = append(payload, symFormatVersion)
	payload = append(payload, salt...)
	payload = append(payload, iv...)
	payload = append(payload, tag...)
	payload = append(payload, ct...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func (k *cryptoKeys) decrypt(config map[string]interface{}, value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("value is not base64: %w", err)
	}
	if k.mode == "asymmetric" {
		h, err := oaepHash(config)
		if err != nil {
			return "", err
		}
		pt, err := rsa.DecryptOAEP(h, nil, k.privateKey, raw, nil)
		if err != nil {
			return "", fmt.Errorf("decryption failed: wrong key or corrupted payload")
		}
		return string(pt), nil
	}
	header := 1 + symSaltLen + symIVLen + symTagLen
	if len(raw) < header {
		return "", fmt.Errorf("ciphertext is malformed or truncated")
	}
	if raw[0] != symFormatVersion {
		return "", fmt.Errorf("unsupported ciphertext version 0x%02x", raw[0])
	}
	salt := raw[1 : 1+symSaltLen]
	iv := raw[1+symSaltLen : 1+symSaltLen+symIVLen]
	tag := raw[1+symSaltLen+symIVLen : header]
	ct := raw[header:]
	aead, err := symAEAD(cfgString(config, "cipher", "aes-256-gcm"), k.passphrase, salt)
	if err != nil {
		return "", err
	}
	pt, err := aead.Open(nil, iv, append(append([]byte{}, ct...), tag...), nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: wrong passphrase, cipher, or corrupted payload")
	}
	return string(pt), nil
}

func symAEAD(name, passphrase string, salt []byte) (cipher.AEAD, error) {
	keyLen, ok := symKeyLens[name]
	if !ok {
		return nil, fmt.Errorf("cipher: unknown value %q (want aes-128-gcm, aes-192-gcm, aes-256-gcm or chacha20-poly1305)", name)
	}
	key, err := scrypt.Key([]byte(passphrase), salt, 65536, 8, 1, keyLen)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	if name == "chacha20-poly1305" {
		return chacha20poly1305.New(key)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

var pemHeader = regexp.MustCompile(`-----BEGIN ([A-Z ]+)-----`)

// normalizePEM repairs PEM text whose line breaks were lost (pasted on one
// line, or with literal "\n"), the way n8n's formatPemBlock does.
func normalizePEM(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, `\n`, "\n"))
	m := pemHeader.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	begin, end := "-----BEGIN "+m[1]+"-----", "-----END "+m[1]+"-----"
	i, j := strings.Index(s, begin), strings.Index(s, end)
	if i < 0 || j < 0 || j < i {
		return s
	}
	body := strings.Join(strings.Fields(s[i+len(begin):j]), "")
	var b strings.Builder
	b.WriteString(begin + "\n")
	for len(body) > 64 {
		b.WriteString(body[:64] + "\n")
		body = body[64:]
	}
	if body != "" {
		b.WriteString(body + "\n")
	}
	b.WriteString(end + "\n")
	return b.String()
}

// parsePrivateKey reads a PKCS#8, PKCS#1 (RSA) or SEC 1 (EC) PEM private key.
func parsePrivateKey(text string) (crypto.Signer, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("is required")
	}
	block, _ := pem.Decode([]byte(normalizePEM(text)))
	if block == nil {
		return nil, fmt.Errorf("not a PEM-encoded key")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		s, ok := k.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("unsupported key type %T", k)
		}
		return s, nil
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("unsupported private key format %q", block.Type)
}

// parsePublicKey reads a PKIX or PKCS#1 public key, or the key of a
// certificate.
func parsePublicKey(text string) (crypto.PublicKey, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("is required")
	}
	block, _ := pem.Decode([]byte(normalizePEM(text)))
	if block == nil {
		return nil, fmt.Errorf("not a PEM-encoded key")
	}
	if k, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return k, nil
	}
	if c, err := x509.ParseCertificate(block.Bytes); err == nil {
		return c.PublicKey, nil
	}
	return nil, fmt.Errorf("unsupported public key format %q", block.Type)
}
