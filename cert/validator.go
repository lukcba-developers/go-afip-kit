package cert

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"regexp"
	"strings"
	"time"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

// cuitDigitsRegex matches exactly 11 digits.
var cuitDigitsRegex = regexp.MustCompile(`^\d{11}$`)

// cuitSearchRegex finds 11 consecutive digits within a string.
var cuitSearchRegex = regexp.MustCompile(`\d{11}`)

// ValidationResult contains details from certificate validation.
type ValidationResult struct {
	// Subject is the certificate's subject string.
	Subject string
	// SerialNumber is the subject's serial number (contains CUIT).
	SerialNumber string
	// NotBefore is when the certificate becomes valid.
	NotBefore time.Time
	// NotAfter is when the certificate expires.
	NotAfter time.Time
	// DaysUntilExpiry is the number of days until expiration.
	DaysUntilExpiry int
	// Fingerprint is the SHA-256 fingerprint hex string (for audit logging).
	Fingerprint string
}

// Validate performs comprehensive validation of a certificate and private key pair.
//
// Checks performed:
//  1. Certificate PEM format is valid
//  2. Certificate has not expired and is not yet-to-be-valid
//  3. CUIT in certificate matches expectedCUIT (exact match after normalization)
//  4. Private key matches the certificate (tls.X509KeyPair)
//
// expectedCUIT can be in any format: "20123456789", "20-12345678-9", etc.
// Pass empty string to skip CUIT check.
func Validate(certPEM, keyPEM []byte, expectedCUIT string) (*ValidationResult, error) {
	// 1. Parse certificate
	cert, err := ParseCertificatePEM(certPEM)
	if err != nil {
		return nil, &afipkit.ErrCertificate{Err: fmt.Errorf("PEM invalido: %w", err)}
	}

	result := buildResult(cert)

	// 2. Check validity period
	if err := checkValidity(cert); err != nil {
		return result, err
	}

	// 3. Verify CUIT match
	if expectedCUIT != "" {
		if err := matchCUIT(cert, expectedCUIT); err != nil {
			return result, &afipkit.ErrCertificate{Err: err}
		}
	}

	// 4. Verify key matches certificate
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return result, &afipkit.ErrCertificate{
			Err: fmt.Errorf("la clave privada no corresponde al certificado: %w", err),
		}
	}

	return result, nil
}

// ValidateCertOnly validates just the certificate (without private key).
// Useful for checking expiration and CUIT without requiring the key.
func ValidateCertOnly(certPEM []byte, expectedCUIT string) (*ValidationResult, error) {
	cert, err := ParseCertificatePEM(certPEM)
	if err != nil {
		return nil, &afipkit.ErrCertificate{Err: fmt.Errorf("PEM invalido: %w", err)}
	}

	result := buildResult(cert)

	if err := checkValidity(cert); err != nil {
		return result, err
	}

	if expectedCUIT != "" {
		if err := matchCUIT(cert, expectedCUIT); err != nil {
			return result, &afipkit.ErrCertificate{Err: err}
		}
	}

	return result, nil
}

func buildResult(cert *x509.Certificate) *ValidationResult {
	hash := sha256.Sum256(cert.Raw)
	return &ValidationResult{
		Subject:         cert.Subject.String(),
		SerialNumber:    cert.Subject.SerialNumber,
		NotBefore:       cert.NotBefore,
		NotAfter:        cert.NotAfter,
		DaysUntilExpiry: int(time.Until(cert.NotAfter).Hours() / 24),
		Fingerprint:     fmt.Sprintf("%x", hash),
	}
}

func checkValidity(cert *x509.Certificate) error {
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return &afipkit.ErrCertificate{
			Err: fmt.Errorf("certificado aun no es valido (valido desde %s)", cert.NotBefore.Format("2006-01-02")),
		}
	}
	if now.After(cert.NotAfter) {
		return &afipkit.ErrCertificate{
			Err: fmt.Errorf("certificado expirado (vencio el %s)", cert.NotAfter.Format("2006-01-02")),
		}
	}
	return nil
}

// matchCUIT verifies that the certificate's SerialNumber contains the expected CUIT.
// Extracts the 11-digit CUIT from both values and performs exact comparison.
// An expectedCUIT without 11 digits is an error, never a skipped check.
func matchCUIT(cert *x509.Certificate, expectedCUIT string) error {
	normalizedSerial := extractCUIT(cert.Subject.SerialNumber)
	normalizedExpected := extractCUIT(expectedCUIT)

	if normalizedExpected == "" {
		return fmt.Errorf("CUIT esperado invalido: %q", expectedCUIT)
	}

	if normalizedSerial != normalizedExpected {
		return fmt.Errorf("CUIT del certificado (%s) no coincide con el esperado (%s)",
			cert.Subject.SerialNumber, expectedCUIT)
	}
	return nil
}

// extractCUIT extracts the 11-digit CUIT from a string, stripping hyphens,
// spaces, and the "CUIT" prefix. Returns empty string if no valid CUIT found.
func extractCUIT(s string) string {
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ToUpper(s)
	s = strings.TrimPrefix(s, "CUIT")

	// Verify the result is exactly 11 digits
	if cuitDigitsRegex.MatchString(s) {
		return s
	}

	// Try to find 11 consecutive digits within the string
	return cuitSearchRegex.FindString(s)
}
