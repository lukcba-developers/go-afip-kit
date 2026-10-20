package fiscal

import (
	"errors"
	"fmt"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

// LeyendaRG5003 es la leyenda que deben llevar los comprobantes clase A (y M)
// emitidos por un Responsable Inscripto a un monotributista (RG 5003/2021,
// Ley 27.618). Ver RequiereLeyendaRG5003.
const LeyendaRG5003 = "El crédito fiscal discriminado en el presente comprobante, sólo podrá ser computado a efectos del Régimen de Sostenimiento e Inclusión Fiscal para Pequeños Contribuyentes de la Ley Nº 27.618"

// RequiereLeyendaRG5003 indica si el comprobante debe imprimir LeyendaRG5003:
// clase A o M con receptor monotributista (condiciones 6, 13 y 16 de RG 5616).
func RequiereLeyendaRG5003(tipoCbte, condicionIVAReceptorID int) bool {
	switch afipkit.CbteLetra(tipoCbte) {
	case "A", "M":
	default:
		return false
	}
	switch condicionIVAReceptorID {
	case afipkit.CondIVAMonotributo, afipkit.CondIVAMonotributistaSocial, afipkit.CondIVAMonotributoTIP:
		return true
	}
	return false
}

// ErrReceptorNoIdentificado indica que el comprobante requiere identificar al
// receptor (clase A/M, o B/C con importe igual o superior al umbral).
var ErrReceptorNoIdentificado = errors.New("fiscal: el receptor debe estar identificado")

// DeterminarDocumento valida y normaliza la identificación del receptor
// (DocTipo, DocNro de FECAEDetRequest) para el tipo de comprobante.
//
// Reglas:
//   - Clase A y M: CUIT (80) válido obligatorio.
//   - Clase B y C sin identificar (DocTipo 99, o DocTipo 0 con DocNro 0): se
//     normaliza a (99, 0); si impTotal >= umbral devuelve
//     ErrReceptorNoIdentificado.
//   - Clase B y C con CUIT (80) o CUIL (86): 11 dígitos con verificador válido.
//   - Clase B y C con DNI (96): número entre 1 y 99.999.999.
//   - Clase B y C con otro documento (pasaporte, CDI, ...): DocNro > 0.
//
// umbral es el importe a partir del cual (inclusive) la identificación es
// obligatoria para consumidores finales; lo fija ARCA y cambia con el tiempo
// (RG 5700/2025: $10.000.000), por eso se recibe como parámetro. Debe ser > 0.
// impTotal debe estar en pesos: para moneda extranjera pasar ImpTotal*MonCotiz.
func DeterminarDocumento(tipoCbte, docTipo int, docNro int64, impTotal, umbral float64) (int, int64, error) {
	if umbral <= 0 {
		return 0, 0, fmt.Errorf("fiscal: umbral de identificacion invalido: %.2f", umbral)
	}

	switch afipkit.CbteLetra(tipoCbte) {
	case "A", "M":
		if docTipo != afipkit.DocCUIT {
			return 0, 0, fmt.Errorf("%w: comprobante clase %s requiere CUIT (DocTipo 80), recibido DocTipo %d",
				ErrReceptorNoIdentificado, afipkit.CbteLetra(tipoCbte), docTipo)
		}
		if err := afipkit.ValidarCUIT(docNro); err != nil {
			return 0, 0, fmt.Errorf("fiscal: CUIT del receptor: %w", err)
		}
		return docTipo, docNro, nil

	case "B", "C":
		if docTipo == afipkit.DocSinIdentificar || (docTipo == 0 && docNro == 0) {
			if toCentavos(impTotal) >= toCentavos(umbral) {
				return 0, 0, fmt.Errorf("%w: importe %.2f igual o superior al umbral %.2f",
					ErrReceptorNoIdentificado, impTotal, umbral)
			}
			return afipkit.DocSinIdentificar, 0, nil
		}
		switch docTipo {
		case afipkit.DocCUIT, afipkit.DocCUIL:
			if err := afipkit.ValidarCUIT(docNro); err != nil {
				return 0, 0, fmt.Errorf("fiscal: documento del receptor: %w", err)
			}
		case afipkit.DocDNI:
			if docNro < 1 || docNro > 99999999 {
				return 0, 0, fmt.Errorf("fiscal: DNI del receptor invalido: %d", docNro)
			}
		default:
			if docTipo <= 0 || docNro <= 0 {
				return 0, 0, fmt.Errorf("fiscal: documento del receptor invalido: DocTipo %d, DocNro %d", docTipo, docNro)
			}
		}
		return docTipo, docNro, nil

	default:
		return 0, 0, fmt.Errorf("fiscal: tipo de comprobante %d no soportado", tipoCbte)
	}
}

// condicionesPorClase son las condiciones frente al IVA del receptor admitidas
// por clase de comprobante (manual WSFEv1 de ARCA, tabla "Condición Frente al
// IVA del receptor"). La clase M sigue a la A.
var condicionesPorClase = map[string][]int{
	"A": {
		afipkit.CondIVAResponsableInscripto, afipkit.CondIVAMonotributo,
		afipkit.CondIVAMonotributistaSocial, afipkit.CondIVAMonotributoTIP,
	},
	"B": {
		afipkit.CondIVAExento, afipkit.CondIVAConsumidorFinal, afipkit.CondIVANoCategorizado,
		afipkit.CondIVAProveedorExterior, afipkit.CondIVAClienteExterior,
		afipkit.CondIVALiberadoLey19640, afipkit.CondIVANoAlcanzado,
	},
	"C": {
		afipkit.CondIVAResponsableInscripto, afipkit.CondIVAExento, afipkit.CondIVAConsumidorFinal,
		afipkit.CondIVAMonotributo, afipkit.CondIVANoCategorizado, afipkit.CondIVAProveedorExterior,
		afipkit.CondIVAClienteExterior, afipkit.CondIVALiberadoLey19640,
		afipkit.CondIVAMonotributistaSocial, afipkit.CondIVANoAlcanzado, afipkit.CondIVAMonotributoTIP,
	},
}

// CondicionesIVAReceptorPermitidas devuelve los códigos de condición frente al
// IVA del receptor admitidos para una clase de comprobante ("A", "B", "C" o
// "M"). Devuelve nil para una clase desconocida.
func CondicionesIVAReceptorPermitidas(clase string) []int {
	if clase == "M" {
		clase = "A"
	}
	conds, ok := condicionesPorClase[clase]
	if !ok {
		return nil
	}
	return append([]int(nil), conds...)
}

// ValidarCondicionIVAReceptor verifica que la condición frente al IVA del
// receptor (CondicionIVAReceptorId, RG 5616) sea admitida por el tipo de
// comprobante. Por ejemplo, un Consumidor Final (5) no puede recibir Factura A.
func ValidarCondicionIVAReceptor(tipoCbte, condicionIVAReceptorID int) error {
	clase := afipkit.CbteLetra(tipoCbte)
	permitidas := CondicionesIVAReceptorPermitidas(clase)
	if permitidas == nil {
		return fmt.Errorf("fiscal: tipo de comprobante %d no soportado", tipoCbte)
	}
	for _, id := range permitidas {
		if id == condicionIVAReceptorID {
			return nil
		}
	}
	return fmt.Errorf("fiscal: condicion IVA del receptor %d no admitida en comprobante clase %s (admitidas: %v)",
		condicionIVAReceptorID, clase, permitidas)
}

// CondicionIVAReceptorID devuelve el código de RG 5616 (CondicionIVAReceptorId)
// para la condición. NoResponsable no tiene equivalente en el catálogo vigente
// y devuelve error.
func (c CondicionIVA) CondicionIVAReceptorID() (int, error) {
	switch c {
	case ResponsableInscripto:
		return afipkit.CondIVAResponsableInscripto, nil
	case Monotributista:
		return afipkit.CondIVAMonotributo, nil
	case Exento:
		return afipkit.CondIVAExento, nil
	case ConsumidorFinal:
		return afipkit.CondIVAConsumidorFinal, nil
	default:
		return 0, fmt.Errorf("fiscal: %s no tiene codigo de condicion IVA del receptor (RG 5616)", c)
	}
}
