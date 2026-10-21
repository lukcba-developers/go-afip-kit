// Package soap provides a minimal SOAP 1.1 client for AFIP web services.
//
// It handles envelope wrapping/unwrapping, HTTP transport, SOAP fault detection,
// and configurable timeouts. No WSDL parsing — callers provide typed request/response structs.
package soap

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// DefaultMaxResponseSize caps how much of a response body is read (10 MiB).
// AFIP responses are a few KB; the cap protects against a misbehaving or
// spoofed endpoint exhausting memory.
const DefaultMaxResponseSize = 10 << 20

// Client is a SOAP 1.1 HTTP client.
type Client struct {
	httpClient      *http.Client
	maxRetries      int
	retryBase       time.Duration
	logger          *slog.Logger
	maxResponseSize int64
}

// Option configures the SOAP client.
type Option func(*Client)

// WithTimeout sets the HTTP client timeout. Default is 30s.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithHTTPClient replaces the underlying HTTP client entirely. The caller's
// client keeps its own redirect policy; the default client never follows
// redirects (see NewClient).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithRetry enables automatic retries for transient failures: network errors
// and HTTP 5xx responses that carry no SOAP fault. SOAP faults and 4xx
// responses are never retried — they indicate a problem with the request, not
// the transport.
//
// maxRetries is the number of additional attempts after the first one;
// baseDelay is doubled on each retry (baseDelay, 2x, 4x, ...). Retries respect
// context cancellation.
//
// Note on FECAESolicitar: a retried request might hit AFIP after the original
// was actually processed. Pair retries with wsfev1.SolicitarCAEConReproceso,
// which recovers the CAE when AFIP answers 10016 and the registered
// comprobante matches the one sent.
func WithRetry(maxRetries int, baseDelay time.Duration) Option {
	return func(c *Client) {
		c.maxRetries = maxRetries
		c.retryBase = baseDelay
	}
}

// WithMaxResponseSize sets the maximum response body size in bytes. Larger
// responses fail with an error. Default is DefaultMaxResponseSize.
func WithMaxResponseSize(n int64) Option {
	return func(c *Client) {
		c.maxResponseSize = n
	}
}

// WithLogger sets a logger for retry attempts. Without it, retries are silent.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		c.logger = l
	}
}

// NewClient creates a SOAP client with the given options.
//
// The default HTTP client does not follow redirects: the request body carries
// the WSAA token and sign, and a 307/308 would resend it to whatever host the
// redirect points to. A redirect response fails as an HTTP error instead.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxResponseSize: DefaultMaxResponseSize,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Fault represents a SOAP Fault element returned by the server.
type Fault struct {
	Code   string `xml:"faultcode"`
	String string `xml:"faultstring"`
	Detail string `xml:"detail"`
}

func (f *Fault) Error() string {
	return fmt.Sprintf("soap fault: [%s] %s", f.Code, f.String)
}

// faultEnvelope is used to detect SOAP faults in the response body.
type faultEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Fault *Fault `xml:"Fault"`
	} `xml:"Body"`
}

// Call performs a SOAP request: marshals request into a SOAP envelope, sends it
// via HTTP POST, and unmarshals the response body into the provided struct.
//
// It detects and returns SOAP faults as *Fault errors.
// The soapAction is sent as the SOAPAction HTTP header; an empty soapAction
// is sent as `""`, since SOAP 1.1 requires the header to be present.
func (c *Client) Call(ctx context.Context, url, soapAction string, request, response any) error {
	// 1. Marshal request body
	bodyXML, err := xml.Marshal(request)
	if err != nil {
		return fmt.Errorf("soap: marshal request: %w", err)
	}

	// 2. Wrap in SOAP envelope
	envelope := NewEnvelope(bodyXML)
	envXML, err := xml.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("soap: marshal envelope: %w", err)
	}

	// Prepend XML declaration
	fullXML := append([]byte(xml.Header), envXML...)

	// 3. HTTP POST, with retries for transient failures
	respBody, err := c.post(ctx, url, soapAction, fullXML)
	if err != nil {
		return err
	}

	// 5. Check for SOAP fault in 200 response
	if fault := extractFault(respBody); fault != nil {
		return fault
	}

	// 6. Unwrap SOAP envelope
	var respEnvelope ResponseEnvelope
	if err := xml.Unmarshal(respBody, &respEnvelope); err != nil {
		return fmt.Errorf("soap: unmarshal envelope: %w (body: %s)", err, truncate(respBody, 300))
	}

	// 7. Unmarshal inner body into caller's response struct
	if err := xml.Unmarshal(respEnvelope.Body.Content, response); err != nil {
		return fmt.Errorf("soap: unmarshal response body: %w", err)
	}

	return nil
}

// post sends the envelope and returns the response body. It retries network
// errors and 5xx-without-fault responses up to c.maxRetries times with
// exponential backoff. A SOAP fault or non-5xx HTTP error returns immediately.
func (c *Client) post(ctx context.Context, url, soapAction string, body []byte) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryBase << (attempt - 1)
			if c.logger != nil {
				c.logger.Warn("soap: retrying request", "url", url, "attempt", attempt, "delay", delay, "error", lastErr)
			}
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("soap: %w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("soap: create request: %w", err)
		}
		req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		// SOAP 1.1 exige el header SOAPAction siempre. Sin acción va como
		// `""`: Axis (WSAA LoginCms) rechaza el request si falta el header
		// con el fault Client.NoSOAPAction.
		if soapAction == "" {
			soapAction = `""`
		}
		req.Header.Set("SOAPAction", soapAction)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("soap: http call: %w", err)
			}
			lastErr = fmt.Errorf("soap: http call: %w", err)
			continue // network error — retryable
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseSize+1))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("soap: read response: %w", err)
			continue // truncated read — retryable
		}
		if int64(len(respBody)) > c.maxResponseSize {
			return nil, fmt.Errorf("soap: response exceeds %d bytes", c.maxResponseSize)
		}

		if resp.StatusCode != http.StatusOK {
			// A SOAP fault means the server understood and rejected the
			// request — retrying won't change the outcome.
			if fault := extractFault(respBody); fault != nil {
				return nil, fault
			}
			httpErr := fmt.Errorf("soap: http status %d: %s", resp.StatusCode, truncate(respBody, 500))
			if resp.StatusCode >= 500 {
				lastErr = httpErr
				continue // server-side transient — retryable
			}
			return nil, httpErr
		}

		return respBody, nil
	}

	return nil, lastErr
}

// extractFault attempts to parse a SOAP Fault from response bytes.
// Returns nil if no fault is found.
func extractFault(body []byte) *Fault {
	// Quick check to avoid unnecessary parsing
	if !strings.Contains(string(body), "Fault") {
		return nil
	}
	var fe faultEnvelope
	if err := xml.Unmarshal(body, &fe); err != nil {
		return nil
	}
	return fe.Body.Fault
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
