// Package qr builds the AFIP/ARCA QR code URL required on every electronic
// comprobante since RG 4892/2020 (later updated by RG 5614/24).
//
// The QR encodes a URL of the form
//
//	https://www.afip.gob.ar/fe/qr/?p=<base64(JSON)>
//
// where the JSON payload describes the comprobante. Field names and types must
// match the AFIP spec exactly — in particular codAut (the CAE) is numeric, not
// a string. Render the resulting URL with any QR library (the payload is just
// a URL; AFIP does not mandate a specific QR encoder).
package qr

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

// BaseURL is the AFIP comprobante verification endpoint the QR must point to.
const BaseURL = "https://www.afip.gob.ar/fe/qr/?p="

// Comprobante is the JSON payload defined by RG 4892 (spec v1).
//
// TipoDocRec/NroDocRec are optional per the spec ("de corresponder"): leave
// them zero for comprobantes to unidentified receivers (DocSinIdentificar).
type Comprobante struct {
	Ver        int     `json:"ver"`                  // spec version, always 1
	Fecha      string  `json:"fecha"`                // YYYY-MM-DD
	CUIT       int64   `json:"cuit"`                 // emisor
	PtoVta     int     `json:"ptoVta"`
	TipoCmp    int     `json:"tipoCmp"`
	NroCmp     int64   `json:"nroCmp"`
	Importe    float64 `json:"importe"`              // total, 2 decimals
	Moneda     string  `json:"moneda"`               // "PES", "DOL", ...
	Ctz        float64 `json:"ctz"`                  // cotización (1 for PES)
	TipoDocRec int     `json:"tipoDocRec,omitempty"` // receptor doc type
	NroDocRec  int64   `json:"nroDocRec,omitempty"`  // receptor doc number
	TipoCodAut string  `json:"tipoCodAut"`           // "E" (CAE) or "A" (CAEA)
	CodAut     int64   `json:"codAut"`               // CAE/CAEA as number
}

// URL builds the AFIP QR URL for the given comprobante.
//
// Missing fields with an unambiguous default are filled in: Ver=1,
// TipoCodAut="E", Moneda="PES", and Ctz=1 when the currency is PES.
func URL(c Comprobante) (string, error) {
	if c.Ver == 0 {
		c.Ver = 1
	}
	if c.TipoCodAut == "" {
		c.TipoCodAut = "E"
	}
	if c.Moneda == "" {
		c.Moneda = afipkit.MonedaPesos
	}
	if c.Ctz == 0 && c.Moneda == afipkit.MonedaPesos {
		c.Ctz = 1
	}

	if _, err := time.Parse("2006-01-02", c.Fecha); err != nil {
		return "", fmt.Errorf("qr: fecha %q invalida, esperado YYYY-MM-DD: %w", c.Fecha, err)
	}
	if err := afipkit.ValidarCUIT(c.CUIT); err != nil {
		return "", fmt.Errorf("qr: cuit emisor: %w", err)
	}
	if c.PtoVta <= 0 || c.TipoCmp <= 0 || c.NroCmp <= 0 {
		return "", fmt.Errorf("qr: ptoVta (%d), tipoCmp (%d) y nroCmp (%d) deben ser positivos", c.PtoVta, c.TipoCmp, c.NroCmp)
	}
	if c.CodAut <= 0 {
		return "", fmt.Errorf("qr: codAut (CAE) requerido")
	}
	if c.Ctz <= 0 {
		return "", fmt.Errorf("qr: ctz debe ser positiva para moneda %s", c.Moneda)
	}

	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("qr: marshal payload: %w", err)
	}
	return BaseURL + base64.StdEncoding.EncodeToString(payload), nil
}

// ParseCAE converts a CAE string (14 digits as returned by WSFEv1) to the
// numeric form the QR payload requires.
func ParseCAE(cae string) (int64, error) {
	n, err := strconv.ParseInt(cae, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("qr: CAE %q no es numerico", cae)
	}
	return n, nil
}
