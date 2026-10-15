package connections

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestValidateGoogleServiceAccount(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	escaped := strings.ReplaceAll(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "\n", `\n`)
	c := &Connection{Platform: "google_service_account", Data: map[string]interface{}{
		"client_email": "sa@p.iam.gserviceaccount.com", "private_key": escaped}}
	id, err := ValidateConnection(context.Background(), c)
	if err != nil || id != "sa@p.iam.gserviceaccount.com" {
		t.Fatalf("id %q err %v", id, err)
	}
	for _, data := range []map[string]interface{}{
		{"client_email": "nope", "private_key": escaped},
		{"client_email": "sa@p", "private_key": "garbage"},
	} {
		if _, err := ValidateConnection(context.Background(), &Connection{Platform: "google_service_account", Data: data}); err == nil {
			t.Errorf("%v accepted", data["client_email"])
		}
	}
	p := Registry["google_service_account"]
	if p.Fields[MethodAPIKey][1].Key != "private_key" || !p.Fields[MethodAPIKey][1].Secret {
		t.Errorf("private_key field %+v", p.Fields[MethodAPIKey][1])
	}
}
