package fiscal

import (
	"errors"
	"testing"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

const umbralTest = 10_000_000

func TestDeterminarDocumento(t *testing.T) {
	tests := []struct {
		name     string
		tipoCbte int
		docTipo  int
		docNro   int64
		total    float64
		wantTipo int
		wantNro  int64
		wantErr  bool
		noIdent  bool // error must be ErrReceptorNoIdentificado
	}{
		{"A con CUIT", afipkit.CbteFacturaA, 80, 20111111112, 5000, 80, 20111111112, false, false},
		{"A con DNI", afipkit.CbteFacturaA, 96, 30111222, 5000, 0, 0, true, true},
		{"A sin identificar", afipkit.CbteFacturaA, 99, 0, 5000, 0, 0, true, true},
		{"A CUIT invalido", afipkit.CbteFacturaA, 80, 20111111113, 5000, 0, 0, true, false},
		{"NC A con CUIT", afipkit.CbteNotaCreditoA, 80, 20111111112, 5000, 80, 20111111112, false, false},
		{"C sin identificar bajo umbral", afipkit.CbteFacturaC, 99, 0, 9_999_999.99, 99, 0, false, false},
		{"C sin identificar, DocNro se normaliza", afipkit.CbteFacturaC, 99, 12345, 100, 99, 0, false, false},
		{"C doc vacio = sin identificar", afipkit.CbteFacturaC, 0, 0, 100, 99, 0, false, false},
		{"C sin identificar en el umbral", afipkit.CbteFacturaC, 99, 0, umbralTest, 0, 0, true, true},
		{"B sin identificar sobre umbral", afipkit.CbteFacturaB, 99, 0, 12_000_000, 0, 0, true, true},
		{"NC C sin identificar sobre umbral", afipkit.CbteNotaCreditoC, 99, 0, 12_000_000, 0, 0, true, true},
		{"C con DNI sobre umbral", afipkit.CbteFacturaC, 96, 30111222, 12_000_000, 96, 30111222, false, false},
		{"B con DNI", afipkit.CbteFacturaB, 96, 30111222, 100, 96, 30111222, false, false},
		{"C con DNI invalido", afipkit.CbteFacturaC, 96, 0, 100, 0, 0, true, false},
		{"C con DNI demasiado largo", afipkit.CbteFacturaC, 96, 100000000, 100, 0, 0, true, false},
		{"C con CUIT", afipkit.CbteFacturaC, 80, 20111111112, 100, 80, 20111111112, false, false},
		{"C con CUIL invalido", afipkit.CbteFacturaC, 86, 20111111113, 100, 0, 0, true, false},
		{"C con pasaporte", afipkit.CbteFacturaC, 94, 123456, 100, 94, 123456, false, false},
		{"C con tipo sin numero", afipkit.CbteFacturaC, 94, 0, 100, 0, 0, true, false},
		{"tipo no soportado", 999, 99, 0, 100, 0, 0, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tipo, nro, err := DeterminarDocumento(tt.tipoCbte, tt.docTipo, tt.docNro, tt.total, umbralTest)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.noIdent && !errors.Is(err, ErrReceptorNoIdentificado) {
				t.Errorf("expected ErrReceptorNoIdentificado, got %v", err)
			}
			if tipo != tt.wantTipo || nro != tt.wantNro {
				t.Errorf("got (%d, %d), want (%d, %d)", tipo, nro, tt.wantTipo, tt.wantNro)
			}
		})
	}
}

func TestDeterminarDocumento_UmbralInvalido(t *testing.T) {
	if _, _, err := DeterminarDocumento(afipkit.CbteFacturaC, 99, 0, 100, 0); err == nil {
		t.Error("expected error for umbral 0")
	}
}

func TestRequiereLeyendaRG5003(t *testing.T) {
	tests := []struct {
		tipoCbte, cond int
		want           bool
	}{
		{afipkit.CbteFacturaA, afipkit.CondIVAMonotributo, true},
		{afipkit.CbteNotaCreditoA, afipkit.CondIVAMonotributistaSocial, true},
		{afipkit.CbteFacturaM, afipkit.CondIVAMonotributoTIP, true},
		{afipkit.CbteFacturaA, afipkit.CondIVAResponsableInscripto, false},
		{afipkit.CbteFacturaC, afipkit.CondIVAMonotributo, false},
		{afipkit.CbteFacturaB, afipkit.CondIVAConsumidorFinal, false},
	}
	for _, tt := range tests {
		if got := RequiereLeyendaRG5003(tt.tipoCbte, tt.cond); got != tt.want {
			t.Errorf("RequiereLeyendaRG5003(%d, %d) = %v, want %v", tt.tipoCbte, tt.cond, got, tt.want)
		}
	}
	if LeyendaRG5003 == "" {
		t.Error("LeyendaRG5003 vacia")
	}
}

func TestValidarCondicionIVAReceptor(t *testing.T) {
	ok := []struct{ tipoCbte, cond int }{
		{afipkit.CbteFacturaA, afipkit.CondIVAResponsableInscripto},
		{afipkit.CbteFacturaA, afipkit.CondIVAMonotributo},
		{afipkit.CbteFacturaA, afipkit.CondIVAMonotributoTIP},
		{afipkit.CbteFacturaM, afipkit.CondIVAMonotributistaSocial},
		{afipkit.CbteFacturaB, afipkit.CondIVAConsumidorFinal},
		{afipkit.CbteNotaCreditoB, afipkit.CondIVAExento},
		{afipkit.CbteFacturaB, afipkit.CondIVANoAlcanzado},
		{afipkit.CbteFacturaC, afipkit.CondIVAConsumidorFinal},
		{afipkit.CbteFacturaC, afipkit.CondIVAResponsableInscripto},
		{afipkit.CbteNotaCreditoC, afipkit.CondIVAMonotributo},
	}
	for _, tt := range ok {
		if err := ValidarCondicionIVAReceptor(tt.tipoCbte, tt.cond); err != nil {
			t.Errorf("(%d, %d): unexpected error %v", tt.tipoCbte, tt.cond, err)
		}
	}

	bad := []struct{ tipoCbte, cond int }{
		{afipkit.CbteFacturaA, afipkit.CondIVAConsumidorFinal},
		{afipkit.CbteFacturaA, afipkit.CondIVAExento},
		{afipkit.CbteFacturaB, afipkit.CondIVAResponsableInscripto},
		{afipkit.CbteFacturaB, afipkit.CondIVAMonotributo},
		{afipkit.CbteFacturaC, 2}, // codigo inexistente
		{afipkit.CbteFacturaC, 0},
		{999, afipkit.CondIVAConsumidorFinal},
	}
	for _, tt := range bad {
		if err := ValidarCondicionIVAReceptor(tt.tipoCbte, tt.cond); err == nil {
			t.Errorf("(%d, %d): expected error", tt.tipoCbte, tt.cond)
		}
	}
}

func TestCondicionesIVAReceptorPermitidas(t *testing.T) {
	if got := len(CondicionesIVAReceptorPermitidas("C")); got != 11 {
		t.Errorf("clase C: expected 11 condiciones, got %d", got)
	}
	a := CondicionesIVAReceptorPermitidas("A")
	m := CondicionesIVAReceptorPermitidas("M")
	if len(a) != 4 || len(m) != len(a) {
		t.Errorf("clase A/M: expected 4 condiciones, got A=%v M=%v", a, m)
	}
	// The returned slice is a copy: mutating it must not affect the catalog.
	a[0] = -1
	if CondicionesIVAReceptorPermitidas("A")[0] == -1 {
		t.Error("CondicionesIVAReceptorPermitidas must return a copy")
	}
	if CondicionesIVAReceptorPermitidas("Z") != nil {
		t.Error("unknown clase should return nil")
	}
}

func TestCondicionIVA_CondicionIVAReceptorID(t *testing.T) {
	tests := []struct {
		c    CondicionIVA
		want int
	}{
		{ResponsableInscripto, afipkit.CondIVAResponsableInscripto},
		{Monotributista, afipkit.CondIVAMonotributo},
		{Exento, afipkit.CondIVAExento},
		{ConsumidorFinal, afipkit.CondIVAConsumidorFinal},
	}
	for _, tt := range tests {
		got, err := tt.c.CondicionIVAReceptorID()
		if err != nil || got != tt.want {
			t.Errorf("%s: got (%d, %v), want %d", tt.c, got, err, tt.want)
		}
	}
	if _, err := NoResponsable.CondicionIVAReceptorID(); err == nil {
		t.Error("NoResponsable should have no RG 5616 code")
	}
}
