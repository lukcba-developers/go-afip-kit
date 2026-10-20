package wsaa

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"go.mozilla.org/pkcs7"
)

func TestSignCMS_UsesSHA256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test", SerialNumber: "CUIT 20111111112"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("<loginTicketRequest/>")
	cms, err := signCMS(data, cert, key)
	if err != nil {
		t.Fatalf("signCMS: %v", err)
	}

	p7, err := pkcs7.Parse(cms)
	if err != nil {
		t.Fatalf("parse CMS: %v", err)
	}
	if err := p7.Verify(); err != nil {
		t.Fatalf("CMS signature does not verify: %v", err)
	}
	if string(p7.Content) != string(data) {
		t.Error("CMS must embed the TRA (detached=false)")
	}
	if len(p7.Signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(p7.Signers))
	}
	if alg := p7.Signers[0].DigestAlgorithm.Algorithm; !alg.Equal(pkcs7.OIDDigestAlgorithmSHA256) {
		t.Errorf("expected SHA-256 digest, got %v", alg)
	}
}
