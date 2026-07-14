package wsfev1

import "encoding/xml"

// --- Auth (shared by all WSFEv1 methods) ---

// Auth holds the WSAA credentials for authenticating WSFEv1 calls.
type Auth struct {
	Token string `xml:"Token"`
	Sign  string `xml:"Sign"`
	Cuit  int64  `xml:"Cuit"`
}

// --- FECAESolicitar ---

// FECAESolicitarRequest is the request for emitting one or more comprobantes.
type FECAESolicitarRequest struct {
	XMLName  xml.Name `xml:"FECAESolicitar"`
	XMLNS    string   `xml:"xmlns,attr"`
	Auth     Auth     `xml:"Auth"`
	FeCAEReq FeCAEReq `xml:"FeCAEReq"`
}

// FeCAEReq groups the header and detail of the CAE request.
type FeCAEReq struct {
	FeCabReq FeCabReq          `xml:"FeCabReq"`
	FeDetReq []FECAEDetRequest `xml:"FeDetReq>FECAEDetRequest"`
}

// FeCabReq is the header of the CAE request.
type FeCabReq struct {
	CantReg  int `xml:"CantReg"`
	PtoVta   int `xml:"PtoVta"`
	CbteTipo int `xml:"CbteTipo"`
}

// FECAEDetRequest is the detail of a single comprobante in a CAE request.
type FECAEDetRequest struct {
	Concepto     int    `xml:"Concepto"`
	DocTipo      int    `xml:"DocTipo"`
	DocNro       int64  `xml:"DocNro"`
	CbteDesde    int64  `xml:"CbteDesde"`
	CbteHasta    int64  `xml:"CbteHasta"`
	CbteFch      string `xml:"CbteFch"`      // YYYYMMDD
	ImpTotal     string `xml:"ImpTotal"`     // decimal as string for precision
	ImpTotConc   string `xml:"ImpTotConc"`
	ImpNeto      string `xml:"ImpNeto"`
	ImpOpEx      string `xml:"ImpOpEx"`
	ImpIVA       string `xml:"ImpIVA"`
	ImpTrib      string `xml:"ImpTrib"`
	FchServDesde string `xml:"FchServDesde,omitempty"` // YYYYMMDD (concepto 2,3)
	FchServHasta string `xml:"FchServHasta,omitempty"`
	FchVtoPago   string `xml:"FchVtoPago,omitempty"`
	MonId        string `xml:"MonId"`
	MonCotiz     string `xml:"MonCotiz"`
	// CondicionIVAReceptorId es la condición frente al IVA del receptor (RG 5616).
	// Es el código OFICIAL de AFIP (FEParamGetCondicionIvaReceptor: 1=RI, 4=Exento,
	// 5=Consumidor Final, 6=Monotributo, ...), NO el enum interno fiscal.CondicionIVA.
	// omitempty por retrocompatibilidad: los callers que no lo setean no lo envían.
	CondicionIVAReceptorId int `xml:"CondicionIVAReceptorId,omitempty"`

	Iva        *IvaArray        `xml:"Iva,omitempty"`
	Tributos   *TributosArray   `xml:"Tributos,omitempty"`
	CbtesAsoc  *CbtesAsocArray  `xml:"CbtesAsoc,omitempty"`
	Opcionales *OpcionalesArray `xml:"Opcionales,omitempty"`
}

// IvaArray wraps the list of IVA rates.
type IvaArray struct {
	AlicIva []AlicIvaItem `xml:"AlicIva"`
}

// AlicIvaItem is a single IVA rate entry.
type AlicIvaItem struct {
	Id      int    `xml:"Id"`
	BaseImp string `xml:"BaseImp"`
	Importe string `xml:"Importe"`
}

// TributosArray wraps the list of tributos.
type TributosArray struct {
	Tributo []TributoItem `xml:"Tributo"`
}

// TributoItem is a single tributo entry (IIBB, municipal, etc.).
type TributoItem struct {
	Id      int    `xml:"Id"`
	Desc    string `xml:"Desc"`
	BaseImp string `xml:"BaseImp"`
	Alic    string `xml:"Alic"`
	Importe string `xml:"Importe"`
}

// CbtesAsocArray wraps associated comprobantes (for NC/ND).
type CbtesAsocArray struct {
	CbteAsoc []CbteAsocItem `xml:"CbteAsoc"`
}

// CbteAsocItem references an associated comprobante.
type CbteAsocItem struct {
	Tipo    int    `xml:"Tipo"`
	PtoVta  int    `xml:"PtoVta"`
	Nro     int64  `xml:"Nro"`
	Cuit    int64  `xml:"Cuit"`
	CbteFch string `xml:"CbteFch"` // YYYYMMDD
}

// OpcionalesArray wraps optional data fields (FCE CBU, etc.).
type OpcionalesArray struct {
	Opcional []OpcionalItem `xml:"Opcional"`
}

// OpcionalItem is an optional key-value pair.
type OpcionalItem struct {
	Id    string `xml:"Id"`
	Valor string `xml:"Valor"`
}

// --- FECompUltimoAutorizado ---

// FECompUltimoAutorizadoRequest asks for the last authorized comprobante number.
type FECompUltimoAutorizadoRequest struct {
	XMLName  xml.Name `xml:"FECompUltimoAutorizado"`
	XMLNS    string   `xml:"xmlns,attr"`
	Auth     Auth     `xml:"Auth"`
	PtoVta   int      `xml:"PtoVta"`
	CbteTipo int      `xml:"CbteTipo"`
}

// --- FECompConsultar ---

// FECompConsultarRequest asks for details of an emitted comprobante.
type FECompConsultarRequest struct {
	XMLName       xml.Name       `xml:"FECompConsultar"`
	XMLNS         string         `xml:"xmlns,attr"`
	Auth          Auth           `xml:"Auth"`
	FeCompConsReq FeCompConsReq  `xml:"FeCompConsReq"`
}

// FeCompConsReq identifies the comprobante to query.
type FeCompConsReq struct {
	CbteTipo int   `xml:"CbteTipo"`
	CbteNro  int64 `xml:"CbteNro"`
	PtoVta   int   `xml:"PtoVta"`
}

// --- FEDummy ---

// FEDummyRequest checks AFIP server health.
type FEDummyRequest struct {
	XMLName xml.Name `xml:"FEDummy"`
	XMLNS   string   `xml:"xmlns,attr"`
}

// --- FEParamGet* ---

// FEParamGetRequest is a generic request for parameter queries that only need Auth.
type FEParamGetRequest struct {
	XMLName xml.Name
	XMLNS   string `xml:"xmlns,attr"`
	Auth    Auth   `xml:"Auth"`
}

// --- FEParamGetCotizacion ---

// FEParamGetCotizacionRequest asks for the exchange rate of a currency.
type FEParamGetCotizacionRequest struct {
	XMLName xml.Name `xml:"FEParamGetCotizacion"`
	XMLNS   string   `xml:"xmlns,attr"`
	Auth    Auth     `xml:"Auth"`
	MonId   string   `xml:"MonId"`
}
