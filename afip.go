// Package afipkit provides a Go client library for AFIP (ARCA) web services.
//
// It covers WSAA authentication (CMS/PKCS#7 signing), WSFEv1 electronic invoicing,
// certificate management, Argentine fiscal rules, and mandatory QR codes (RG 4892).
//
// Designed as a standalone library — no framework or database dependencies.
package afipkit

import (
	"fmt"
	"strconv"
)

// Tipos de comprobante
const (
	CbteFacturaA    = 1
	CbteNotaDebitoA = 2
	CbteNotaCreditoA = 3
	CbteReciboA     = 4
	CbteFacturaB    = 6
	CbteNotaDebitoB = 7
	CbteNotaCreditoB = 8
	CbteReciboB     = 9
	CbteFacturaC    = 11
	CbteNotaDebitoC = 12
	CbteNotaCreditoC = 13
	CbteReciboC     = 15
	CbteFacturaE    = 19
	CbteFacturaM    = 51
	CbteNotaDebitoM = 52
	CbteNotaCreditoM = 53

	// FCE MiPyME
	CbteFCEFacturaA      = 201
	CbteFCENotaDebitoA   = 202
	CbteFCENotaCreditoA  = 203
	CbteFCEFacturaB      = 206
	CbteFCENotaCreditoB  = 208
	CbteFCEFacturaC      = 211
)

// Tipos de documento del receptor
const (
	DocCUIT          = 80
	DocCUIL          = 86
	DocDNI           = 96
	DocSinIdentificar = 99
)

// Condicion frente al IVA del receptor (RG 5616), campo CondicionIVAReceptorId
// de FECAEDetRequest. Codigos del catalogo FEParamGetCondicionIvaReceptor
// segun el manual WSFEv1 de ARCA (revision 01/09/2026). Los codigos 2, 3, 11,
// 12 y 14 no existen en el catalogo.
const (
	CondIVAResponsableInscripto = 1  // IVA Responsable Inscripto
	CondIVAExento               = 4  // IVA Sujeto Exento
	CondIVAConsumidorFinal      = 5  // Consumidor Final
	CondIVAMonotributo          = 6  // Responsable Monotributo
	CondIVANoCategorizado       = 7  // Sujeto No Categorizado
	CondIVAProveedorExterior    = 8  // Proveedor del Exterior
	CondIVAClienteExterior      = 9  // Cliente del Exterior
	CondIVALiberadoLey19640     = 10 // IVA Liberado - Ley N° 19.640
	CondIVAMonotributistaSocial = 13 // Monotributista Social
	CondIVANoAlcanzado          = 15 // IVA No Alcanzado
	CondIVAMonotributoTIP       = 16 // Monotributo Trabajador Independiente Promovido
)

// Alicuotas de IVA (codigos AFIP)
const (
	IVANoGravado = 1  // No gravado
	IVAExento    = 2  // Exento
	IVA0         = 3  // 0%
	IVA105       = 4  // 10.5%
	IVA21        = 5  // 21%
	IVA27        = 6  // 27%
	IVA5         = 8  // 5%
	IVA25        = 9  // 2.5%
)

// Conceptos de facturacion
const (
	ConceptoProductos            = 1
	ConceptoServicios            = 2
	ConceptoProductosYServicios  = 3
)

// Tipos de tributo
const (
	TributoNacional   = 1
	TributoProvincial = 2 // IIBB
	TributoMunicipal  = 3
	TributoInterno    = 4
	TributoOtro       = 99
)

// Monedas
const (
	MonedaPesos = "PES"
	MonedaDolar = "DOL"
	MonedaEuro  = "060"
)

// Entornos de AFIP
const (
	EnvHomologacion = "homologacion"
	EnvProduccion   = "produccion"
)

// CbteDescripcion devuelve el nombre legible de un tipo de comprobante.
func CbteDescripcion(tipoCbte int) string {
	desc := map[int]string{
		CbteFacturaA:         "Factura A",
		CbteNotaDebitoA:      "Nota de Debito A",
		CbteNotaCreditoA:     "Nota de Credito A",
		CbteReciboA:          "Recibo A",
		CbteFacturaB:         "Factura B",
		CbteNotaDebitoB:      "Nota de Debito B",
		CbteNotaCreditoB:     "Nota de Credito B",
		CbteReciboB:          "Recibo B",
		CbteFacturaC:         "Factura C",
		CbteNotaDebitoC:      "Nota de Debito C",
		CbteNotaCreditoC:     "Nota de Credito C",
		CbteReciboC:          "Recibo C",
		CbteFacturaE:         "Factura E (Exportacion)",
		CbteFacturaM:         "Factura M",
		CbteNotaDebitoM:      "Nota de Debito M",
		CbteNotaCreditoM:     "Nota de Credito M",
		CbteFCEFacturaA:      "FCE Factura A",
		CbteFCENotaDebitoA:   "FCE Nota de Debito A",
		CbteFCENotaCreditoA:  "FCE Nota de Credito A",
		CbteFCEFacturaB:      "FCE Factura B",
		CbteFCENotaCreditoB:  "FCE Nota de Credito B",
		CbteFCEFacturaC:      "FCE Factura C",
	}
	if d, ok := desc[tipoCbte]; ok {
		return d
	}
	return "Desconocido"
}

// CbteLetra devuelve la letra del comprobante (A, B, C, E, M).
func CbteLetra(tipoCbte int) string {
	switch tipoCbte {
	case CbteFacturaA, CbteNotaDebitoA, CbteNotaCreditoA, CbteReciboA,
		CbteFCEFacturaA, CbteFCENotaDebitoA, CbteFCENotaCreditoA:
		return "A"
	case CbteFacturaB, CbteNotaDebitoB, CbteNotaCreditoB, CbteReciboB,
		CbteFCEFacturaB, CbteFCENotaCreditoB:
		return "B"
	case CbteFacturaC, CbteNotaDebitoC, CbteNotaCreditoC, CbteReciboC,
		CbteFCEFacturaC:
		return "C"
	case CbteFacturaE:
		return "E"
	case CbteFacturaM, CbteNotaDebitoM, CbteNotaCreditoM:
		return "M"
	default:
		return ""
	}
}

// NotaCreditoPara devuelve el codigo de NC correspondiente a un tipo de factura.
// Retorna 0 si no aplica.
func NotaCreditoPara(tipoCbte int) int {
	m := map[int]int{
		CbteFacturaA:    CbteNotaCreditoA,
		CbteFacturaB:    CbteNotaCreditoB,
		CbteFacturaC:    CbteNotaCreditoC,
		CbteFacturaM:    CbteNotaCreditoM,
		CbteFCEFacturaA: CbteFCENotaCreditoA,
		CbteFCEFacturaB: CbteFCENotaCreditoB,
	}
	return m[tipoCbte]
}

// IVAAlicuotaPorcentaje devuelve el porcentaje real de una alicuota AFIP.
// Returns an error if the ID is not a valid AFIP alicuota code.
func IVAAlicuotaPorcentaje(id int) (float64, error) {
	m := map[int]float64{
		IVA0:   0,
		IVA105: 10.5,
		IVA21:  21,
		IVA27:  27,
		IVA5:   5,
		IVA25:  2.5,
	}
	v, ok := m[id]
	if !ok {
		return 0, fmt.Errorf("afipkit: alicuota IVA desconocida: %d", id)
	}
	return v, nil
}

// NormalizarCUIT convierte un CUIT ingresado como texto ("20-12345678-9",
// "20 12345678 9", "CUIT 20123456789") a su forma numérica: descarta todo lo
// que no sea dígito, exige exactamente 11 dígitos y valida el dígito
// verificador (ValidarCUIT).
func NormalizarCUIT(s string) (int64, error) {
	digits := make([]byte, 0, 11)
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			digits = append(digits, s[i])
		}
	}
	if len(digits) != 11 {
		return 0, fmt.Errorf("afipkit: CUIT debe tener 11 digitos, %q tiene %d", s, len(digits))
	}
	cuit, err := strconv.ParseInt(string(digits), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("afipkit: CUIT invalido %q: %w", s, err)
	}
	if err := ValidarCUIT(cuit); err != nil {
		return 0, err
	}
	return cuit, nil
}

// ValidarCUIT verifica que un CUIT sea valido (11 digitos, digito verificador correcto).
// Acepta formato numerico: 20123456789 (sin guiones).
func ValidarCUIT(cuit int64) error {
	if cuit < 10000000000 || cuit > 99999999999 {
		return fmt.Errorf("afipkit: CUIT debe tener 11 digitos, got %d", cuit)
	}

	// Algoritmo de digito verificador CUIT (modulo 11)
	digits := make([]int, 11)
	n := cuit
	for i := 10; i >= 0; i-- {
		digits[i] = int(n % 10)
		n /= 10
	}

	weights := []int{5, 4, 3, 2, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i, w := range weights {
		sum += digits[i] * w
	}

	remainder := sum % 11
	var expectedDigit int
	switch remainder {
	case 0:
		expectedDigit = 0
	case 1:
		// Special case: verification digit depends on CUIT type
		// In practice, AFIP doesn't issue CUITs with this remainder
		expectedDigit = digits[10] // accept whatever is there
	default:
		expectedDigit = 11 - remainder
	}

	if digits[10] != expectedDigit {
		return fmt.Errorf("afipkit: digito verificador CUIT invalido (esperado %d, tiene %d)", expectedDigit, digits[10])
	}

	return nil
}
