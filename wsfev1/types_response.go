package wsfev1

import "encoding/xml"

// --- FECAESolicitar response ---

// FECAESolicitarResponse is the full response from FECAESolicitar.
type FECAESolicitarResponse struct {
	XMLName              xml.Name             `xml:"FECAESolicitarResponse"`
	FECAESolicitarResult FECAESolicitarResult `xml:"FECAESolicitarResult"`
}

// FECAESolicitarResult contains header, details, and errors.
type FECAESolicitarResult struct {
	FeCabResp FeCabResp          `xml:"FeCabResp"`
	FeDetResp []FECAEDetResponse `xml:"FeDetResp>FECAEDetResponse"`
	Errors    *ErrorsArray       `xml:"Errors,omitempty"`
	Events    *EventsArray       `xml:"Events,omitempty"`
}

// FeCabResp is the response header.
type FeCabResp struct {
	Cuit       int64  `xml:"Cuit"`
	PtoVta     int    `xml:"PtoVta"`
	CbteTipo   int    `xml:"CbteTipo"`
	FchProceso string `xml:"FchProceso"` // YYYYMMDDHHmmss
	CantReg    int    `xml:"CantReg"`
	Resultado  string `xml:"Resultado"` // A=Aprobado, R=Rechazado, P=Parcial
	Reproceso  string `xml:"Reproceso"` // S=Si, N=No
}

// FECAEDetResponse is the detail of a single comprobante in the response.
type FECAEDetResponse struct {
	Concepto      int              `xml:"Concepto"`
	DocTipo       int              `xml:"DocTipo"`
	DocNro        int64            `xml:"DocNro"`
	CbteDesde     int64            `xml:"CbteDesde"`
	CbteHasta     int64            `xml:"CbteHasta"`
	CbteFch       string           `xml:"CbteFch"`
	Resultado     string           `xml:"Resultado"` // A or R
	CAE           string           `xml:"CAE"`
	CAEFchVto     string           `xml:"CAEFchVto"` // YYYYMMDD
	Observaciones *ObservacionesArray `xml:"Observaciones,omitempty"`
}

// --- FECompUltimoAutorizado response ---

// FECompUltimoAutorizadoResponse returns the last authorized comprobante number.
type FECompUltimoAutorizadoResponse struct {
	XMLName                      xml.Name `xml:"FECompUltimoAutorizadoResponse"`
	FECompUltimoAutorizadoResult struct {
		PtoVta   int          `xml:"PtoVta"`
		CbteTipo int          `xml:"CbteTipo"`
		CbteNro  int64        `xml:"CbteNro"`
		Errors   *ErrorsArray `xml:"Errors,omitempty"`
	} `xml:"FECompUltimoAutorizadoResult"`
}

// --- FEDummy response ---

// FEDummyResponse returns the health status of AFIP servers.
type FEDummyResponse struct {
	XMLName       xml.Name `xml:"FEDummyResponse"`
	FEDummyResult struct {
		AppServer  string `xml:"AppServer"`
		DbServer   string `xml:"DbServer"`
		AuthServer string `xml:"AuthServer"`
	} `xml:"FEDummyResult"`
}

// --- FECompConsultar response ---

// FECompConsultarResponse returns details of a queried comprobante.
type FECompConsultarResponse struct {
	XMLName              xml.Name `xml:"FECompConsultarResponse"`
	FECompConsultarResult struct {
		ResultGet *ResultGet   `xml:"ResultGet,omitempty"`
		Errors    *ErrorsArray `xml:"Errors,omitempty"`
	} `xml:"FECompConsultarResult"`
}

// ResultGet is the detail of a queried comprobante.
type ResultGet struct {
	Concepto     int    `xml:"Concepto"`
	DocTipo      int    `xml:"DocTipo"`
	DocNro       int64  `xml:"DocNro"`
	CbteDesde    int64  `xml:"CbteDesde"`
	CbteHasta    int64  `xml:"CbteHasta"`
	CbteFch      string `xml:"CbteFch"`
	ImpTotal     string `xml:"ImpTotal"`
	ImpTotConc   string `xml:"ImpTotConc"`
	ImpNeto      string `xml:"ImpNeto"`
	ImpOpEx      string `xml:"ImpOpEx"`
	ImpIVA       string `xml:"ImpIVA"`
	ImpTrib      string `xml:"ImpTrib"`
	FchServDesde string `xml:"FchServDesde"`
	FchServHasta string `xml:"FchServHasta"`
	FchVtoPago   string `xml:"FchVtoPago"`
	MonId        string `xml:"MonId"`
	MonCotiz     string `xml:"MonCotiz"`
	Resultado    string `xml:"Resultado"`
	CodAutorizacion string `xml:"CodAutorizacion"` // CAE
	FchVto       string `xml:"FchVto"`             // CAE expiration
	EmisionTipo  string `xml:"EmisionTipo"`
	FchProceso   string `xml:"FchProceso"`
	PtoVta       int    `xml:"PtoVta"`
	CbteTipo     int    `xml:"CbteTipo"`

	Observaciones *ObservacionesArray `xml:"Observaciones,omitempty"`
	Iva           *IvaRespArray       `xml:"Iva,omitempty"`
}

// IvaRespArray wraps IVA entries in query responses.
type IvaRespArray struct {
	AlicIva []AlicIvaResp `xml:"AlicIva"`
}

// AlicIvaResp is an IVA entry in a query response.
type AlicIvaResp struct {
	Id      int    `xml:"Id"`
	BaseImp string `xml:"BaseImp"`
	Importe string `xml:"Importe"`
}

// --- FEParamGet* responses ---

// FEParamGetTiposCbteResponse lists available comprobante types.
type FEParamGetTiposCbteResponse struct {
	XMLName                  xml.Name `xml:"FEParamGetTiposCbteResponse"`
	FEParamGetTiposCbteResult struct {
		ResultGet []ParamTipoCbte `xml:"ResultGet>CbteTipo"`
		Errors    *ErrorsArray    `xml:"Errors,omitempty"`
	} `xml:"FEParamGetTiposCbteResult"`
}

// ParamTipoCbte is a comprobante type entry.
type ParamTipoCbte struct {
	Id       int    `xml:"Id"`
	Desc     string `xml:"Desc"`
	FchDesde string `xml:"FchDesde"`
	FchHasta string `xml:"FchHasta"`
}

// FEParamGetTiposIvaResponse lists available IVA rates.
type FEParamGetTiposIvaResponse struct {
	XMLName                  xml.Name `xml:"FEParamGetTiposIvaResponse"`
	FEParamGetTiposIvaResult struct {
		ResultGet []ParamTipoIva `xml:"ResultGet>IvaTipo"`
		Errors    *ErrorsArray   `xml:"Errors,omitempty"`
	} `xml:"FEParamGetTiposIvaResult"`
}

// ParamTipoIva is an IVA rate entry.
type ParamTipoIva struct {
	Id       int    `xml:"Id"`
	Desc     string `xml:"Desc"`
	FchDesde string `xml:"FchDesde"`
	FchHasta string `xml:"FchHasta"`
}

// FEParamGetPtosVentaResponse lists the tenant's enabled puntos de venta.
type FEParamGetPtosVentaResponse struct {
	XMLName                   xml.Name `xml:"FEParamGetPtosVentaResponse"`
	FEParamGetPtosVentaResult struct {
		ResultGet []ParamPtoVenta `xml:"ResultGet>PtoVenta"`
		Errors    *ErrorsArray    `xml:"Errors,omitempty"`
	} `xml:"FEParamGetPtosVentaResult"`
}

// ParamPtoVenta is a punto de venta entry.
type ParamPtoVenta struct {
	Nro         int    `xml:"Nro"`
	EmisionTipo string `xml:"EmisionTipo"`
	Bloqueado   string `xml:"Bloqueado"`
	FchBaja     string `xml:"FchBaja"`
}

// FEParamGetTiposDocResponse lists available document types.
type FEParamGetTiposDocResponse struct {
	XMLName                  xml.Name `xml:"FEParamGetTiposDocResponse"`
	FEParamGetTiposDocResult struct {
		ResultGet []ParamTipoDoc `xml:"ResultGet>DocTipo"`
		Errors    *ErrorsArray   `xml:"Errors,omitempty"`
	} `xml:"FEParamGetTiposDocResult"`
}

// ParamTipoDoc is a document type entry.
type ParamTipoDoc struct {
	Id       int    `xml:"Id"`
	Desc     string `xml:"Desc"`
	FchDesde string `xml:"FchDesde"`
	FchHasta string `xml:"FchHasta"`
}

// FEParamGetTiposMonedasResponse lists available currencies.
type FEParamGetTiposMonedasResponse struct {
	XMLName                      xml.Name `xml:"FEParamGetTiposMonedasResponse"`
	FEParamGetTiposMonedasResult struct {
		ResultGet []ParamMoneda `xml:"ResultGet>Moneda"`
		Errors    *ErrorsArray  `xml:"Errors,omitempty"`
	} `xml:"FEParamGetTiposMonedasResult"`
}

// ParamMoneda is a currency entry.
type ParamMoneda struct {
	Id       string `xml:"Id"`
	Desc     string `xml:"Desc"`
	FchDesde string `xml:"FchDesde"`
	FchHasta string `xml:"FchHasta"`
}

// FEParamGetCotizacionResponse returns the exchange rate for a currency.
type FEParamGetCotizacionResponse struct {
	XMLName                    xml.Name `xml:"FEParamGetCotizacionResponse"`
	FEParamGetCotizacionResult struct {
		ResultGet *ParamCotizacion `xml:"ResultGet,omitempty"`
		Errors    *ErrorsArray     `xml:"Errors,omitempty"`
	} `xml:"FEParamGetCotizacionResult"`
}

// ParamCotizacion is an exchange rate value.
type ParamCotizacion struct {
	MonId      string `xml:"MonId"`
	MonCotiz   string `xml:"MonCotiz"`
	FchDesde   string `xml:"FchDesde"`
}

// FEParamGetCondicionIvaReceptorResponse lists the receptor IVA conditions
// (RG 5616) and the comprobante classes each one admits.
type FEParamGetCondicionIvaReceptorResponse struct {
	XMLName                              xml.Name `xml:"FEParamGetCondicionIvaReceptorResponse"`
	FEParamGetCondicionIvaReceptorResult struct {
		ResultGet []ParamCondicionIvaReceptor `xml:"ResultGet>CondicionIvaReceptor"`
		Errors    *ErrorsArray                `xml:"Errors,omitempty"`
	} `xml:"FEParamGetCondicionIvaReceptorResult"`
}

// ParamCondicionIvaReceptor is a receptor IVA condition entry.
type ParamCondicionIvaReceptor struct {
	Id       int    `xml:"Id"`
	Desc     string `xml:"Desc"`
	CmpClase string `xml:"Cmp_Clase"` // e.g. "A/M/C", "B/C"
}

// FECompTotXRequestResponse returns max comprobantes per request.
type FECompTotXRequestResponse struct {
	XMLName                   xml.Name `xml:"FECompTotXRequestResponse"`
	FECompTotXRequestResult struct {
		RegXReq int          `xml:"RegXReq"`
		Errors  *ErrorsArray `xml:"Errors,omitempty"`
	} `xml:"FECompTotXRequestResult"`
}

// --- Shared error/observation types ---

// ErrorsArray wraps AFIP error entries.
type ErrorsArray struct {
	Err []ErrItem `xml:"Err"`
}

// ErrItem is a single AFIP error.
type ErrItem struct {
	Code int    `xml:"Code"`
	Msg  string `xml:"Msg"`
}

// ObservacionesArray wraps AFIP observation entries.
type ObservacionesArray struct {
	Obs []ObsItem `xml:"Obs"`
}

// ObsItem is a single AFIP observation.
type ObsItem struct {
	Code int    `xml:"Code"`
	Msg  string `xml:"Msg"`
}

// EventsArray wraps AFIP event entries.
type EventsArray struct {
	Evt []EvtItem `xml:"Evt"`
}

// EvtItem is a single AFIP event.
type EvtItem struct {
	Code int    `xml:"Code"`
	Msg  string `xml:"Msg"`
}
