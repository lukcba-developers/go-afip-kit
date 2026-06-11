// Package wsfev1 implements the AFIP WSFEv1 (Web Service de Facturacion Electronica v1)
// client for emitting electronic invoices (facturas A, B, C) and obtaining CAE codes.
package wsfev1

import (
	"context"
	"encoding/xml"
	"fmt"

	afipkit "github.com/lukcba-developers/go-afip-kit"
	"github.com/lukcba-developers/go-afip-kit/soap"
	"github.com/lukcba-developers/go-afip-kit/wsaa"
)

const (
	homoURL = "https://wswhomo.afip.gov.ar/wsfev1/service.asmx"
	prodURL = "https://servicios1.afip.gov.ar/wsfev1/service.asmx"

	wsfeNS = "http://ar.gov.afip.dif.FEV1/"

	actionCAESolicitar    = wsfeNS + "FECAESolicitar"
	actionUltimoAut       = wsfeNS + "FECompUltimoAutorizado"
	actionCompConsultar   = wsfeNS + "FECompConsultar"
	actionDummy           = wsfeNS + "FEDummy"
	actionTiposCbte       = wsfeNS + "FEParamGetTiposCbte"
	actionTiposIva        = wsfeNS + "FEParamGetTiposIva"
	actionPtosVenta       = wsfeNS + "FEParamGetPtosVenta"
	actionTiposDoc        = wsfeNS + "FEParamGetTiposDoc"
	actionTiposMonedas    = wsfeNS + "FEParamGetTiposMonedas"
	actionCotizacion      = wsfeNS + "FEParamGetCotizacion"
	actionCompTotXRequest = wsfeNS + "FECompTotXRequest"
)

// HealthStatus represents the state of AFIP's servers.
type HealthStatus struct {
	AppServer  string
	DbServer   string
	AuthServer string
}

// IsOK returns true if all three servers report "OK".
func (h *HealthStatus) IsOK() bool {
	return h.AppServer == "OK" && h.DbServer == "OK" && h.AuthServer == "OK"
}

// CAEResult is the parsed result of a FECAESolicitar call.
type CAEResult struct {
	CAE           string
	CAEFchVto     string // YYYYMMDD
	CbteDesde     int64
	CbteHasta     int64
	Resultado     string // A=Aprobado, R=Rechazado
	Reproceso     bool
	Observaciones []afipkit.AFIPErrorDetail
}

// Client communicates with WSFEv1 for electronic invoicing.
type Client struct {
	soap       *soap.Client
	production bool
}

// NewClient creates a WSFEv1 client. Set production=true for live AFIP.
func NewClient(production bool, opts ...soap.Option) *Client {
	return &Client{
		soap:       soap.NewClient(opts...),
		production: production,
	}
}

func (c *Client) url() string {
	if c.production {
		return prodURL
	}
	return homoURL
}

func (c *Client) auth(token *wsaa.TokenAcceso, cuit int64) Auth {
	return Auth{
		Token: token.Token,
		Sign:  token.Sign,
		Cuit:  cuit,
	}
}

// errorsToAFIP converts XML error items to the shared error type.
func errorsToAFIP(ea *ErrorsArray) *afipkit.AFIPError {
	if ea == nil || len(ea.Err) == 0 {
		return nil
	}
	details := make([]afipkit.AFIPErrorDetail, len(ea.Err))
	for i, e := range ea.Err {
		details[i] = afipkit.AFIPErrorDetail{Code: e.Code, Msg: e.Msg}
	}
	return afipkit.NewAFIPError(details)
}

func obsToDetails(oa *ObservacionesArray) []afipkit.AFIPErrorDetail {
	if oa == nil {
		return nil
	}
	details := make([]afipkit.AFIPErrorDetail, len(oa.Obs))
	for i, o := range oa.Obs {
		details[i] = afipkit.AFIPErrorDetail{Code: o.Code, Msg: o.Msg}
	}
	return details
}

// HealthCheck verifies AFIP server status (FEDummy). Does not require authentication.
func (c *Client) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	req := FEDummyRequest{XMLNS: wsfeNS}
	var resp FEDummyResponse

	if err := c.soap.Call(ctx, c.url(), actionDummy, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}

	return &HealthStatus{
		AppServer:  resp.FEDummyResult.AppServer,
		DbServer:   resp.FEDummyResult.DbServer,
		AuthServer: resp.FEDummyResult.AuthServer,
	}, nil
}

// UltimoAutorizado returns the last authorized comprobante number
// for the given punto de venta and comprobante type.
func (c *Client) UltimoAutorizado(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, ptoVta, tipoCbte int) (int64, error) {
	req := FECompUltimoAutorizadoRequest{
		XMLNS:    wsfeNS,
		Auth:     c.auth(token, cuit),
		PtoVta:   ptoVta,
		CbteTipo: tipoCbte,
	}
	var resp FECompUltimoAutorizadoResponse

	if err := c.soap.Call(ctx, c.url(), actionUltimoAut, &req, &resp); err != nil {
		return 0, &afipkit.ErrSOAP{Err: err}
	}

	result := resp.FECompUltimoAutorizadoResult
	if afipErr := errorsToAFIP(result.Errors); afipErr != nil {
		return 0, afipErr
	}

	return result.CbteNro, nil
}

// SolicitarCAE emits a comprobante and requests a CAE from AFIP.
// The request is validated locally first (see Validate) so malformed
// solicitudes fail fast without consuming an AFIP round-trip.
func (c *Client) SolicitarCAE(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, solicitud *FECAESolicitarRequest) (*CAEResult, error) {
	if err := solicitud.Validate(); err != nil {
		return nil, err
	}

	// Fill auth
	solicitud.XMLNS = wsfeNS
	solicitud.Auth = c.auth(token, cuit)

	var resp FECAESolicitarResponse
	if err := c.soap.Call(ctx, c.url(), actionCAESolicitar, solicitud, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}

	result := resp.FECAESolicitarResult

	// Check header-level errors (total rejection)
	if afipErr := errorsToAFIP(result.Errors); afipErr != nil {
		return nil, afipErr
	}

	// Process first detail response
	if len(result.FeDetResp) == 0 {
		return nil, fmt.Errorf("wsfev1: empty FeDetResp in response")
	}

	det := result.FeDetResp[0]
	caeResult := &CAEResult{
		CAE:       det.CAE,
		CAEFchVto: det.CAEFchVto,
		CbteDesde: det.CbteDesde,
		CbteHasta: det.CbteHasta,
		Resultado: det.Resultado,
		Reproceso: result.FeCabResp.Reproceso == "S",
		Observaciones: obsToDetails(det.Observaciones),
	}

	// If rejected at detail level, return as AFIPError with observations
	if det.Resultado == "R" {
		obs := obsToDetails(det.Observaciones)
		if len(obs) > 0 {
			return caeResult, afipkit.NewAFIPError(obs)
		}
		return caeResult, fmt.Errorf("wsfev1: comprobante rejected without observations")
	}

	return caeResult, nil
}

// ConsultarComprobante retrieves details of a previously emitted comprobante.
func (c *Client) ConsultarComprobante(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, tipoCbte int, ptoVta int, cbteNro int64) (*ResultGet, error) {
	req := FECompConsultarRequest{
		XMLNS: wsfeNS,
		Auth:  c.auth(token, cuit),
		FeCompConsReq: FeCompConsReq{
			CbteTipo: tipoCbte,
			CbteNro:  cbteNro,
			PtoVta:   ptoVta,
		},
	}
	var resp FECompConsultarResponse

	if err := c.soap.Call(ctx, c.url(), actionCompConsultar, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}

	result := resp.FECompConsultarResult
	if afipErr := errorsToAFIP(result.Errors); afipErr != nil {
		return nil, afipErr
	}

	if result.ResultGet == nil {
		return nil, fmt.Errorf("wsfev1: comprobante not found")
	}

	return result.ResultGet, nil
}

// SolicitarCAEConReproceso emits a comprobante and handles error 10016 (already registered)
// by recovering the CAE via FECompConsultar — the idempotent retry pattern from pyafipws.
func (c *Client) SolicitarCAEConReproceso(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, solicitud *FECAESolicitarRequest) (*CAEResult, error) {
	result, err := c.SolicitarCAE(ctx, token, cuit, solicitud)
	if err != nil {
		afipErr, ok := afipkit.IsAFIPError(err)
		if ok && afipErr.IsAlreadyRegistered() {
			// Comprobante already authorized — recover CAE
			det := solicitud.FeCAEReq.FeDetReq[0]
			queried, qErr := c.ConsultarComprobante(ctx, token, cuit,
				solicitud.FeCAEReq.FeCabReq.CbteTipo,
				solicitud.FeCAEReq.FeCabReq.PtoVta,
				det.CbteDesde,
			)
			if qErr != nil {
				return nil, fmt.Errorf("wsfev1: reproceso 10016: consultar: %w", qErr)
			}
			return &CAEResult{
				CAE:       queried.CodAutorizacion,
				CAEFchVto: queried.FchVto,
				CbteDesde: queried.CbteDesde,
				CbteHasta: queried.CbteHasta,
				Resultado: queried.Resultado,
				Reproceso: true,
			}, nil
		}
		return result, err
	}
	return result, nil
}

// --- Parameter queries ---

// TiposComprobante returns available comprobante types.
func (c *Client) TiposComprobante(ctx context.Context, token *wsaa.TokenAcceso, cuit int64) ([]ParamTipoCbte, error) {
	req := FEParamGetRequest{
		XMLName: xml.Name{Local: "FEParamGetTiposCbte"},
		XMLNS:   wsfeNS,
		Auth:    c.auth(token, cuit),
	}
	var resp FEParamGetTiposCbteResponse

	if err := c.soap.Call(ctx, c.url(), actionTiposCbte, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}
	if afipErr := errorsToAFIP(resp.FEParamGetTiposCbteResult.Errors); afipErr != nil {
		return nil, afipErr
	}
	return resp.FEParamGetTiposCbteResult.ResultGet, nil
}

// AlicuotasIVA returns available IVA rates.
func (c *Client) AlicuotasIVA(ctx context.Context, token *wsaa.TokenAcceso, cuit int64) ([]ParamTipoIva, error) {
	req := FEParamGetRequest{
		XMLName: xml.Name{Local: "FEParamGetTiposIva"},
		XMLNS:   wsfeNS,
		Auth:    c.auth(token, cuit),
	}
	var resp FEParamGetTiposIvaResponse

	if err := c.soap.Call(ctx, c.url(), actionTiposIva, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}
	if afipErr := errorsToAFIP(resp.FEParamGetTiposIvaResult.Errors); afipErr != nil {
		return nil, afipErr
	}
	return resp.FEParamGetTiposIvaResult.ResultGet, nil
}

// PuntosVenta returns the tenant's enabled puntos de venta.
func (c *Client) PuntosVenta(ctx context.Context, token *wsaa.TokenAcceso, cuit int64) ([]ParamPtoVenta, error) {
	req := FEParamGetRequest{
		XMLName: xml.Name{Local: "FEParamGetPtosVenta"},
		XMLNS:   wsfeNS,
		Auth:    c.auth(token, cuit),
	}
	var resp FEParamGetPtosVentaResponse

	if err := c.soap.Call(ctx, c.url(), actionPtosVenta, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}
	if afipErr := errorsToAFIP(resp.FEParamGetPtosVentaResult.Errors); afipErr != nil {
		return nil, afipErr
	}
	return resp.FEParamGetPtosVentaResult.ResultGet, nil
}

// TiposDocumento returns available document types.
func (c *Client) TiposDocumento(ctx context.Context, token *wsaa.TokenAcceso, cuit int64) ([]ParamTipoDoc, error) {
	req := FEParamGetRequest{
		XMLName: xml.Name{Local: "FEParamGetTiposDoc"},
		XMLNS:   wsfeNS,
		Auth:    c.auth(token, cuit),
	}
	var resp FEParamGetTiposDocResponse

	if err := c.soap.Call(ctx, c.url(), actionTiposDoc, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}
	if afipErr := errorsToAFIP(resp.FEParamGetTiposDocResult.Errors); afipErr != nil {
		return nil, afipErr
	}
	return resp.FEParamGetTiposDocResult.ResultGet, nil
}

// TiposMonedas returns available currencies.
func (c *Client) TiposMonedas(ctx context.Context, token *wsaa.TokenAcceso, cuit int64) ([]ParamMoneda, error) {
	req := FEParamGetRequest{
		XMLName: xml.Name{Local: "FEParamGetTiposMonedas"},
		XMLNS:   wsfeNS,
		Auth:    c.auth(token, cuit),
	}
	var resp FEParamGetTiposMonedasResponse

	if err := c.soap.Call(ctx, c.url(), actionTiposMonedas, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}
	if afipErr := errorsToAFIP(resp.FEParamGetTiposMonedasResult.Errors); afipErr != nil {
		return nil, afipErr
	}
	return resp.FEParamGetTiposMonedasResult.ResultGet, nil
}

// Cotizacion returns the exchange rate for the given currency.
func (c *Client) Cotizacion(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, monedaID string) (*ParamCotizacion, error) {
	req := FEParamGetCotizacionRequest{
		XMLNS: wsfeNS,
		Auth:  c.auth(token, cuit),
		MonId: monedaID,
	}
	var resp FEParamGetCotizacionResponse

	if err := c.soap.Call(ctx, c.url(), actionCotizacion, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}
	result := resp.FEParamGetCotizacionResult
	if afipErr := errorsToAFIP(result.Errors); afipErr != nil {
		return nil, afipErr
	}
	if result.ResultGet == nil {
		return nil, fmt.Errorf("wsfev1: cotizacion not found for %s", monedaID)
	}
	return result.ResultGet, nil
}

// MaxComprobantesPerRequest returns the maximum number of comprobantes
// allowed in a single FECAESolicitar call.
func (c *Client) MaxComprobantesPerRequest(ctx context.Context, token *wsaa.TokenAcceso, cuit int64) (int, error) {
	req := FEParamGetRequest{
		XMLName: xml.Name{Local: "FECompTotXRequest"},
		XMLNS:   wsfeNS,
		Auth:    c.auth(token, cuit),
	}
	var resp FECompTotXRequestResponse

	if err := c.soap.Call(ctx, c.url(), wsfeNS+"FECompTotXRequest", &req, &resp); err != nil {
		return 0, &afipkit.ErrSOAP{Err: err}
	}
	if afipErr := errorsToAFIP(resp.FECompTotXRequestResult.Errors); afipErr != nil {
		return 0, afipErr
	}
	return resp.FECompTotXRequestResult.RegXReq, nil
}
