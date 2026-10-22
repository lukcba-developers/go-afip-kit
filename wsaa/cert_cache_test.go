package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lukcba-developers/go-afip-kit/soap"
)

// certFake issues a token named after the certificate that logged in, and
// rejects the certificates listed in untrusted the way WSAA does.
type certFake struct {
	calls     int32
	untrusted map[string]bool
}

func (f *certFake) Authenticate(_ context.Context, cert *x509.Certificate, _ *rsa.PrivateKey, _ string, _ bool) (*TokenAcceso, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.untrusted[string(cert.Raw)] {
		return nil, &soap.Fault{Code: "ns1:" + FaultCertUntrusted, String: "Certificado no emitido por AC de confianza"}
	}
	return &TokenAcceso{Token: "tok-" + string(cert.Raw), Sign: "s", Expiration: time.Now().Add(12 * time.Hour)}, nil
}

// TestCachedClient_TokenIsPerCertificate verifies that a certificate never
// receives the token another certificate obtained for the same CUIT.
func TestCachedClient_TokenIsPerCertificate(t *testing.T) {
	ctx := context.Background()
	certA := &x509.Certificate{Raw: []byte("A")}
	certB := &x509.Certificate{Raw: []byte("B")}
	fake := &certFake{}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	tokA, err := cc.Authenticate(ctx, certA, nil, 20111111112, "wsfe", true)
	if err != nil {
		t.Fatal(err)
	}
	tokB, err := cc.Authenticate(ctx, certB, nil, 20111111112, "wsfe", true)
	if err != nil {
		t.Fatal(err)
	}
	if tokA.Token != "tok-A" || tokB.Token != "tok-B" {
		t.Fatalf("each certificate must get its own token, got %q and %q", tokA.Token, tokB.Token)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Fatalf("expected one LoginCms per certificate, got %d", n)
	}

	// Both stay cached under their own key.
	if tok, _ := cc.Authenticate(ctx, certA, nil, 20111111112, "wsfe", true); tok == nil || tok.Token != "tok-A" {
		t.Errorf("certificate A should hit its own cache entry, got %+v", tok)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 2 {
		t.Fatalf("expected a cache hit, got %d LoginCms", n)
	}
}

// TestCachedClient_RejectedCertificateGetsNoCachedToken verifies that a
// certificate WSAA does not accept gets the WSAA error, never the cached token
// of another certificate with the same CUIT. The same holds for Renew and
// CallWithAuthRetry.
func TestCachedClient_RejectedCertificateGetsNoCachedToken(t *testing.T) {
	ctx := context.Background()
	good := &x509.Certificate{Raw: []byte("good")}
	other := &x509.Certificate{Raw: []byte("other")}
	fake := &certFake{untrusted: map[string]bool{"other": true}}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	if _, err := cc.Authenticate(ctx, good, nil, 20111111112, "wsfe", true); err != nil {
		t.Fatal(err)
	}

	tok, err := cc.Authenticate(ctx, other, nil, 20111111112, "wsfe", true)
	if !IsCertUntrusted(err) || tok != nil {
		t.Fatalf("expected cms.cert.untrusted and no token, got %+v, %v", tok, err)
	}
	if tok, err := cc.Renew(ctx, other, nil, 20111111112, "wsfe", true, nil); !IsCertUntrusted(err) || tok != nil {
		t.Fatalf("Renew: expected cms.cert.untrusted and no token, got %+v, %v", tok, err)
	}
	called := false
	err = cc.CallWithAuthRetry(ctx, other, nil, 20111111112, "wsfe", true, func(*TokenAcceso) error {
		called = true
		return nil
	})
	if !IsCertUntrusted(err) || called {
		t.Fatalf("CallWithAuthRetry: expected cms.cert.untrusted without calling, got %v (called=%v)", err, called)
	}
}

func TestCachedClient_NilCertificate(t *testing.T) {
	ctx := context.Background()
	fake := &certFake{}
	cc := NewCachedClient(nil, NewMemoryStore())
	cc.client = fake

	if _, err := cc.Authenticate(ctx, nil, nil, 20111111112, "wsfe", true); !errors.Is(err, ErrNilCertificate) {
		t.Errorf("Authenticate: expected ErrNilCertificate, got %v", err)
	}
	if _, err := cc.Renew(ctx, nil, nil, 20111111112, "wsfe", true, nil); !errors.Is(err, ErrNilCertificate) {
		t.Errorf("Renew: expected ErrNilCertificate, got %v", err)
	}
	if err := cc.Invalidate(ctx, nil, 20111111112, "wsfe", true); !errors.Is(err, ErrNilCertificate) {
		t.Errorf("Invalidate: expected ErrNilCertificate, got %v", err)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 0 {
		t.Fatalf("must not call LoginCms without a certificate, got %d", n)
	}
}

// TestLocker_IsPerCertificate verifies the distributed lock key includes the
// certificate: a login of one certificate does not wait for another's.
func TestLocker_IsPerCertificate(t *testing.T) {
	certA := &x509.Certificate{Raw: []byte("A")}
	certB := &x509.Certificate{Raw: []byte("B")}
	locker := NewMemoryLocker()
	kA, err := cacheKey(certA, 20111111112, "wsfe", true)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = locker.TryLock(context.Background(), kA+":lock", "other-pod", time.Minute)

	fake := &certFake{}
	cc := newReplica(fake, NewMemoryStore(), locker)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tok, err := cc.Authenticate(ctx, certB, nil, 20111111112, "wsfe", true)
	if err != nil {
		t.Fatalf("certificate B must not wait for A's lock: %v", err)
	}
	if tok.Token != "tok-B" {
		t.Errorf("got %q", tok.Token)
	}
}

// TestCacheKey_Format pins the key layout documented for TokenStore
// implementations: environment, CUIT, service and certificate fingerprint.
func TestCacheKey_Format(t *testing.T) {
	k, err := cacheKey(&x509.Certificate{Raw: []byte("A")}, 20111111112, "wsfe", false)
	if err != nil {
		t.Fatal(err)
	}
	// SHA-256("A")
	const want = "wsaa:homo:20111111112:wsfe:559aead08264d5795d3909718cdd05abd49572e84fe55590eef31a88a08fdffd"
	if k != want {
		t.Errorf("cacheKey = %q, want %q", k, want)
	}
}
