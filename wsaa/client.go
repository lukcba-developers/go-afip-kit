// Package wsaa implements the AFIP WSAA (Web Service de Autenticacion y Autorizacion)
// client for obtaining access tokens via CMS/PKCS#7 signed login tickets.
package wsaa

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"sync/atomic"
	"time"

	afipkit "github.com/lukcba-developers/go-afip-kit"
	"github.com/lukcba-developers/go-afip-kit/soap"
	"go.mozilla.org/pkcs7"
)

const (
	homoURL = "https://wsaahomo.afip.gov.ar/ws/services/LoginCms"
	prodURL = "https://wsaa.afip.gov.ar/ws/services/LoginCms"

	wsaaNamespace = "http://wsaa.view.sua.dvadac.desein.afip.gov"
)

// Client communicates with WSAA to obtain access tokens.
//
// The TRA is signed with SHA-256. The WSAA technical spec still documents
// SHA1+RSA, so if LoginCms answers cms.sign.invalid ("firma inválida o
// algoritmo no soportado") the login is retried once with SHA-1 and the client
// keeps using SHA-1 from then on. SignatureDigest reports which one is in use.
type Client struct {
	soap     *soap.Client
	baseURL  string      // overrides the WSAA URL; only set by tests
	sha1Only atomic.Bool // WSAA rejected SHA-256
}

// NewClient creates a WSAA client.
func NewClient(opts ...soap.Option) *Client {
	return &Client{
		soap: soap.NewClient(opts...),
	}
}

// Authenticate obtains a TokenAcceso from WSAA by signing a TRA with the
// provided certificate and private key.
//
// The service parameter identifies the target web service (e.g. "wsfe" for WSFEv1).
// Set production=true to use the production WSAA endpoint.
func (c *Client) Authenticate(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, service string, production bool) (*TokenAcceso, error) {
	if !c.sha1Only.Load() {
		token, err := c.login(ctx, cert, key, service, production, pkcs7.OIDDigestAlgorithmSHA256)
		if !IsSignatureInvalid(err) {
			return token, err
		}
		c.sha1Only.Store(true)
	}
	return c.login(ctx, cert, key, service, production, pkcs7.OIDDigestAlgorithmSHA1)
}

// SignatureDigest returns the digest used to sign the TRA: "SHA-256", or
// "SHA-1" once WSAA rejected SHA-256 with cms.sign.invalid.
func (c *Client) SignatureDigest() string {
	if c.sha1Only.Load() {
		return "SHA-1"
	}
	return "SHA-256"
}

// lastUniqueID is the last TRA uniqueId issued by this process.
var lastUniqueID atomic.Uint32

// nextUniqueID returns a TRA uniqueId. The loginTicketRequest schema declares
// it xs:unsignedInt (32 bits): a larger value makes WSAA answer xml.bad. It is
// the Unix time in seconds (valid until 2106), bumped when needed so two TRAs
// of the same process never share it (e.g. the SHA-1 retry in the same second).
func nextUniqueID(now time.Time) uint32 {
	for {
		last := lastUniqueID.Load()
		id := uint32(now.Unix())
		if id <= last {
			id = last + 1
		}
		if lastUniqueID.CompareAndSwap(last, id) {
			return id
		}
	}
}

// buildTRA returns the TRA XML (with its XML declaration, which WSAA requires)
// following loginTicketRequest.xsd: header (uniqueId, generationTime,
// expirationTime) and service.
func buildTRA(now time.Time, service string) ([]byte, error) {
	tra := loginTicketRequest{
		Version: "1.0",
		Header: traHeader{
			UniqueID:       nextUniqueID(now),
			GenerationTime: now.Add(-10 * time.Minute).Format(time.RFC3339),
			ExpirationTime: now.Add(10 * time.Minute).Format(time.RFC3339),
		},
		Service: service,
	}
	traXML, err := xml.Marshal(tra)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), traXML...), nil
}

// login signs a fresh TRA with the given digest and calls LoginCms.
func (c *Client) login(ctx context.Context, cert *x509.Certificate, key *rsa.PrivateKey, service string, production bool, digest asn1.ObjectIdentifier) (*TokenAcceso, error) {
	// 1. Generate TRA (Ticket de Requerimiento de Acceso)
	traXML, err := buildTRA(time.Now(), service)
	if err != nil {
		return nil, &afipkit.ErrWSAAAuth{Err: fmt.Errorf("marshal TRA: %w", err)}
	}

	// 2. Sign TRA with CMS/PKCS#7
	cms, err := signCMS(traXML, cert, key, digest)
	if err != nil {
		return nil, &afipkit.ErrWSAAAuth{Err: fmt.Errorf("sign CMS: %w", err)}
	}
	cmsBase64 := base64.StdEncoding.EncodeToString(cms)

	// 3. Call LoginCms via SOAP
	url := homoURL
	if production {
		url = prodURL
	}
	if c.baseURL != "" {
		url = c.baseURL
	}

	req := loginCmsRequest{
		XMLNS: wsaaNamespace,
		In0:   cmsBase64,
	}

	var resp loginCmsResponse
	if err := c.soap.Call(ctx, url, "", &req, &resp); err != nil {
		return nil, &afipkit.ErrWSAAAuth{Err: fmt.Errorf("LoginCms: %w", err)}
	}

	// 4. Parse the TA (Ticket de Acceso) from the response string
	token, err := parseLoginTicketResponseXML(resp.LoginCmsReturn)
	if err != nil {
		return nil, &afipkit.ErrWSAAAuth{Err: fmt.Errorf("parse TA: %w", err)}
	}

	return token, nil
}

// parseLoginTicketResponseXML parses the XML string returned inside loginCmsReturn.
func parseLoginTicketResponseXML(xmlStr string) (*TokenAcceso, error) {
	var ta loginTicketResponse
	if err := xml.Unmarshal([]byte(xmlStr), &ta); err != nil {
		return nil, fmt.Errorf("unmarshal TA: %w", err)
	}

	expiration, err := time.Parse(time.RFC3339, ta.Header.ExpirationTime)
	if err != nil {
		return nil, fmt.Errorf("parse expiration time: %w", err)
	}

	return &TokenAcceso{
		Token:      ta.Credentials.Token,
		Sign:       ta.Credentials.Sign,
		Expiration: expiration,
	}, nil
}
