package wsaa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lukcba-developers/go-afip-kit/soap"
)

// TestAuthenticate_SendsSOAPAction verifies LoginCms carries the SOAPAction
// header. WSAA (Axis) rejects a request without it with Client.NoSOAPAction;
// its WSDL declares an empty action, sent as `""`.
func TestAuthenticate_SendsSOAPAction(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Values("SOAPAction")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(taResponse))
	}))
	t.Cleanup(srv.Close)

	c := &Client{soap: soap.NewClient(), baseURL: srv.URL}
	cert, key := testCertKey(t)
	if _, err := c.Authenticate(context.Background(), cert, key, "wsfe", false); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != `""` {
		t.Fatalf(`SOAPAction = %q, want [""]`, got)
	}
}

// TestFakeWSAA_RejectsMissingSOAPAction pins the fake server's Axis behavior,
// so the other tests in this package fail if the header goes missing again.
func TestFakeWSAA_RejectsMissingSOAPAction(t *testing.T) {
	f := &fakeWSAA{acceptSHA256: true}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Client.NoSOAPAction)", resp.StatusCode)
	}
}
