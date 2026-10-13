package data

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

var jwtNow = time.Unix(1_700_000_000, 0)

func jwtNode() *JWTNode { return &JWTNode{now: func() time.Time { return jwtNow }} }

func jwtExec(t *testing.T, cfg map[string]interface{}, in ...workflow.Item) ([]map[string]interface{}, error) {
	t.Helper()
	if len(in) == 0 {
		in = []workflow.Item{{JSON: map[string]interface{}{}}}
	}
	out, err := jwtNode().Execute(context.Background(), workflow.NodeInput{Items: in}, cfg)
	if err != nil {
		return nil, err
	}
	var res []map[string]interface{}
	for _, it := range out[0].Items {
		res = append(res, it.JSON)
	}
	return res, nil
}

func TestJWTVerifiesNodeTokens(t *testing.T) {
	f := loadNodeFixtures(t)
	got, err := jwtExec(t, map[string]interface{}{"operation": "verify", "secret": f.JWTHS["secret"], "token": f.JWTHS["token"]})
	if err != nil || got[0]["payload"].(map[string]interface{})["sub"] != "1" {
		t.Fatalf("HS256: %v %v", got, err)
	}
	for alg, fx := range map[string]map[string]string{"ES256": f.JWTES, "RS256": f.JWTRS} {
		got, err := jwtExec(t, map[string]interface{}{"operation": "verify", "key_type": "pemKey", "algorithm": alg, "public_key": fx["public_key"], "token": fx["token"], "complete": true})
		if err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
		if got[0]["header"].(map[string]interface{})["alg"] != alg || got[0]["signature"] == "" {
			t.Errorf("%s complete: %v", alg, got[0])
		}
	}
	if _, err := jwtExec(t, map[string]interface{}{"operation": "verify", "secret": "wrong", "token": f.JWTHS["token"]}); err == nil || !strings.Contains(err.Error(), "can't be verified") {
		t.Errorf("wrong secret: %v", err)
	}
	if _, err := jwtExec(t, map[string]interface{}{"operation": "verify", "algorithm": "HS384", "secret": f.JWTHS["secret"], "token": f.JWTHS["token"]}); err == nil {
		t.Error("algorithm mismatch: expected error")
	}
}

func TestJWTSignVerifyRoundTrips(t *testing.T) {
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	rpriv, _ := x509.MarshalPKCS8PrivateKey(rk)
	rpub, _ := x509.MarshalPKIXPublicKey(&rk.PublicKey)
	keys := map[string][2]string{}
	for _, c := range []struct {
		curve elliptic.Curve
		algs  string
	}{{elliptic.P256(), "ES256"}, {elliptic.P384(), "ES384"}, {elliptic.P521(), "ES512"}} {
		ek, _ := ecdsa.GenerateKey(c.curve, rand.Reader)
		priv, _ := x509.MarshalECPrivateKey(ek)
		pub, _ := x509.MarshalPKIXPublicKey(&ek.PublicKey)
		keys[c.algs] = [2]string{pemEncode(t, "EC PRIVATE KEY", priv), pemEncode(t, "PUBLIC KEY", pub)}
	}
	for _, a := range []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512"} {
		keys[a] = [2]string{pemEncode(t, "PRIVATE KEY", rpriv), pemEncode(t, "PUBLIC KEY", rpub)}
	}
	for _, a := range []string{"HS256", "HS384", "HS512"} {
		keys[a] = [2]string{}
	}
	for alg, k := range keys {
		cfg := map[string]interface{}{"operation": "sign", "algorithm": alg, "key_type": "pem_key", "private_key": k[0], "public_key": k[1], "secret": "s",
			"claims": map[string]interface{}{"subject": "u1", "expiresIn": 60.0, "role": "admin"}, "kid": "k1"}
		if strings.HasPrefix(alg, "HS") {
			cfg["key_type"] = "passphrase"
		}
		signed, err := jwtExec(t, cfg)
		if err != nil {
			t.Fatalf("%s sign: %v", alg, err)
		}
		cfg["operation"] = "verify"
		cfg["token"] = signed[0]["token"]
		cfg["complete"] = true
		got, err := jwtExec(t, cfg)
		if err != nil {
			t.Fatalf("%s verify: %v", alg, err)
		}
		wantJSON(t, got[0]["payload"], `{"sub":"u1","exp":1700000060,"iat":1700000000,"role":"admin"}`)
		if h := got[0]["header"].(map[string]interface{}); h["kid"] != "k1" || h["typ"] != "JWT" {
			t.Errorf("%s header: %v", alg, h)
		}
	}
}

func TestJWTClaimsJSONDecodeAndTimeChecks(t *testing.T) {
	item := workflow.Item{JSON: map[string]interface{}{"id": "u9"}}
	signed, err := jwtExec(t, map[string]interface{}{"operation": "sign", "secret": "s", "use_json": true, "claims_json": `{"sub":"{{ $json.id }}","nbf":1700000100,"exp":1699999990}`, "header_claims": `{"cty":"x"}`}, item)
	if err != nil {
		t.Fatal(err)
	}
	token := signed[0]["token"].(string)
	dec, err := jwtExec(t, map[string]interface{}{"operation": "decode", "token": token})
	if err != nil || dec[0]["payload"].(map[string]interface{})["sub"] != "u9" {
		t.Fatalf("decode: %v %v", dec, err)
	}
	dec, _ = jwtExec(t, map[string]interface{}{"operation": "decode", "token": token, "complete": true})
	if dec[0]["header"].(map[string]interface{})["cty"] != "x" {
		t.Errorf("header claims: %v", dec[0]["header"])
	}
	base := map[string]interface{}{"operation": "verify", "secret": "s", "token": token}
	if _, err := jwtExec(t, base); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired: %v", err)
	}
	base["ignore_expiration"] = true
	if _, err := jwtExec(t, base); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Errorf("nbf: %v", err)
	}
	base["clock_tolerance"] = 200.0
	if _, err := jwtExec(t, base); err != nil {
		t.Errorf("tolerance: %v", err)
	}
	for _, cfg := range []map[string]interface{}{
		{"operation": "decode", "token": "a.b"},
		{"operation": "decode", "token": ""},
		{"operation": "sign"},
		{"operation": "sign", "algorithm": "RS256", "secret": "s"},
		{"operation": "sign", "algorithm": "none", "secret": "s"},
		{"operation": "rotate"},
	} {
		if _, err := jwtExec(t, cfg); err == nil {
			t.Errorf("%v: expected error", cfg)
		}
	}
}
