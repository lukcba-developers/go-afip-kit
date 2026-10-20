package fiscal

import (
	"fmt"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

// DeterminarTipoComprobante returns the comprobante type code based on
// the IVA condition of the emisor (seller) and receptor (buyer).
//
// Rules:
//   - RI -> RI:                          Factura A (1)
//   - RI -> Mono:                        Factura A (1), RG 5003 (ver LeyendaRG5003)
//   - RI -> CF/Exento/NoResp:            Factura B (6)
//   - Mono/Exento -> any:                Factura C (11)
//
// Returns 0 and an error for invalid or unsupported combinations.
func DeterminarTipoComprobante(emisor, receptor CondicionIVA) (int, error) {
	switch emisor {
	case ResponsableInscripto:
		switch receptor {
		case ResponsableInscripto, Monotributista:
			return afipkit.CbteFacturaA, nil
		case Exento, ConsumidorFinal, NoResponsable:
			return afipkit.CbteFacturaB, nil
		default:
			return 0, fmt.Errorf("fiscal: condicion IVA receptor desconocida: %d", receptor)
		}
	case Monotributista, Exento:
		return afipkit.CbteFacturaC, nil
	default:
		return 0, fmt.Errorf("fiscal: condicion IVA emisor desconocida: %d", emisor)
	}
}

// DeterminarTipoNC returns the nota de credito type for the given factura type.
// E.g., Factura A -> NC A, Factura B -> NC B.
func DeterminarTipoNC(tipoFactura int) (int, error) {
	nc := afipkit.NotaCreditoPara(tipoFactura)
	if nc == 0 {
		return 0, fmt.Errorf("fiscal: no hay NC para tipo de comprobante %d", tipoFactura)
	}
	return nc, nil
}

// DeterminarTipoND returns the nota de debito type for the given factura type.
func DeterminarTipoND(tipoFactura int) (int, error) {
	m := map[int]int{
		afipkit.CbteFacturaA: afipkit.CbteNotaDebitoA,
		afipkit.CbteFacturaB: afipkit.CbteNotaDebitoB,
		afipkit.CbteFacturaC: afipkit.CbteNotaDebitoC,
		afipkit.CbteFacturaM: afipkit.CbteNotaDebitoM,
	}
	nd, ok := m[tipoFactura]
	if !ok {
		return 0, fmt.Errorf("fiscal: no hay ND para tipo de comprobante %d", tipoFactura)
	}
	return nd, nil
}

// DeterminarTipoDocumento returns the AFIP document type code for the receptor.
//
// Rules:
//   - Factura A always requires CUIT (80)
//   - Factura B/C: CUIT if available, otherwise DocSinIdentificar (99)
//
// For DNI receptors and the mandatory identification threshold use
// DeterminarDocumento.
func DeterminarTipoDocumento(tipoCbte int, receptorCUIT int64) int {
	switch tipoCbte {
	case afipkit.CbteFacturaA, afipkit.CbteNotaCreditoA, afipkit.CbteNotaDebitoA:
		return afipkit.DocCUIT
	default:
		if receptorCUIT > 0 {
			return afipkit.DocCUIT
		}
		return afipkit.DocSinIdentificar
	}
}
