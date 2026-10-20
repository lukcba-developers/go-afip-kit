package fiscal

import (
	"testing"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

func TestDeterminarTipoComprobante(t *testing.T) {
	tests := []struct {
		name     string
		emisor   CondicionIVA
		receptor CondicionIVA
		expected int
		wantErr  bool
	}{
		{"RI a RI = Factura A", ResponsableInscripto, ResponsableInscripto, afipkit.CbteFacturaA, false},
		{"RI a CF = Factura B", ResponsableInscripto, ConsumidorFinal, afipkit.CbteFacturaB, false},
		{"RI a Mono = Factura A (RG 5003)", ResponsableInscripto, Monotributista, afipkit.CbteFacturaA, false},
		{"RI a Exento = Factura B", ResponsableInscripto, Exento, afipkit.CbteFacturaB, false},
		{"RI a NoResp = Factura B", ResponsableInscripto, NoResponsable, afipkit.CbteFacturaB, false},
		{"Mono a RI = Factura C", Monotributista, ResponsableInscripto, afipkit.CbteFacturaC, false},
		{"Mono a CF = Factura C", Monotributista, ConsumidorFinal, afipkit.CbteFacturaC, false},
		{"Exento a RI = Factura C", Exento, ResponsableInscripto, afipkit.CbteFacturaC, false},
		{"CF emisor = error", ConsumidorFinal, ResponsableInscripto, 0, true},
		{"RI a desconocido = error", ResponsableInscripto, CondicionIVA(99), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeterminarTipoComprobante(tt.emisor, tt.receptor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.expected {
				t.Errorf("got %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestDeterminarTipoNC(t *testing.T) {
	tests := []struct {
		factura  int
		expected int
		wantErr  bool
	}{
		{afipkit.CbteFacturaA, afipkit.CbteNotaCreditoA, false},
		{afipkit.CbteFacturaB, afipkit.CbteNotaCreditoB, false},
		{afipkit.CbteFacturaC, afipkit.CbteNotaCreditoC, false},
		{afipkit.CbteFacturaM, afipkit.CbteNotaCreditoM, false},
		{999, 0, true},
	}

	for _, tt := range tests {
		got, err := DeterminarTipoNC(tt.factura)
		if (err != nil) != tt.wantErr {
			t.Fatalf("DeterminarTipoNC(%d): error = %v, wantErr = %v", tt.factura, err, tt.wantErr)
		}
		if got != tt.expected {
			t.Errorf("DeterminarTipoNC(%d) = %d, want %d", tt.factura, got, tt.expected)
		}
	}
}

func TestDeterminarTipoND(t *testing.T) {
	got, err := DeterminarTipoND(afipkit.CbteFacturaA)
	if err != nil {
		t.Fatal(err)
	}
	if got != afipkit.CbteNotaDebitoA {
		t.Errorf("got %d, want %d", got, afipkit.CbteNotaDebitoA)
	}

	_, err = DeterminarTipoND(999)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestDeterminarTipoDocumento(t *testing.T) {
	// Factura A always requires CUIT
	got := DeterminarTipoDocumento(afipkit.CbteFacturaA, 0)
	if got != afipkit.DocCUIT {
		t.Errorf("Factura A should always be DocCUIT, got %d", got)
	}

	// Factura B with CUIT
	got = DeterminarTipoDocumento(afipkit.CbteFacturaB, 30123456789)
	if got != afipkit.DocCUIT {
		t.Errorf("Factura B with CUIT should be DocCUIT, got %d", got)
	}

	// Factura B without CUIT
	got = DeterminarTipoDocumento(afipkit.CbteFacturaB, 0)
	if got != afipkit.DocSinIdentificar {
		t.Errorf("Factura B without CUIT should be DocSinIdentificar, got %d", got)
	}
}

func TestCondicionIVA_String(t *testing.T) {
	if ResponsableInscripto.String() != "Responsable Inscripto" {
		t.Errorf("unexpected: %s", ResponsableInscripto.String())
	}
	if CondicionIVA(99).String() != "Desconocido" {
		t.Errorf("expected Desconocido for unknown")
	}
}

func TestCondicionIVA_Slug(t *testing.T) {
	if ResponsableInscripto.Slug() != "responsable_inscripto" {
		t.Errorf("unexpected slug: %s", ResponsableInscripto.Slug())
	}
}

func TestCondicionIVA_IsValid(t *testing.T) {
	if !ResponsableInscripto.IsValid() {
		t.Error("ResponsableInscripto should be valid")
	}
	if CondicionDesconocida.IsValid() {
		t.Error("CondicionDesconocida should not be valid")
	}
	if CondicionIVA(99).IsValid() {
		t.Error("99 should not be valid")
	}
}

func TestParseCondicionIVA(t *testing.T) {
	got, err := ParseCondicionIVA("responsable_inscripto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ResponsableInscripto {
		t.Errorf("expected ResponsableInscripto, got %d", got)
	}

	_, err = ParseCondicionIVA("unknown")
	if err == nil {
		t.Fatal("expected error for unknown value")
	}
}
