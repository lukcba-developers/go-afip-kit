package soap

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testRequest struct {
	XMLName xml.Name `xml:"TestRequest"`
	XMLNS   string   `xml:"xmlns,attr"`
	Value   string   `xml:"Value"`
}

type testResponse struct {
	XMLName xml.Name `xml:"TestResponse"`
	Result  string   `xml:"Result"`
}

func TestCall_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "text/xml; charset=utf-8" {
			t.Errorf("expected content-type text/xml, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("SOAPAction") != "TestAction" {
			t.Errorf("expected SOAPAction TestAction, got %s", r.Header.Get("SOAPAction"))
		}

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <TestResponse><Result>OK</Result></TestResponse>
  </soap:Body>
</soap:Envelope>`))
	}))
	defer server.Close()

	client := NewClient(WithTimeout(5 * time.Second))
	req := testRequest{XMLNS: "http://test.example.com/", Value: "hello"}
	var resp testResponse

	err := client.Call(context.Background(), server.URL, "TestAction", &req, &resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Result != "OK" {
		t.Errorf("expected Result=OK, got %s", resp.Result)
	}
}

func TestCall_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewClient()
	req := testRequest{Value: "test"}
	var resp testResponse

	err := client.Call(context.Background(), server.URL, "", &req, &resp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCall_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	client := NewClient(WithTimeout(10 * time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req := testRequest{Value: "test"}
	var resp testResponse

	err := client.Call(ctx, server.URL, "", &req, &resp)
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
}

func TestCall_SOAPFault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <soap:Fault>
      <faultcode>soap:Server</faultcode>
      <faultstring>El servicio no esta disponible</faultstring>
      <detail>WSAA offline</detail>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	req := testRequest{Value: "test"}
	var resp testResponse

	err := client.Call(context.Background(), server.URL, "", &req, &resp)
	if err == nil {
		t.Fatal("expected error for SOAP fault")
	}

	fault, ok := err.(*Fault)
	if !ok {
		t.Fatalf("expected *Fault, got %T: %v", err, err)
	}
	if fault.Code != "soap:Server" {
		t.Errorf("expected fault code soap:Server, got %s", fault.Code)
	}
}

func TestCall_SOAPFaultInHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <soap:Fault>
      <faultcode>soap:Client</faultcode>
      <faultstring>Invalid request</faultstring>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	req := testRequest{Value: "test"}
	var resp testResponse

	err := client.Call(context.Background(), server.URL, "", &req, &resp)
	if err == nil {
		t.Fatal("expected error")
	}

	_, ok := err.(*Fault)
	if !ok {
		t.Fatalf("expected *Fault from HTTP 500 with SOAP fault body, got %T", err)
	}
}

// TestCall_EmptySOAPActionSendsHeader verifies an empty action is still sent
// as the header `SOAPAction: ""` (SOAP 1.1): Axis rejects a request without
// it with Client.NoSOAPAction.
func TestCall_EmptySOAPActionSendsHeader(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Values("SOAPAction")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body><TestResponse><Result>OK</Result></TestResponse></soap:Body>
</soap:Envelope>`))
	}))
	defer server.Close()

	var resp testResponse
	if err := NewClient().Call(context.Background(), server.URL, "", &testRequest{Value: "x"}, &resp); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != `""` {
		t.Fatalf(`SOAPAction = %q, want [""]`, got)
	}
}
