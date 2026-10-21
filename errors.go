package afipkit

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// AFIPError represents one or more errors returned by AFIP web services.
type AFIPError struct {
	Errors []AFIPErrorDetail
}

// AFIPErrorDetail is a single AFIP error code + message.
type AFIPErrorDetail struct {
	Code int
	Msg  string
}

func (e *AFIPError) Error() string {
	if len(e.Errors) == 0 {
		return "afip: unknown error"
	}
	if len(e.Errors) == 1 {
		return fmt.Sprintf("afip error %d: %s", e.Errors[0].Code, e.Errors[0].Msg)
	}
	parts := make([]string, len(e.Errors))
	for i, d := range e.Errors {
		parts[i] = fmt.Sprintf("[%d] %s", d.Code, d.Msg)
	}
	return fmt.Sprintf("afip errors: %s", strings.Join(parts, "; "))
}

// FirstCode returns the first error code, or 0 if empty.
func (e *AFIPError) FirstCode() int {
	if len(e.Errors) == 0 {
		return 0
	}
	return e.Errors[0].Code
}

// HasCode checks if any error matches the given code.
func (e *AFIPError) HasCode(code int) bool {
	for _, d := range e.Errors {
		if d.Code == code {
			return true
		}
	}
	return false
}

// Well-known AFIP error codes.
const (
	// ErrCodeAlreadyRegistered (10016): CbteDesde/CbteFch no corresponde al
	// próximo a autorizar. Tras un corte suele significar que el comprobante ya
	// fue autorizado, pero también aparece con un número salteado o una fecha
	// fuera de rango: hay que verificarlo con FECompConsultar (wsfev1.Recuperar),
	// nunca asumirlo.
	ErrCodeAlreadyRegistered = 10016
	ErrCodeInvalidPtoVta     = 10005 // Punto de venta no dado de alta o no es del tipo RECE
	ErrCodePtoVtaOutOfRange  = 10004 // PtoVta fuera de 1..99998
	ErrCodeUnauthorized      = 10000 // CUIT not authorized
	ErrCodeInvalidNumber     = 10013 // Invalid comprobante number
	ErrCodeAmountMismatch    = 10048 // Amounts don't add up
	ErrCodeReceiverNotID     = 10015 // Receiver not identified

	// Errores de infraestructura de AFIP (estructura Errors, todos los métodos).
	ErrCodeInternalApp = 500 // Error interno de aplicación
	ErrCodeInternalDB  = 501 // Error interno de base de datos
	ErrCodeInternalTx  = 502 // Error interno de base de datos - transacción activa
	ErrCodeNotFound    = 602 // No existen datos en nuestros registros (FECompConsultar)

	// Errores de autenticación del token WSAA en los servicios de negocio.
	ErrCodeTokenRejected  = 600 // No se corresponden token y firma / token vencido
	ErrCodeCUITNotInToken = 601 // CUIT representada no incluida en el token

	// Condición frente al IVA del receptor (RG 5616).
	ErrCodeCondIVAReceptorInvalid  = 10242 // valor no permitido
	ErrCodeCondIVAReceptorClase    = 10243 // no válido para la clase de comprobante
	ErrCodeCondIVAReceptorRequired = 10246 // obligatorio (RG 5616)
)

// permanentErrors are AFIP error codes that indicate a permanent failure.
// Retrying these without fixing the request data will produce the same error.
var permanentErrors = map[int]bool{
	ErrCodeInvalidPtoVta:  true, // config issue
	ErrCodeUnauthorized:   true, // cert/CUIT not associated
	ErrCodeAmountMismatch: true, // calculation error
	ErrCodeReceiverNotID:  true, // missing receiver data
}

// IsAlreadyRegistered returns true for error 10016 (número no es el próximo a
// autorizar). It is the trigger for idempotent retry, but it does NOT prove the
// comprobante exists: confirm it with FECompConsultar (wsfev1.Recuperar).
func (e *AFIPError) IsAlreadyRegistered() bool { return e.HasCode(ErrCodeAlreadyRegistered) }

// IsInvalidPtoVta returns true for punto de venta errors: not registered as
// RECE (10005) or out of range (10004).
func (e *AFIPError) IsInvalidPtoVta() bool {
	return e.HasCode(ErrCodeInvalidPtoVta) || e.HasCode(ErrCodePtoVtaOutOfRange)
}

// IsUnauthorized returns true when the CUIT is not authorized for the service.
func (e *AFIPError) IsUnauthorized() bool { return e.HasCode(ErrCodeUnauthorized) }

// IsInvalidNumber returns true for correlative number errors (resync needed).
func (e *AFIPError) IsInvalidNumber() bool { return e.HasCode(ErrCodeInvalidNumber) }

// IsAmountMismatch returns true when ImpTotal != sum of parts.
func (e *AFIPError) IsAmountMismatch() bool { return e.HasCode(ErrCodeAmountMismatch) }

// IsNotFound returns true when AFIP has no record for the query (error 602).
func (e *AFIPError) IsNotFound() bool { return e.HasCode(ErrCodeNotFound) }

// IsInternal returns true for AFIP infrastructure errors (500, 501, 502): the
// service failed internally and did not give a business answer.
func (e *AFIPError) IsInternal() bool {
	return e.HasCode(ErrCodeInternalApp) || e.HasCode(ErrCodeInternalDB) || e.HasCode(ErrCodeInternalTx)
}

// IsRetryable returns true if the error might succeed on retry (e.g., network issues
// or already-registered comprobantes). Permanent errors like invalid amounts or
// unauthorized CUIT are NOT retryable.
func (e *AFIPError) IsRetryable() bool {
	for _, d := range e.Errors {
		if permanentErrors[d.Code] {
			return false
		}
	}
	// Errors not in the permanent list (including 10016) are potentially retryable
	return true
}

// NewAFIPError creates an AFIPError from a list of details. Returns nil if empty.
func NewAFIPError(details []AFIPErrorDetail) *AFIPError {
	if len(details) == 0 {
		return nil
	}
	return &AFIPError{Errors: details}
}

// IsAFIPError checks whether err is (or wraps) an *AFIPError and returns it.
func IsAFIPError(err error) (*AFIPError, bool) {
	var afipErr *AFIPError
	if errors.As(err, &afipErr) {
		return afipErr, true
	}
	return nil, false
}

// hasAFIPCode reports whether err is (or wraps) an *AFIPError with any of codes.
func hasAFIPCode(err error, codes ...int) bool {
	afipErr, ok := IsAFIPError(err)
	if !ok {
		return false
	}
	for _, c := range codes {
		if afipErr.HasCode(c) {
			return true
		}
	}
	return false
}

// IsTokenRechazado reports AFIP error 600: the WSAA token/sign was not
// accepted (expired, revoked, or issued for another certificate). Getting a
// new token may fix it — see wsaa.CachedClient.CallWithAuthRetry.
func IsTokenRechazado(err error) bool { return hasAFIPCode(err, ErrCodeTokenRejected) }

// IsPuntoVentaInvalido reports a punto de venta error (10004, 10005): the
// PtoVta is out of range or is not registered as RECE (web services).
func IsPuntoVentaInvalido(err error) bool {
	return hasAFIPCode(err, ErrCodeInvalidPtoVta, ErrCodePtoVtaOutOfRange)
}

// IsCondicionIVAReceptorInvalida reports a CondicionIVAReceptorId error
// (10242 invalid value, 10243 not valid for the comprobante class, 10246
// missing). Validate locally with fiscal.ValidarCondicionIVAReceptor.
func IsCondicionIVAReceptorInvalida(err error) bool {
	return hasAFIPCode(err, ErrCodeCondIVAReceptorInvalid, ErrCodeCondIVAReceptorClase, ErrCodeCondIVAReceptorRequired)
}

// ErrIncompleteResponse indicates AFIP answered but the response lacks the data
// needed to know the outcome (e.g. FECAESolicitar without FeDetResp, or
// Resultado "A" without CAE).
var ErrIncompleteResponse = errors.New("afip: respuesta incompleta")

// IsIndeterminate reports whether err, returned by wsfev1.SolicitarCAE or
// wsfev1.SolicitarCAEConReproceso, leaves the outcome UNKNOWN: AFIP may or may
// not have authorized the comprobante. The caller must not mark it as rejected;
// it should keep it pending and confirm it later with wsfev1.Recuperar.
//
// The same classification applies to every wsfev1 call (UltimoAutorizado,
// Recuperar, ConsultarComprobante, FEParamGet*): true means "no answer from
// AFIP, try again later"; false with an *AFIPError means AFIP answered and
// rejected the query (for Recuperar, 602 is already turned into found=false).
//
// Indeterminate (true):
//   - *ErrSOAP: network failures, timeouts, HTTP errors, SOAP faults and
//     malformed responses (every soap.Client.Call failure is wrapped in it).
//   - context.DeadlineExceeded / context.Canceled anywhere in the chain.
//   - ErrIncompleteResponse: AFIP answered without a usable result.
//   - *AFIPError with an infrastructure code (500, 501, 502).
//
// Determinate (false):
//   - nil.
//   - *AFIPError with business codes: header Errors or detail Observaciones
//     with Resultado "R" (10016, 10048, 10015, ...). AFIP processed the request
//     and did not authorize it.
//   - Local validation errors (FECAESolicitarRequest.Validate): nothing was sent.
//   - wsfev1.ErrComprobanteNoCoincide: the number is taken by another
//     comprobante — a conflict that needs human attention, not a retry.
//   - Any other error not listed above.
func IsIndeterminate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, ErrIncompleteResponse) {
		return true
	}
	var soapErr *ErrSOAP
	if errors.As(err, &soapErr) {
		return true
	}
	if afipErr, ok := IsAFIPError(err); ok {
		return afipErr.IsInternal()
	}
	return false
}

// --- Infrastructure errors ---

// ErrSOAP represents a SOAP transport-level error (network, timeout, malformed XML).
type ErrSOAP struct {
	Err error
}

func (e *ErrSOAP) Error() string { return fmt.Sprintf("soap: %v", e.Err) }
func (e *ErrSOAP) Unwrap() error { return e.Err }

// ErrWSAAAuth represents an authentication error with WSAA.
type ErrWSAAAuth struct {
	Err error
}

func (e *ErrWSAAAuth) Error() string { return fmt.Sprintf("wsaa auth: %v", e.Err) }
func (e *ErrWSAAAuth) Unwrap() error { return e.Err }

// ErrCertificate represents a certificate validation error.
type ErrCertificate struct {
	Err error
}

func (e *ErrCertificate) Error() string { return fmt.Sprintf("certificate: %v", e.Err) }
func (e *ErrCertificate) Unwrap() error { return e.Err }
