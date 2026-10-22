package wsaa

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strings"
)

// legacyCacheKey is the TokenStore key used up to v0.6.x, without the
// certificate: "wsaa:{homo|prod}:{cuit}:{service}".
//
// CachedClient only reads it to migrate a still valid TA when WSAA answers
// coe.alreadyAuthenticated right after upgrading, and only if tokenIssuedTo
// confirms the TA belongs to the same certificate. Without this, the upgrade
// would leave each certificate without a token until its old TA expired (up
// to 12 h). It will be removed in a future version.
func legacyCacheKey(cuit int64, service string, production bool) string {
	env := "homo"
	if production {
		env = "prod"
	}
	return fmt.Sprintf("wsaa:%s:%d:%s", env, cuit, service)
}

// ssoToken is the XML inside a WSAA token (WSAA Manual del Desarrollador,
// 6.3): login.uid is the DN of the certificate that obtained the TA, e.g.
// "SERIALNUMBER=CUIT 20190178154, CN=alias".
type ssoToken struct {
	Login struct {
		UID string `xml:"uid,attr"`
	} `xml:"operation>login"`
}

// tokenIssuedTo reports whether the WSAA token was issued to a certificate
// with the same CUIT (serialNumber) and alias (CN) as cert. Any decoding
// problem returns false.
//
// It is only meaningful after WSAA answered coe.alreadyAuthenticated to cert:
// that proves cert is trusted by WSAA and already holds a valid TA, so a
// self-signed certificate copying the subject never reaches this check.
func tokenIssuedTo(token string, cert *x509.Certificate) bool {
	if cert == nil || cert.Subject.SerialNumber == "" || cert.Subject.CommonName == "" {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(token), ""))
	if err != nil {
		return false
	}
	var sso ssoToken
	if err := xml.Unmarshal(raw, &sso); err != nil {
		return false
	}
	dn := parseDN(sso.Login.UID)
	return dn["SERIALNUMBER"] == cert.Subject.SerialNumber && dn["CN"] == cert.Subject.CommonName
}

// parseDN splits a DN like "SERIALNUMBER=CUIT 20190178154, CN=alias" into its
// attributes (upper-case names). Repeated attributes make it ambiguous: they
// clear the value so the comparison fails.
func parseDN(dn string) map[string]string {
	attrs := make(map[string]string)
	seen := make(map[string]bool)
	for _, part := range strings.Split(dn, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		name = strings.ToUpper(strings.TrimSpace(name))
		if seen[name] {
			attrs[name] = ""
			continue
		}
		seen[name] = true
		attrs[name] = strings.TrimSpace(value)
	}
	return attrs
}
