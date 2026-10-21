package wsaa

import (
	"encoding/xml"
	"time"
)

// TokenAcceso contains the credentials returned by WSAA's LoginCms.
// Use these to authenticate against WSFEv1 and other AFIP services.
type TokenAcceso struct {
	Token      string
	Sign       string
	Expiration time.Time
}

// IsExpired returns true if the token has expired or will expire within
// the given margin. A zero margin checks exact expiration.
func (ta *TokenAcceso) IsExpired(margin time.Duration) bool {
	return time.Now().After(ta.Expiration.Add(-margin))
}

// --- TRA (Ticket de Requerimiento de Acceso) ---

type loginTicketRequest struct {
	XMLName xml.Name  `xml:"loginTicketRequest"`
	Version string    `xml:"version,attr"`
	Header  traHeader `xml:"header"`
	Service string    `xml:"service"`
}

type traHeader struct {
	UniqueID       uint32 `xml:"uniqueId"` // xs:unsignedInt
	GenerationTime string `xml:"generationTime"`
	ExpirationTime string `xml:"expirationTime"`
}

// --- LoginCms SOAP request/response ---

type loginCmsRequest struct {
	XMLName xml.Name `xml:"loginCms"`
	XMLNS   string   `xml:"xmlns,attr"`
	In0     string   `xml:"in0"`
}

type loginCmsResponse struct {
	XMLName        xml.Name `xml:"loginCmsResponse"`
	LoginCmsReturn string   `xml:"loginCmsReturn"`
}

// --- TA (Ticket de Acceso) — parsed from loginCmsReturn ---

type loginTicketResponse struct {
	XMLName     xml.Name      `xml:"loginTicketResponse"`
	Header      taHeader      `xml:"header"`
	Credentials taCredentials `xml:"credentials"`
}

type taHeader struct {
	Source         string `xml:"source"`
	Destination    string `xml:"destination"`
	UniqueID       int64  `xml:"uniqueId"`
	GenerationTime string `xml:"generationTime"`
	ExpirationTime string `xml:"expirationTime"`
}

type taCredentials struct {
	Token string `xml:"token"`
	Sign  string `xml:"sign"`
}
