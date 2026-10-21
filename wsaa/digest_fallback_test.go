package wsaa

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/lukcba-developers/go-afip-kit/soap"
	"go.mozilla.org/pkcs7"
)

func testCertKey(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
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
	return cert, key
}

var in0Re = regexp.MustCompile(`<in0>([^<]+)</in0>`)

const taResponse = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body>
<loginCmsResponse xmlns="http://wsaa.view.sua.dvadac.desein.afip.gov"><loginCmsReturn>&lt;loginTicketResponse version="1.0"&gt;&lt;header&gt;&lt;expirationTime&gt;2099-01-01T00:00:00-03:00&lt;/expirationTime&gt;&lt;/header&gt;&lt;credentials&gt;&lt;token&gt;TOK&lt;/token&gt;&lt;sign&gt;SIG&lt;/sign&gt;&lt;/credentials&gt;&lt;/loginTicketResponse&gt;</loginCmsReturn></loginCmsResponse>
</soapenv:Body></soapenv:Envelope>`

func faultResponse(code, msg string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault>
<faultcode xmlns:ns1="http://xml.apache.org/axis/">ns1:` + code + `</faultcode><faultstring>` + msg + `</faultstring>
</soapenv:Fault></soapenv:Body></soapenv:Envelope>`
}

// fakeWSAA records the digest of every CMS it receives. acceptSHA256 decides
// whether SHA-256 signatures get cms.sign.invalid; otherFault, if set, is
// returned for every request.
type fakeWSAA struct {
	acceptSHA256 bool
	otherFault   string

	mu      sync.Mutex
	digests []string
}

func (f *fakeWSAA) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Como Axis: sin header SOAPAction, LoginCms responde Client.NoSOAPAction.
		if len(r.Header.Values("SOAPAction")) == 0 {
			w.Header().Set("Content-Type", "text/xml")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(faultResponse("Client.NoSOAPAction", "no SOAPAction header!")))
			return
		}
		body, _ := io.ReadAll(r.Body)
		m := in0Re.FindSubmatch(body)
		if m == nil {
			t.Errorf("request without in0: %s", body)
			return
		}
		der, err := base64.StdEncoding.DecodeString(string(m[1]))
		if err != nil {
			t.Errorf("in0 is not base64: %v", err)
			return
		}
		p7, err := pkcs7.Parse(der)
		if err != nil || p7.Verify() != nil {
			t.Errorf("invalid CMS: %v", err)
			return
		}
		digest := "SHA-1"
		if p7.Signers[0].DigestAlgorithm.Algorithm.Equal(pkcs7.OIDDigestAlgorithmSHA256) {
			digest = "SHA-256"
		}
		f.mu.Lock()
		f.digests = append(f.digests, digest)
		f.mu.Unlock()

		w.Header().Set("Content-Type", "text/xml")
		switch {
		case f.otherFault != "":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(faultResponse(f.otherFault, "fault")))
		case digest == "SHA-256" && !f.acceptSHA256:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(faultResponse(FaultSignInvalid, "Firma inválida o algoritmo no soportado")))
		default:
			_, _ = w.Write([]byte(taResponse))
		}
	}
}

func (f *fakeWSAA) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.digests...)
}

func newFakeWSAAClient(t *testing.T, f *fakeWSAA) *Client {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return &Client{soap: soap.NewClient(), baseURL: srv.URL}
}

func TestAuthenticate_SHA256Accepted(t *testing.T) {
	f := &fakeWSAA{acceptSHA256: true}
	c := newFakeWSAAClient(t, f)
	cert, key := testCertKey(t)

	tok, err := c.Authenticate(context.Background(), cert, key, "wsfe", false)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token != "TOK" || tok.Sign != "SIG" {
		t.Errorf("unexpected token: %+v", tok)
	}
	if got := f.seen(); len(got) != 1 || got[0] != "SHA-256" {
		t.Errorf("expected a single SHA-256 login, got %v", got)
	}
	if c.SignatureDigest() != "SHA-256" {
		t.Errorf("SignatureDigest = %s", c.SignatureDigest())
	}
}

func TestAuthenticate_FallsBackToSHA1(t *testing.T) {
	f := &fakeWSAA{acceptSHA256: false}
	c := newFakeWSAAClient(t, f)
	cert, key := testCertKey(t)

	tok, err := c.Authenticate(context.Background(), cert, key, "wsfe", false)
	if err != nil {
		t.Fatalf("expected the SHA-1 retry to succeed, got %v", err)
	}
	if tok.Token != "TOK" {
		t.Errorf("unexpected token: %+v", tok)
	}
	if got := f.seen(); len(got) != 2 || got[0] != "SHA-256" || got[1] != "SHA-1" {
		t.Errorf("expected SHA-256 then SHA-1, got %v", got)
	}
	if c.SignatureDigest() != "SHA-1" {
		t.Errorf("SignatureDigest = %s, want SHA-1", c.SignatureDigest())
	}

	// The client remembers: next login goes straight to SHA-1.
	if _, err := c.Authenticate(context.Background(), cert, key, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if got := f.seen(); len(got) != 3 || got[2] != "SHA-1" {
		t.Errorf("expected the third login with SHA-1 only, got %v", got)
	}
}

func TestAuthenticate_OtherFaultsDoNotFallBack(t *testing.T) {
	f := &fakeWSAA{otherFault: FaultNotAuthorized}
	c := newFakeWSAAClient(t, f)
	cert, key := testCertKey(t)

	_, err := c.Authenticate(context.Background(), cert, key, "wsfe", false)
	if !IsNotAuthorized(err) {
		t.Fatalf("expected coe.notAuthorized, got %v", err)
	}
	if got := f.seen(); len(got) != 1 {
		t.Errorf("a non-signature fault must not trigger a retry, got %v", got)
	}
	if c.SignatureDigest() != "SHA-256" {
		t.Error("digest must stay SHA-256")
	}
}
