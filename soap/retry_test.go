package soap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const okEnvelope = `<?xml version="1.0"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body><PingResponse><Result>pong</Result></PingResponse></soap:Body>
</soap:Envelope>`

const faultEnvelope500 = `<?xml version="1.0"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body><soap:Fault>
    <faultcode>ns1:coe.alreadyAuthenticated</faultcode>
    <faultstring>El CEE ya posee un TA valido</faultstring>
  </soap:Fault></soap:Body>
</soap:Envelope>`

type pingRequest struct {
	XMLName struct{} `xml:"Ping"`
}

type pingResponse struct {
	XMLName struct{} `xml:"PingResponse"`
	Result  string   `xml:"Result"`
}

func TestCall_RetriesOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(okEnvelope))
	}))
	defer srv.Close()

	c := NewClient(WithRetry(3, time.Millisecond))
	var resp pingResponse
	if err := c.Call(context.Background(), srv.URL, "", &pingRequest{}, &resp); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if resp.Result != "pong" {
		t.Fatalf("got %q", resp.Result)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("expected 3 attempts, got %d", n)
	}
}

func TestCall_NoRetryOnSOAPFault(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(faultEnvelope500))
	}))
	defer srv.Close()

	c := NewClient(WithRetry(3, time.Millisecond))
	var resp pingResponse
	err := c.Call(context.Background(), srv.URL, "", &pingRequest{}, &resp)
	if err == nil {
		t.Fatal("expected fault error")
	}
	if _, ok := err.(*Fault); !ok {
		t.Fatalf("expected *Fault, got %T: %v", err, err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("SOAP faults must not be retried, got %d attempts", n)
	}
}

func TestCall_NoRetryWithoutOption(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewClient()
	var resp pingResponse
	if err := c.Call(context.Background(), srv.URL, "", &pingRequest{}, &resp); err == nil {
		t.Fatal("expected error")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("expected single attempt without WithRetry, got %d", n)
	}
}

func TestCall_RetryRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := NewClient(WithRetry(10, 50*time.Millisecond))
	var resp pingResponse
	start := time.Now()
	err := c.Call(ctx, srv.URL, "", &pingRequest{}, &resp)
	if err == nil {
		t.Fatal("expected error")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("context cancellation ignored, took %v", elapsed)
	}
}

func TestCall_NoRetryOn4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(WithRetry(3, time.Millisecond))
	var resp pingResponse
	if err := c.Call(context.Background(), srv.URL, "", &pingRequest{}, &resp); err == nil {
		t.Fatal("expected error")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("4xx must not be retried, got %d attempts", n)
	}
}
