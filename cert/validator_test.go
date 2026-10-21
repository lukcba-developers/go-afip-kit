package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// generateTestCertAndKey creates a self-signed cert + key for testing.
func generateTestCertAndKey(serialNumber string, notBefore, notAfter time.Time) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Country:      []string{"AR"},
			Organization: []string{"Test"},
			CommonName:   "Test",
			SerialNumber: serialNumber,
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return certPEM, keyPEM, nil
}

func TestValidate_Success(t *testing.T) {
	certPEM, keyPEM, err := generateTestCertAndKey(
		"CUIT 20123456786",
		time.Now().Add(-1*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Validate(certPEM, keyPEM, "20123456786")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DaysUntilExpiry < 364 {
		t.Errorf("expected ~365 days until expiry, got %d", result.DaysUntilExpiry)
	}
	if result.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
}

func TestValidate_CUITWithHyphens(t *testing.T) {
	certPEM, keyPEM, err := generateTestCertAndKey(
		"CUIT 20-12345678-6",
		time.Now().Add(-1*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Should match regardless of hyphens
	_, err = Validate(certPEM, keyPEM, "20123456786")
	if err != nil {
		t.Fatalf("should match CUIT ignoring hyphens: %v", err)
	}

	_, err = Validate(certPEM, keyPEM, "20-12345678-6")
	if err != nil {
		t.Fatalf("should match CUIT with hyphens: %v", err)
	}
}

func TestValidate_CUITMismatch(t *testing.T) {
	certPEM, keyPEM, err := generateTestCertAndKey(
		"CUIT 20123456786",
		time.Now().Add(-1*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Validate(certPEM, keyPEM, "30999999999")
	if err == nil {
		t.Fatal("expected error for CUIT mismatch")
	}
}

func TestValidate_Expired(t *testing.T) {
	certPEM, keyPEM, err := generateTestCertAndKey(
		"CUIT 20123456786",
		time.Now().Add(-2*365*24*time.Hour),
		time.Now().Add(-1*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Validate(certPEM, keyPEM, "20123456786")
	if err == nil {
		t.Fatal("expected error for expired cert")
	}
}

func TestValidate_NotYetValid(t *testing.T) {
	certPEM, keyPEM, err := generateTestCertAndKey(
		"CUIT 20123456786",
		time.Now().Add(24*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Validate(certPEM, keyPEM, "20123456786")
	if err == nil {
		t.Fatal("expected error for not-yet-valid cert")
	}
}

func TestValidate_KeyMismatch(t *testing.T) {
	certPEM, _, err := generateTestCertAndKey(
		"CUIT 20123456786",
		time.Now().Add(-1*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Generate a different key
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(otherKey),
	})

	_, err = Validate(certPEM, otherKeyPEM, "20123456786")
	if err == nil {
		t.Fatal("expected error for key mismatch")
	}
}

func TestValidate_InvalidPEM(t *testing.T) {
	_, err := Validate([]byte("not a cert"), []byte("not a key"), "")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestValidateCertOnly_Success(t *testing.T) {
	certPEM, _, err := generateTestCertAndKey(
		"CUIT 20123456786",
		time.Now().Add(-1*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ValidateCertOnly(certPEM, "20123456786")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SerialNumber != "CUIT 20123456786" {
		t.Errorf("unexpected serial: %s", result.SerialNumber)
	}
}

func TestValidate_InvalidExpectedCUITFailsClosed(t *testing.T) {
	certPEM, keyPEM, err := generateTestCertAndKey("CUIT 20123456786", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"abc", "2012345678", "20-1234-5"} {
		if _, err := Validate(certPEM, keyPEM, expected); err == nil {
			t.Errorf("Validate with expectedCUIT %q must fail, not skip the check", expected)
		}
		if _, err := ValidateCertOnly(certPEM, expected); err == nil {
			t.Errorf("ValidateCertOnly with expectedCUIT %q must fail, not skip the check", expected)
		}
	}
	// An empty expectedCUIT still skips the check, as documented.
	if _, err := Validate(certPEM, keyPEM, ""); err != nil {
		t.Errorf("empty expectedCUIT should skip the check: %v", err)
	}
}

func TestExtractCUIT(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"20123456786", "20123456786"},
		{"20-12345678-6", "20123456786"},
		{"CUIT 20123456786", "20123456786"},
		{"CUIT 20-12345678-6", "20123456786"},
		{"SERIALNUMBER=CUIT 20123456786", "20123456786"},
		{"abc", ""},                   // no valid CUIT
		{"12345", ""},                 // too short
	}
	for _, tt := range tests {
		got := extractCUIT(tt.input)
		if got != tt.expected {
			t.Errorf("extractCUIT(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseCertificatePEM_Invalid(t *testing.T) {
	_, err := ParseCertificatePEM([]byte("garbage"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePrivateKeyPEM_PKCS1(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	parsed, err := ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParsePrivateKeyPEM_PKCS8(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	parsed, err := ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParsePrivateKeyPEM_NonRSA(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(ecKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	_, err := ParsePrivateKeyPEM(keyPEM)
	if err == nil {
		t.Fatal("expected error for non-RSA key")
	}
}

func TestCUITDelCertificado_IgnoresExpiry(t *testing.T) {
	expired, _, err := generateTestCertAndKey("CUIT 20111111112", time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	cuit, err := CUITDelCertificado(expired)
	if err != nil || cuit != 20111111112 {
		t.Fatalf("got (%d, %v), want 20111111112", cuit, err)
	}

	// Validate still fails on expiry, but reports the CUIT in the result.
	res, err := ValidateCertOnly(expired, "")
	if err == nil {
		t.Fatal("expected expiry error")
	}
	if res == nil || res.CUIT != 20111111112 {
		t.Errorf("expected CUIT in the result even when expired, got %+v", res)
	}

	noCUIT, _, _ := generateTestCertAndKey("", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if _, err := CUITDelCertificado(noCUIT); err == nil {
		t.Error("expected error for a certificate without CUIT")
	}
	if _, err := CUITDelCertificado([]byte("not pem")); err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestValidate_ExpectedCUITWithBadCheckDigit(t *testing.T) {
	certPEM, keyPEM, err := generateTestCertAndKey("CUIT 20111111112", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(certPEM, keyPEM, "20-11111111-2"); err != nil {
		t.Fatalf("formatted valid CUIT should match: %v", err)
	}
	if _, err := Validate(certPEM, keyPEM, "20111111113"); err == nil {
		t.Error("expected error for a CUIT with an invalid check digit")
	}
}
