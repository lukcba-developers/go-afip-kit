package wsfev1

import (
	"strings"
	"testing"
)

// validRequest is a Factura B for $12,100.00 ($10,000 neto + $2,100 IVA 21%).
func validRequest() *FECAESolicitarRequest {
	return &FECAESolicitarRequest{
		FeCAEReq: FeCAEReq{
			FeCabReq: FeCabReq{CantReg: 1, PtoVta: 3, CbteTipo: 6},
			FeDetReq: []FECAEDetRequest{{
				Concepto:     2,
				DocTipo:      80,
				DocNro:       20111111112,
				CbteDesde:    100,
				CbteHasta:    100,
				CbteFch:      "20260611",
				ImpTotal:     "12100.00",
				ImpTotConc:   "0.00",
				ImpNeto:      "10000.00",
				ImpOpEx:      "0.00",
				ImpIVA:       "2100.00",
				ImpTrib:      "0.00",
				FchServDesde: "20260601",
				FchServHasta: "20260610",
				FchVtoPago:   "20260711",
				MonId:        "PES",
				MonCotiz:     "1",
				Iva: &IvaArray{AlicIva: []AlicIvaItem{
					{Id: 5, BaseImp: "10000.00", Importe: "2100.00"},
				}},
			}},
		},
	}
}

func TestValidate_OK(t *testing.T) {
	if err := validRequest().Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*FECAESolicitarRequest)
		wantMsg string
	}{
		{
			"FeDetReq vacio",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq = nil },
			"FeDetReq vacio",
		},
		{
			"CantReg no coincide",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeCabReq.CantReg = 2 },
			"CantReg",
		},
		{
			"PtoVta fuera de rango",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeCabReq.PtoVta = 0 },
			"PtoVta",
		},
		{
			"importes no cuadran (error 10048)",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq[0].ImpTotal = "12100.01" },
			"no cuadran",
		},
		{
			"IVA breakdown no coincide con ImpIVA",
			func(r *FECAESolicitarRequest) {
				r.FeCAEReq.FeDetReq[0].Iva.AlicIva[0].Importe = "2000.00"
			},
			"alicuotas",
		},
		{
			"ImpIVA sin array Iva",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq[0].Iva = nil },
			"Iva esta vacio",
		},
		{
			"servicios sin FchVtoPago",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq[0].FchVtoPago = "" },
			"FchVtoPago",
		},
		{
			"fecha en formato ISO en vez de AFIP",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq[0].CbteFch = "2026-06-11" },
			"CbteFch",
		},
		{
			"sin MonCotiz",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq[0].MonCotiz = "" },
			"MonCotiz",
		},
		{
			"rango de numeracion invertido",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq[0].CbteHasta = 99 },
			"CbteDesde",
		},
		{
			"importe no numerico",
			func(r *FECAESolicitarRequest) { r.FeCAEReq.FeDetReq[0].ImpNeto = "10.000,00" },
			"ImpNeto",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest()
			tc.mutate(r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestSolicitarCAE_ValidatesBeforeHTTP ensures an invalid request never
// reaches AFIP — Validate must short-circuit before any SOAP call.
func TestSolicitarCAE_ValidatesBeforeHTTP(t *testing.T) {
	r := validRequest()
	r.FeCAEReq.FeDetReq[0].ImpTotal = "999.99" // no cuadra

	// Client points at production AFIP; if validation failed to short-circuit,
	// this test would attempt a real network call and fail differently.
	c := NewClient(true)
	_, err := c.SolicitarCAE(t.Context(), nil, 20111111112, r)
	if err == nil || !strings.Contains(err.Error(), "no cuadran") {
		t.Fatalf("expected local validation error, got %v", err)
	}
}
