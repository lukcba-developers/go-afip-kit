package afipkit

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCbteDescripcion(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{CbteFacturaA, "Factura A"},
		{CbteFacturaB, "Factura B"},
		{CbteFacturaC, "Factura C"},
		{CbteNotaCreditoA, "Nota de Credito A"},
		{CbteFCEFacturaA, "FCE Factura A"},
		{999, "Desconocido"},
	}
	for _, tt := range tests {
		got := CbteDescripcion(tt.code)
		if got != tt.want {
			t.Errorf("CbteDescripcion(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestCbteLetra(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{CbteFacturaA, "A"},
		{CbteNotaCreditoA, "A"},
		{CbteFacturaB, "B"},
		{CbteFacturaC, "C"},
		{CbteFacturaE, "E"},
		{CbteFacturaM, "M"},
		{CbteFCEFacturaA, "A"},
		{999, ""},
	}
	for _, tt := range tests {
		got := CbteLetra(tt.code)
		if got != tt.want {
			t.Errorf("CbteLetra(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestNotaCreditoPara(t *testing.T) {
	if got := NotaCreditoPara(CbteFacturaA); got != CbteNotaCreditoA {
		t.Errorf("got %d, want %d", got, CbteNotaCreditoA)
	}
	if got := NotaCreditoPara(CbteFacturaB); got != CbteNotaCreditoB {
		t.Errorf("got %d, want %d", got, CbteNotaCreditoB)
	}
	if got := NotaCreditoPara(999); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestIVAAlicuotaPorcentaje(t *testing.T) {
	got, err := IVAAlicuotaPorcentaje(IVA21)
	if err != nil {
		t.Fatal(err)
	}
	if got != 21 {
		t.Errorf("got %f, want 21", got)
	}

	got, err = IVAAlicuotaPorcentaje(IVA105)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10.5 {
		t.Errorf("got %f, want 10.5", got)
	}

	_, err = IVAAlicuotaPorcentaje(999)
	if err == nil {
		t.Fatal("expected error for unknown alicuota")
	}
}

func TestValidarCUIT(t *testing.T) {
	// Valid CUITs (verified with mod-11 algorithm)
	validCUITs := []int64{
		30111111118, // empresa (ficticio)
		27222222228, // persona fisica (ficticio)
	}
	for _, cuit := range validCUITs {
		if err := ValidarCUIT(cuit); err != nil {
			t.Errorf("ValidarCUIT(%d) unexpected error: %v", cuit, err)
		}
	}

	// Invalid: too short
	if err := ValidarCUIT(123); err == nil {
		t.Error("expected error for short CUIT")
	}

	// Invalid: too long
	if err := ValidarCUIT(201234567890); err == nil {
		t.Error("expected error for long CUIT")
	}
}

func TestIsIndeterminate(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"soap transport", &ErrSOAP{Err: errors.New("connection reset")}, true},
		{"wrapped soap", fmt.Errorf("reproceso: %w", &ErrSOAP{Err: errors.New("eof")}), true},
		{"deadline", fmt.Errorf("x: %w", context.DeadlineExceeded), true},
		{"canceled", context.Canceled, true},
		{"incomplete response", fmt.Errorf("wsfev1: %w", ErrIncompleteResponse), true},
		{"afip internal 500", NewAFIPError([]AFIPErrorDetail{{Code: 500}}), true},
		{"afip internal 502", NewAFIPError([]AFIPErrorDetail{{Code: 502}}), true},
		{"afip business 10048", NewAFIPError([]AFIPErrorDetail{{Code: 10048}}), false},
		{"afip 10016", NewAFIPError([]AFIPErrorDetail{{Code: 10016}}), false},
		{"afip auth 600", NewAFIPError([]AFIPErrorDetail{{Code: 600}}), false},
		{"other", errors.New("wsfev1: CantReg=2 pero hay 1 detalles"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsIndeterminate(tt.err); got != tt.want {
				t.Errorf("IsIndeterminate(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestAFIPError_IsNotFound(t *testing.T) {
	if !NewAFIPError([]AFIPErrorDetail{{Code: 602}}).IsNotFound() {
		t.Error("602 should be not found")
	}
	if NewAFIPError([]AFIPErrorDetail{{Code: 10016}}).IsNotFound() {
		t.Error("10016 is not 602")
	}
}

func TestNormalizarCUIT(t *testing.T) {
	ok := map[string]int64{
		"20111111112":        20111111112,
		"20-11111111-2":      20111111112,
		" 20 11111111 2 ":    20111111112,
		"CUIT 20-12345678-6": 20123456786,
	}
	for in, want := range ok {
		got, err := NormalizarCUIT(in)
		if err != nil || got != want {
			t.Errorf("NormalizarCUIT(%q) = (%d, %v), want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "2011111111", "201111111123", "20-11111111-3"} {
		if _, err := NormalizarCUIT(in); err == nil {
			t.Errorf("NormalizarCUIT(%q): expected error", in)
		}
	}
}

func TestErrorCodeHelpers(t *testing.T) {
	wrap := func(code int) error {
		return fmt.Errorf("wsfev1: %w", NewAFIPError([]AFIPErrorDetail{{Code: code, Msg: "x"}}))
	}
	if !IsTokenRechazado(wrap(600)) || IsTokenRechazado(wrap(601)) {
		t.Error("IsTokenRechazado must match 600 only")
	}
	if !IsPuntoVentaInvalido(wrap(10004)) || !IsPuntoVentaInvalido(wrap(10005)) || IsPuntoVentaInvalido(wrap(10016)) {
		t.Error("IsPuntoVentaInvalido must match 10004/10005")
	}
	for _, c := range []int{10242, 10243, 10246} {
		if !IsCondicionIVAReceptorInvalida(wrap(c)) {
			t.Errorf("IsCondicionIVAReceptorInvalida(%d) = false", c)
		}
	}
	if IsCondicionIVAReceptorInvalida(wrap(10247)) || IsCondicionIVAReceptorInvalida(errors.New("x")) || IsTokenRechazado(nil) {
		t.Error("false positive")
	}
	if !NewAFIPError([]AFIPErrorDetail{{Code: 10004}}).IsInvalidPtoVta() {
		t.Error("IsInvalidPtoVta must include 10004")
	}
}
