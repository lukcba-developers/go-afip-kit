// Package wsfev1 implements the AFIP WSFEv1 (Web Service de Facturacion Electronica v1)
// client for emitting electronic invoices (facturas A, B, C) and obtaining CAE codes.
package wsfev1

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

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
	actionCondIvaReceptor = wsfeNS + "FEParamGetCondicionIvaReceptor"
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
	baseURL    string // overrides the AFIP URL; only set by tests
}

// NewClient creates a WSFEv1 client. Set production=true for live AFIP.
func NewClient(production bool, opts ...soap.Option) *Client {
	return &Client{
		soap:       soap.NewClient(opts...),
		production: production,
	}
}

func (c *Client) url() string {
	if c.baseURL != "" {
		return c.baseURL
	}
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
		return nil, fmt.Errorf("wsfev1: %w: FeDetResp vacio", afipkit.ErrIncompleteResponse)
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

	if det.Resultado == "A" && det.CAE == "" {
		return caeResult, fmt.Errorf("wsfev1: %w: Resultado A sin CAE", afipkit.ErrIncompleteResponse)
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
		return nil, fmt.Errorf("wsfev1: %w: FECompConsultar sin ResultGet", afipkit.ErrIncompleteResponse)
	}

	return result.ResultGet, nil
}

// ErrComprobanteNoCoincide is matched (errors.Is) by *ComprobanteNoCoincideError.
var ErrComprobanteNoCoincide = errors.New("wsfev1: el comprobante registrado en AFIP no coincide con el esperado")

// ComprobanteNoCoincideError reports that the comprobante number exists in AFIP
// but with different data than expected: the number was used by another
// emission (e.g. from outside the system). Its CAE must NOT be reused.
type ComprobanteNoCoincideError struct {
	PtoVta      int
	CbteTipo    int
	CbteNro     int64
	Diferencias []string   // "Campo: registrado=X esperado=Y"
	Registrado  *ResultGet // what AFIP has for that number
}

func (e *ComprobanteNoCoincideError) Error() string {
	return fmt.Sprintf("%v (pto vta %d, tipo %d, nro %d): %s",
		ErrComprobanteNoCoincide, e.PtoVta, e.CbteTipo, e.CbteNro, strings.Join(e.Diferencias, "; "))
}

// Is makes errors.Is(err, ErrComprobanteNoCoincide) work.
func (e *ComprobanteNoCoincideError) Is(target error) bool { return target == ErrComprobanteNoCoincide }

// Recuperar checks with FECompConsultar whether comprobante cbteNro was
// authorized, typically after an indeterminate SolicitarCAE (see
// afipkit.IsIndeterminate). It compares what AFIP registered against esperado,
// the detail that was sent:
//
//   - registered and matching → (CAEResult with CAE and vencimiento, true, nil)
//   - not registered (AFIP error 602) → (nil, false, nil): safe to emit again
//     with the same number
//   - registered with different data → (nil, false, *ComprobanteNoCoincideError)
//   - any other failure → (nil, false, err); afipkit.IsIndeterminate(err) tells
//     whether it is worth retrying later
//
// The fields compared are DocTipo, DocNro, ImpTotal (to the centavo) and
// CbteFch (only when esperado.CbteFch is set; otherwise AFIP assigned it).
//
// AFIP may still be processing a request that timed out on our side; wait a
// bit before calling Recuperar. Re-emitting a number that did get authorized is
// harmless anyway: AFIP answers 10016 and SolicitarCAEConReproceso recovers it.
func (c *Client) Recuperar(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, ptoVta, tipoCbte int, cbteNro int64, esperado *FECAEDetRequest) (*CAEResult, bool, error) {
	if esperado == nil {
		return nil, false, fmt.Errorf("wsfev1: Recuperar: esperado es nil")
	}
	if cbteNro <= 0 {
		return nil, false, fmt.Errorf("wsfev1: Recuperar: numero de comprobante invalido: %d", cbteNro)
	}

	reg, err := c.ConsultarComprobante(ctx, token, cuit, tipoCbte, ptoVta, cbteNro)
	if err != nil {
		if afipErr, ok := afipkit.IsAFIPError(err); ok && afipErr.IsNotFound() {
			return nil, false, nil
		}
		return nil, false, err
	}

	if difs := diferenciasComprobante(reg, ptoVta, tipoCbte, esperado); len(difs) > 0 {
		return nil, false, &ComprobanteNoCoincideError{
			PtoVta:      ptoVta,
			CbteTipo:    tipoCbte,
			CbteNro:     cbteNro,
			Diferencias: difs,
			Registrado:  reg,
		}
	}

	return &CAEResult{
		CAE:           reg.CodAutorizacion,
		CAEFchVto:     reg.FchVto,
		CbteDesde:     reg.CbteDesde,
		CbteHasta:     reg.CbteHasta,
		Resultado:     reg.Resultado,
		Reproceso:     true,
		Observaciones: obsToDetails(reg.Observaciones),
	}, true, nil
}

// diferenciasComprobante lists the identifying fields where the registered
// comprobante differs from the expected one.
func diferenciasComprobante(reg *ResultGet, ptoVta, tipoCbte int, esp *FECAEDetRequest) []string {
	var difs []string
	add := func(campo string, registrado, esperado any) {
		difs = append(difs, fmt.Sprintf("%s: registrado=%v esperado=%v", campo, registrado, esperado))
	}

	// AFIP echoes PtoVta/CbteTipo; a mismatch means it answered for another comprobante.
	if reg.PtoVta != 0 && reg.PtoVta != ptoVta {
		add("PtoVta", reg.PtoVta, ptoVta)
	}
	if reg.CbteTipo != 0 && reg.CbteTipo != tipoCbte {
		add("CbteTipo", reg.CbteTipo, tipoCbte)
	}
	if reg.DocTipo != esp.DocTipo {
		add("DocTipo", reg.DocTipo, esp.DocTipo)
	}
	if reg.DocNro != esp.DocNro {
		add("DocNro", reg.DocNro, esp.DocNro)
	}
	regTotal, okReg := centavos(reg.ImpTotal)
	espTotal, okEsp := centavos(esp.ImpTotal)
	if !okReg || !okEsp || regTotal != espTotal {
		add("ImpTotal", reg.ImpTotal, esp.ImpTotal)
	}
	if esp.CbteFch != "" && reg.CbteFch != esp.CbteFch {
		add("CbteFch", reg.CbteFch, esp.CbteFch)
	}
	return difs
}

// centavos parses an AFIP decimal string into integer centavos.
func centavos(s string) (int64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return int64(math.Round(f * 100)), true
}

// SolicitarCAEConReproceso emits a comprobante and, when AFIP answers 10016
// (number is not the next one to authorize), checks with Recuperar whether that
// exact comprobante was already authorized — e.g. a previous attempt reached
// AFIP but the response was lost:
//
//   - registered and matching → its CAE is returned (Reproceso=true)
//   - not registered → the original 10016 *AFIPError is returned
//   - registered with different data → an error wrapping
//     *ComprobanteNoCoincideError (the number belongs to another comprobante)
//
// Only the first FeDetReq is checked, so use it with CantReg=1.
func (c *Client) SolicitarCAEConReproceso(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, solicitud *FECAESolicitarRequest) (*CAEResult, error) {
	result, err := c.SolicitarCAE(ctx, token, cuit, solicitud)
	if err != nil {
		afipErr, ok := afipkit.IsAFIPError(err)
		if ok && afipErr.IsAlreadyRegistered() {
			det := solicitud.FeCAEReq.FeDetReq[0]
			recovered, found, rErr := c.Recuperar(ctx, token, cuit,
				solicitud.FeCAEReq.FeCabReq.PtoVta,
				solicitud.FeCAEReq.FeCabReq.CbteTipo,
				det.CbteDesde,
				&det,
			)
			if rErr != nil {
				return nil, fmt.Errorf("wsfev1: reproceso 10016: %w", rErr)
			}
			if found {
				return recovered, nil
			}
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

// CondicionesIVAReceptor returns the receptor IVA conditions (RG 5616,
// FEParamGetCondicionIvaReceptor) admitted for claseCmp: "A", "B", "C", "ALEY"
// (A con leyenda "operación sujeta a retención") or "49" (bienes usados).
// An empty claseCmp lists every condition with all its classes.
func (c *Client) CondicionesIVAReceptor(ctx context.Context, token *wsaa.TokenAcceso, cuit int64, claseCmp string) ([]ParamCondicionIvaReceptor, error) {
	req := FEParamGetCondicionIvaReceptorRequest{
		XMLNS:    wsfeNS,
		Auth:     c.auth(token, cuit),
		ClaseCmp: claseCmp,
	}
	var resp FEParamGetCondicionIvaReceptorResponse

	if err := c.soap.Call(ctx, c.url(), actionCondIvaReceptor, &req, &resp); err != nil {
		return nil, &afipkit.ErrSOAP{Err: err}
	}
	result := resp.FEParamGetCondicionIvaReceptorResult
	if afipErr := errorsToAFIP(result.Errors); afipErr != nil {
		return nil, afipErr
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
