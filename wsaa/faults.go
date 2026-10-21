package wsaa

import (
	"errors"
	"strings"

	"github.com/lukcba-developers/go-afip-kit/soap"
)

// WSAA fault codes (Especificación Técnica WSAA, ARCA). LoginCms returns them as
// the SOAP faultcode, usually with a namespace prefix ("ns1:coe.notAuthorized");
// FaultCode strips it.
const (
	FaultNotAuthorized         = "coe.notAuthorized"          // CEE sin acceso al servicio (falta la relación en el Administrador de Relaciones / WSASS)
	FaultAlreadyAuthenticated  = "coe.alreadyAuthenticated"   // ya hay un TA vigente para ese certificado y servicio
	FaultCMSBad                = "cms.bad"                    // el CMS no es válido
	FaultCMSBadBase64          = "cms.bad.base64"             // no se puede decodificar el base64
	FaultCertNotFound          = "cms.cert.notFound"          // el CMS no trae el certificado de firma
	FaultSignInvalid           = "cms.sign.invalid"           // firma inválida o algoritmo no soportado
	FaultCertExpired           = "cms.cert.expired"           // certificado vencido
	FaultCertInvalid           = "cms.cert.invalid"           // certificado con fecha de generación futura
	FaultCertUntrusted         = "cms.cert.untrusted"         // certificado no emitido por una AC de confianza (p. ej. cert de homo en prod)
	FaultXMLBad                = "xml.bad"                    // el TRA no valida contra el schema
	FaultSourceInvalid         = "xml.source.invalid"         // source no corresponde al DN del certificado
	FaultDestinationInvalid    = "xml.destination.invalid"    // destination no corresponde al DN del WSAA
	FaultVersionNotSupported   = "xml.version.notSupported"   // versión del TRA no soportada
	FaultGenerationTimeInvalid = "xml.generationTime.invalid" // generationTime futuro o de más de 24 h: revisar el reloj
	FaultExpirationTimeExpired = "xml.expirationTime.expired" // expirationTime ya pasó: revisar el reloj
	FaultExpirationTimeInvalid = "xml.expirationTime.invalid" // expirationTime a más de 24 h
	FaultServiceUnavailable    = "wsn.unavailable"            // el servicio de negocio no está disponible
	FaultServiceNotFound       = "wsn.notFound"               // servicio inexistente (p. ej. "wsfe" mal escrito)
	FaultWSAAUnavailable       = "wsaa.unavailable"           // WSAA fuera de servicio
	FaultWSAAInternalError     = "wsaa.internalError"         // WSAA no pudo procesar el requerimiento
)

// FaultCode returns the WSAA fault code carried by err (e.g.
// "coe.notAuthorized"), without its namespace prefix, or "" if err is not a
// SOAP fault. Works through the wrapping done by Client.Authenticate.
func FaultCode(err error) string {
	var fault *soap.Fault
	if !errors.As(err, &fault) {
		return ""
	}
	code := fault.Code
	if i := strings.LastIndex(code, ":"); i >= 0 {
		code = code[i+1:]
	}
	return code
}

// IsAlreadyAuthenticated reports whether err is the WSAA fault
// coe.alreadyAuthenticated ("El CEE ya posee un TA valido para el acceso al
// WSN solicitado").
//
// WSAA refuses to issue a new TA while a previous one is still valid for the
// same certificate + service. If your process lost the cached token (e.g. a
// restart with an in-memory TokenStore), this error means you must wait until
// the previous TA expires (up to 12h) — re-trying immediately will keep
// failing. The durable fix is a persistent TokenStore (Redis, database) so
// the TA survives restarts.
func IsAlreadyAuthenticated(err error) bool {
	var fault *soap.Fault
	if !errors.As(err, &fault) {
		return false
	}
	return strings.Contains(fault.Code, "alreadyAuthenticated") ||
		strings.Contains(fault.String, "ya posee un TA")
}

// IsNotAuthorized reports coe.notAuthorized: the certificate is not
// authorized for the service. Fix the relation in the Administrador de
// Relaciones (production) or WSASS (homologación); retrying will not help.
func IsNotAuthorized(err error) bool { return FaultCode(err) == FaultNotAuthorized }

// IsCertUntrusted reports cms.cert.untrusted: the certificate was not issued by
// a CA that this WSAA trusts — typically a homologación certificate used in
// production or vice versa.
func IsCertUntrusted(err error) bool { return FaultCode(err) == FaultCertUntrusted }

// IsCertExpired reports cms.cert.expired: the certificate is expired.
func IsCertExpired(err error) bool { return FaultCode(err) == FaultCertExpired }

// IsSignatureInvalid reports cms.sign.invalid: invalid signature or
// unsupported algorithm.
func IsSignatureInvalid(err error) bool { return FaultCode(err) == FaultSignInvalid }

// IsClockError reports the faults caused by a wrong system clock
// (xml.generationTime.invalid, xml.expirationTime.expired): sync it with NTP.
func IsClockError(err error) bool {
	switch FaultCode(err) {
	case FaultGenerationTimeInvalid, FaultExpirationTimeExpired:
		return true
	}
	return false
}

// IsUnavailable reports the transient WSAA faults (wsaa.*, wsn.unavailable).
// Per the WSAA spec these are the only faults worth retrying, and not before
// 60 seconds; for any other fault fix the cause before requesting a new TA.
func IsUnavailable(err error) bool {
	switch FaultCode(err) {
	case FaultWSAAUnavailable, FaultWSAAInternalError, FaultServiceUnavailable:
		return true
	}
	return false
}
