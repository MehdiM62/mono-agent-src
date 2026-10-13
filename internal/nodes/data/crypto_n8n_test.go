package data

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

type nodeFixtures struct {
	N8nAES    map[string]string `json:"n8n_aes256gcm"`
	N8nChacha map[string]string `json:"n8n_chacha"`
	RIPEMD    string            `json:"ripemd160_abc"`
	SHA3      string            `json:"sha3_256_abc"`
	JWTHS     map[string]string `json:"jwt_hs256"`
	JWTES     map[string]string `json:"jwt_es256"`
	JWTRS     map[string]string `json:"jwt_rs256"`
}

// loadNodeFixtures reads values produced by Node.js's crypto module (the
// runtime n8n uses), so these tests check interoperability, not just
// round trips.
func loadNodeFixtures(t *testing.T) nodeFixtures {
	t.Helper()
	b, err := os.ReadFile("testdata/crypto/node_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f nodeFixtures
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func cryptoRun(t *testing.T, cfg map[string]interface{}, in ...workflow.Item) []map[string]interface{} {
	t.Helper()
	if len(in) == 0 {
		in = []workflow.Item{{JSON: map[string]interface{}{"x": "abc"}}}
	}
	return run(t, &CryptoNode{}, in, cfg)
}

func pemEncode(t *testing.T, typ string, der []byte) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
}

func TestCryptoHashAndHMAC(t *testing.T) {
	f := loadNodeFixtures(t)
	got := cryptoRun(t, map[string]interface{}{"operation": "hash", "type": "RIPEMD160", "value": "{{ $json.x }}"})
	if got[0]["data"] != f.RIPEMD {
		t.Errorf("ripemd160: %v", got[0]["data"])
	}
	got = cryptoRun(t, map[string]interface{}{"operation": "hash", "type": "SHA3-256", "value": "abc", "data_property_name": "out.h"})
	if got[0]["out"].(map[string]interface{})["h"] != f.SHA3 {
		t.Errorf("sha3-256: %v", got[0])
	}
	sum := sha256.Sum256([]byte("abc"))
	got = cryptoRun(t, map[string]interface{}{"operation": "hash", "value": "abc", "encoding": "base64"})
	if got[0]["data"] != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Errorf("sha256 base64: %v", got[0]["data"])
	}
	for name := range hashTypes {
		if len(cryptoRun(t, map[string]interface{}{"operation": "hash", "type": name, "value": "abc"})[0]["data"].(string)) == 0 {
			t.Errorf("%s: empty digest", name)
		}
	}
	bin := workflow.Item{JSON: map[string]interface{}{"file": base64.StdEncoding.EncodeToString([]byte("abc"))}}
	got = cryptoRun(t, map[string]interface{}{"operation": "hash", "type": "SHA256", "binary_data": true, "binary_property_name": "file"}, bin)
	if got[0]["data"] != hex.EncodeToString(sum[:]) {
		t.Errorf("binary hash: %v", got[0]["data"])
	}
	// RFC 4231 test case 2.
	got = cryptoRun(t, map[string]interface{}{"operation": "hmac", "type": "SHA256", "secret": "Jefe", "value": "what do ya want for nothing?"})
	if got[0]["data"] != "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Errorf("hmac: %v", got[0]["data"])
	}
	if _, err := (&CryptoNode{}).Execute(context.Background(), workflow.NodeInput{Items: []workflow.Item{{}}}, map[string]interface{}{"operation": "hmac", "value": "x"}); err == nil {
		t.Error("hmac without secret: expected error")
	}
	if _, err := (&CryptoNode{}).Execute(context.Background(), workflow.NodeInput{Items: []workflow.Item{{}}}, map[string]interface{}{"operation": "hash", "type": "MD4", "value": "x"}); err == nil {
		t.Error("unsupported hash: expected error")
	}
}

func TestCryptoGenerate(t *testing.T) {
	for kind, pattern := range map[string]string{
		"uuid":   `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
		"hex":    `^[0-9a-f]{20}$`,
		"base64": `^\w{20}$`,
		"ascii":  `^[!-~]{20}$`,
	} {
		got := cryptoRun(t, map[string]interface{}{"operation": "generate", "encoding_type": kind, "string_length": 20.0})
		if s, _ := got[0]["data"].(string); !regexp.MustCompile(pattern).MatchString(s) {
			t.Errorf("%s: %q", kind, s)
		}
	}
}

func TestCryptoSign(t *testing.T) {
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(rk)
	// One-line PEM (line breaks lost when pasted) must still parse.
	oneLine := strings.ReplaceAll(pemEncode(t, "PRIVATE KEY", der), "\n", " ")
	got := cryptoRun(t, map[string]interface{}{"operation": "sign", "algorithm": "RSA-SHA256", "private_key": oneLine, "value": "sign me"})
	sig, _ := hex.DecodeString(got[0]["data"].(string))
	digest := sha256.Sum256([]byte("sign me"))
	if err := rsa.VerifyPKCS1v15(&rk.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("rsa signature does not verify: %v", err)
	}

	ek, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	eder, _ := x509.MarshalECPrivateKey(ek)
	got = cryptoRun(t, map[string]interface{}{"operation": "sign", "algorithm": "sha256", "private_key": pemEncode(t, "EC PRIVATE KEY", eder), "value": "sign me", "encoding": "base64"})
	esig, _ := base64.StdEncoding.DecodeString(got[0]["data"].(string))
	if !ecdsa.VerifyASN1(&ek.PublicKey, digest[:], esig) {
		t.Error("ecdsa signature does not verify")
	}

	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	edder, _ := x509.MarshalPKCS8PrivateKey(edk)
	got = cryptoRun(t, map[string]interface{}{"operation": "sign", "private_key": pemEncode(t, "PRIVATE KEY", edder), "value": "sign me"})
	edsig, _ := hex.DecodeString(got[0]["data"].(string))
	if !ed25519.Verify(edk.Public().(ed25519.PublicKey), []byte("sign me"), edsig) {
		t.Error("ed25519 signature does not verify")
	}
	if _, err := (&CryptoNode{}).Execute(context.Background(), workflow.NodeInput{Items: []workflow.Item{{}}}, map[string]interface{}{"operation": "sign", "private_key": "garbage"}); err == nil {
		t.Error("bad key: expected error")
	}
}

func TestCryptoEncryptDecrypt(t *testing.T) {
	f := loadNodeFixtures(t)
	for _, fx := range []map[string]string{f.N8nAES, f.N8nChacha} {
		got := cryptoRun(t, map[string]interface{}{"operation": "decrypt", "passphrase": fx["passphrase"], "cipher": fx["cipher"], "value": fx["value"]})
		if got[0]["data"] != fx["plaintext"] {
			t.Errorf("decrypt n8n %s payload: %v", fx["cipher"], got[0]["data"])
		}
	}
	for _, c := range []string{"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-poly1305"} {
		enc := cryptoRun(t, map[string]interface{}{"operation": "encrypt", "passphrase": "pw", "cipher": c, "value": "round trip"})
		dec := cryptoRun(t, map[string]interface{}{"operation": "decrypt", "passphrase": "pw", "cipher": c, "value": enc[0]["data"]})
		if dec[0]["data"] != "round trip" {
			t.Errorf("%s round trip: %v", c, dec[0]["data"])
		}
	}
	if _, err := (&CryptoNode{}).Execute(context.Background(), workflow.NodeInput{Items: []workflow.Item{{}}}, map[string]interface{}{"operation": "decrypt", "passphrase": "wrong", "value": f.N8nAES["value"]}); err == nil || !strings.Contains(err.Error(), "decryption failed") {
		t.Errorf("wrong passphrase: err = %v", err)
	}

	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	pubDER, _ := x509.MarshalPKIXPublicKey(&rk.PublicKey)
	privDER := x509.MarshalPKCS1PrivateKey(rk)
	for _, h := range []string{"sha256", "sha1"} {
		enc := cryptoRun(t, map[string]interface{}{"operation": "encrypt", "mode": "asymmetric", "oaep_hash": h, "public_key": pemEncode(t, "PUBLIC KEY", pubDER), "value": "rsa secret"})
		dec := cryptoRun(t, map[string]interface{}{"operation": "decrypt", "mode": "asymmetric", "oaep_hash": h, "private_key": pemEncode(t, "RSA PRIVATE KEY", privDER), "value": enc[0]["data"]})
		if dec[0]["data"] != "rsa secret" {
			t.Errorf("rsa oaep %s round trip: %v", h, dec[0]["data"])
		}
	}
}

func TestCryptoLegacyOperationsUnchanged(t *testing.T) {
	got := cryptoRun(t, map[string]interface{}{"operation": "hash_md5", "field": "x"})
	if got[0]["x"] != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("hash_md5: %v", got[0]["x"])
	}
}
