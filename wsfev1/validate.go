package wsfev1

import (
	"fmt"
	"math"
	"strconv"

	"github.com/lukcba-developers/go-afip-kit/fiscal"
)

// Validate checks the request against the WSFEv1 rules that AFIP enforces
// server-side, so obvious mistakes fail fast with a clear message instead of
// costing a round-trip and an opaque AFIP error code.
//
// It verifies header consistency (CantReg, PtoVta), per-detail invariants
// (numbering, date formats, service dates for servicios), exact amount
// cuadratura (AFIP error 10048) and that the IVA breakdown matches ImpNeto and
// ImpIVA. It does NOT call AFIP and cannot catch server-side state errors
// (wrong correlative number, unauthorized CUIT, etc.).
func (r *FECAESolicitarRequest) Validate() error {
	cab := r.FeCAEReq.FeCabReq
	dets := r.FeCAEReq.FeDetReq

	if len(dets) == 0 {
		return fmt.Errorf("wsfev1: FeDetReq vacio")
	}
	if cab.CantReg != len(dets) {
		return fmt.Errorf("wsfev1: CantReg=%d pero hay %d detalles", cab.CantReg, len(dets))
	}
	if cab.PtoVta < 1 || cab.PtoVta > 99998 {
		return fmt.Errorf("wsfev1: PtoVta %d fuera de rango (1-99998)", cab.PtoVta)
	}
	if cab.CbteTipo <= 0 {
		return fmt.Errorf("wsfev1: CbteTipo invalido: %d", cab.CbteTipo)
	}

	for i, det := range dets {
		if err := validateDet(det); err != nil {
			return fmt.Errorf("wsfev1: detalle %d: %w", i, err)
		}
	}
	return nil
}

func validateDet(det FECAEDetRequest) error {
	if det.CbteDesde <= 0 || det.CbteHasta < det.CbteDesde {
		return fmt.Errorf("rango CbteDesde=%d CbteHasta=%d invalido", det.CbteDesde, det.CbteHasta)
	}
	if det.CbteFch != "" {
		if err := fiscal.ValidarFechaAFIP(det.CbteFch); err != nil {
			return fmt.Errorf("CbteFch: %w", err)
		}
	}
	if err := fiscal.ValidarConcepto(det.Concepto, det.FchServDesde, det.FchServHasta, det.FchVtoPago); err != nil {
		return err
	}
	if det.MonId == "" {
		return fmt.Errorf("MonId requerido")
	}
	cotiz, err := parseAmount(det.MonCotiz, "MonCotiz")
	if err != nil {
		return err
	}
	if cotiz <= 0 {
		return fmt.Errorf("MonCotiz requerida y positiva (1 para PES), recibido %q", det.MonCotiz)
	}

	// Exact cuadratura: ImpTotal = ImpTotConc + ImpNeto + ImpOpEx + ImpIVA + ImpTrib
	total, err := parseAmount(det.ImpTotal, "ImpTotal")
	if err != nil {
		return err
	}
	neto, err := parseAmount(det.ImpNeto, "ImpNeto")
	if err != nil {
		return err
	}
	iva, err := parseAmount(det.ImpIVA, "ImpIVA")
	if err != nil {
		return err
	}
	trib, err := parseAmount(det.ImpTrib, "ImpTrib")
	if err != nil {
		return err
	}
	totConc, err := parseAmount(det.ImpTotConc, "ImpTotConc")
	if err != nil {
		return err
	}
	opEx, err := parseAmount(det.ImpOpEx, "ImpOpEx")
	if err != nil {
		return err
	}

	if err := fiscal.ValidarCuadratura(fiscal.Importes{
		ImpNeto:      neto,
		ImpIVA:       iva,
		ImpTributos:  trib,
		ImpNoGravado: totConc,
		ImpExento:    opEx,
		ImpTotal:     total,
	}); err != nil {
		return err
	}

	// The IVA breakdown must match ImpNeto / ImpIVA exactly.
	if det.Iva != nil && len(det.Iva.AlicIva) > 0 {
		alicuotas := make([]fiscal.AlicuotaIVA, len(det.Iva.AlicIva))
		for i, a := range det.Iva.AlicIva {
			base, err := parseAmount(a.BaseImp, fmt.Sprintf("Iva[%d].BaseImp", i))
			if err != nil {
				return err
			}
			imp, err := parseAmount(a.Importe, fmt.Sprintf("Iva[%d].Importe", i))
			if err != nil {
				return err
			}
			alicuotas[i] = fiscal.AlicuotaIVA{ID: a.Id, BaseImp: base, Importe: imp}
		}
		if err := fiscal.ValidarAlicuotasIVA(alicuotas, neto, iva); err != nil {
			return err
		}
	} else if math.Round(iva*100) > 0 {
		return fmt.Errorf("ImpIVA=%.2f pero el array Iva esta vacio", iva)
	}

	return nil
}

// parseAmount parses an AFIP decimal-as-string field. Empty is treated as 0
// (the zero value of optional amount fields).
func parseAmount(s, field string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q no es un importe valido", field, s)
	}
	if f < 0 {
		return 0, fmt.Errorf("%s: importe negativo %q", field, s)
	}
	return f, nil
}
