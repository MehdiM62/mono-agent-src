package connections

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
)

// validateGoogleServiceAccount checks a service account's fields offline:
// the email looks like one and the private key is an RSA key in PEM (with
// real or escaped "\n" line breaks, as JSON key files hold it). The token
// exchange happens when a node uses the connection.
func validateGoogleServiceAccount(c *Connection) (string, error) {
	email := strings.TrimSpace(getStr(c.Data, "client_email"))
	if !strings.Contains(email, "@") {
		return "", fmt.Errorf("client_email %q is not a service account email", email)
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(strings.TrimSpace(getStr(c.Data, "private_key")), `\n`, "\n")))
	if block == nil {
		return "", fmt.Errorf("private_key is not a PEM key")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if _, ok := k.(*rsa.PrivateKey); !ok {
			return "", fmt.Errorf("private_key is not an RSA key")
		}
		return email, nil
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		return "", fmt.Errorf("private_key is not a PKCS#8 or PKCS#1 RSA key")
	}
	return email, nil
}
