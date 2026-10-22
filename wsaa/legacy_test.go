package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lukcba-developers/go-afip-kit/soap"
)

// ssoXML builds a WSAA token like the one in the WSAA Manual del
// Desarrollador (6.3), issued to the certificate DN uid.
func ssoXML(uid string) string {
	xml := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sso version="2.0">
    <id src="CN=wsaahomo, O=AFIP, C=AR, SERIALNUMBER=CUIT 33693450239" dst="CN=wsfe, O=AFIP, C=AR" unique_id="1540869723" gen_time="1573830140" exp_time="1573873400"/>
    <operation type="login" value="granted">
        <login entity="33693450239" service="wsfe" uid="` + uid + `" authmethod="cms" regmethod="22">
            <relations>
                <relation key="20111111112" reltype="4"/>
            </relations>
        </login>
    </operation>
</sso>
`
	return base64.StdEncoding.EncodeToString([]byte(xml))
}

func arcaCert(raw, cuit, alias string) *x509.Certificate {
	return &x509.Certificate{Raw: []byte(raw), Subject: pkix.Name{SerialNumber: "CUIT " + cuit, CommonName: alias}}
}

// wsaaFault answers every LoginCms with the given WSAA fault.
type wsaaFault struct {
	code  string
	calls int32
}

func (f *wsaaFault) Authenticate(context.Context, *x509.Certificate, *rsa.PrivateKey, string, bool) (*TokenAcceso, error) {
	atomic.AddInt32(&f.calls, 1)
	return nil, &soap.Fault{Code: "ns1:" + f.code, String: f.code}
}

func storeLegacy(t *testing.T, store TokenStore, token string, exp time.Time) {
	t.Helper()
	if err := store.Set(context.Background(), legacyCacheKey(20111111112, "wsfe", true),
		&TokenAcceso{Token: token, Sign: "s", Expiration: exp}); err != nil {
		t.Fatal(err)
	}
}

// TestLegacyKey_MigratesTAOfSameCertificate verifies the upgrade path: WSAA
// answers coe.alreadyAuthenticated and the valid TA stored under the
// pre-v0.7.0 key was issued to this certificate, so it is reused and moved
// to the new key.
func TestLegacyKey_MigratesTAOfSameCertificate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	cert := arcaCert("cert", "20111111112", "facturacion")
	legacyTok := ssoXML("SERIALNUMBER=CUIT 20111111112, CN=facturacion")
	storeLegacy(t, store, legacyTok, time.Now().Add(6*time.Hour))

	fake := &wsaaFault{code: FaultAlreadyAuthenticated}
	cc := NewCachedClient(nil, store)
	cc.client = fake

	tok, err := cc.Authenticate(ctx, cert, nil, 20111111112, "wsfe", true)
	if err != nil {
		t.Fatalf("expected the migrated TA, got %v", err)
	}
	if tok.Token != legacyTok {
		t.Fatal("expected the TA stored under the legacy key")
	}
	k, _ := cacheKey(cert, 20111111112, "wsfe", true)
	if moved, _ := store.Get(ctx, k); moved == nil || moved.Token != legacyTok {
		t.Fatal("the TA should now be stored under the new key")
	}

	// Next call hits the new key: no LoginCms.
	if _, err := cc.Authenticate(ctx, cert, nil, 20111111112, "wsfe", true); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 1 {
		t.Fatalf("expected a single LoginCms, got %d", n)
	}
}

// TestLegacyKey_NotMigrated covers every case where the legacy TA must NOT be
// returned: the error from WSAA is returned instead.
func TestLegacyKey_NotMigrated(t *testing.T) {
	valid := time.Now().Add(6 * time.Hour)
	sameUID := ssoXML("SERIALNUMBER=CUIT 20111111112, CN=facturacion")
	cert := arcaCert("cert", "20111111112", "facturacion")

	cases := []struct {
		name   string
		fault  string
		cert   *x509.Certificate
		token  string
		expiry time.Time
	}{
		// WSAA did not authenticate the certificate (e.g. self-signed with
		// the same subject): no fallback, whatever the token says.
		{"untrusted certificate", FaultCertUntrusted, cert, sameUID, valid},
		{"other CUIT", FaultAlreadyAuthenticated, arcaCert("y", "20222222223", "facturacion"), sameUID, valid},
		{"other alias", FaultAlreadyAuthenticated, arcaCert("z", "20111111112", "otro"), sameUID, valid},
		{"no subject", FaultAlreadyAuthenticated, &x509.Certificate{Raw: []byte("n")}, sameUID, valid},
		{"token not base64", FaultAlreadyAuthenticated, cert, "%%%", valid},
		{"token without uid", FaultAlreadyAuthenticated, cert, base64.StdEncoding.EncodeToString([]byte("<sso/>")), valid},
		{"repeated CN", FaultAlreadyAuthenticated, cert, ssoXML("SERIALNUMBER=CUIT 20111111112, CN=facturacion, CN=x"), valid},
		{"expired legacy TA", FaultAlreadyAuthenticated, cert, sameUID, time.Now().Add(-time.Minute)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := NewMemoryStore()
			storeLegacy(t, store, c.token, c.expiry)
			cc := NewCachedClient(nil, store)
			cc.client = &wsaaFault{code: c.fault}

			tok, err := cc.Authenticate(context.Background(), c.cert, nil, 20111111112, "wsfe", true)
			if err == nil || tok != nil {
				t.Fatalf("expected the WSAA error and no token, got %+v, %v", tok, err)
			}
			if FaultCode(err) != c.fault {
				t.Errorf("expected fault %s, got %v", c.fault, err)
			}
		})
	}
}

// TestLegacyKey_RenewSkipsRejectedTA verifies Renew does not hand back the
// legacy TA that the business service just rejected.
func TestLegacyKey_RenewSkipsRejectedTA(t *testing.T) {
	store := NewMemoryStore()
	cert := arcaCert("cert", "20111111112", "facturacion")
	legacyTok := ssoXML("SERIALNUMBER=CUIT 20111111112, CN=facturacion")
	storeLegacy(t, store, legacyTok, time.Now().Add(6*time.Hour))
	cc := NewCachedClient(nil, store)
	cc.client = &wsaaFault{code: FaultAlreadyAuthenticated}

	tok, err := cc.Renew(context.Background(), cert, nil, 20111111112, "wsfe", true, &TokenAcceso{Token: legacyTok})
	if !IsAlreadyAuthenticated(err) || tok != nil {
		t.Fatalf("expected coe.alreadyAuthenticated and no token, got %+v, %v", tok, err)
	}
}

func TestTokenIssuedTo_ManualExample(t *testing.T) {
	// uid exactly as printed in the WSAA Manual del Desarrollador.
	tok := ssoXML("SERIALNUMBER=CUIT 20190178154, CN=glarriera20181029")
	if !tokenIssuedTo(tok, arcaCert("c", "20190178154", "glarriera20181029")) {
		t.Fatal("the manual's token should match its certificate")
	}
	// A token wrapped across lines still decodes.
	if !tokenIssuedTo(tok[:40]+"\n"+tok[40:], arcaCert("c", "20190178154", "glarriera20181029")) {
		t.Fatal("whitespace in the token should be ignored")
	}
}
