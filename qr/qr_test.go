package qr

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func validComprobante() Comprobante {
	return Comprobante{
		Fecha:      "2026-06-11",
		CUIT:       20111111112,
		PtoVta:     3,
		TipoCmp:    6,
		NroCmp:     1234,
		Importe:    12100.50,
		TipoDocRec: 80,
		NroDocRec:  20111111112,
		CodAut:     75123456789012,
	}
}

func TestURL_PayloadMatchesSpec(t *testing.T) {
	u, err := URL(validComprobante())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, BaseURL) {
		t.Fatalf("URL must start with %s, got %s", BaseURL, u)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(u, BaseURL))
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}

	// Defaults applied
	if payload["ver"] != float64(1) {
		t.Errorf("ver: expected 1, got %v", payload["ver"])
	}
	if payload["tipoCodAut"] != "E" {
		t.Errorf("tipoCodAut: expected E, got %v", payload["tipoCodAut"])
	}
	if payload["moneda"] != "PES" {
		t.Errorf("moneda: expected PES default, got %v", payload["moneda"])
	}
	if payload["ctz"] != float64(1) {
		t.Errorf("ctz: expected 1 default for PES, got %v", payload["ctz"])
	}

	// codAut must be a JSON number, not a string (spec RG 4892)
	if _, isString := payload["codAut"].(string); isString {
		t.Error("codAut must be numeric per RG 4892, got string")
	}
	if payload["codAut"] != float64(75123456789012) {
		t.Errorf("codAut: got %v", payload["codAut"])
	}
}

func TestURL_Invalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Comprobante)
	}{
		{"fecha formato AFIP en vez de ISO", func(c *Comprobante) { c.Fecha = "20260611" }},
		{"cuit invalido", func(c *Comprobante) { c.CUIT = 12345678901 }},
		{"sin CAE", func(c *Comprobante) { c.CodAut = 0 }},
		{"nroCmp cero", func(c *Comprobante) { c.NroCmp = 0 }},
		{"ctz cero con moneda extranjera", func(c *Comprobante) { c.Moneda = "DOL"; c.Ctz = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validComprobante()
			tc.mutate(&c)
			if _, err := URL(c); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestParseCAE(t *testing.T) {
	n, err := ParseCAE("75123456789012")
	if err != nil || n != 75123456789012 {
		t.Fatalf("got %d, %v", n, err)
	}
	if _, err := ParseCAE("not-a-cae"); err == nil {
		t.Fatal("expected error for non-numeric CAE")
	}
	if _, err := ParseCAE(""); err == nil {
		t.Fatal("expected error for empty CAE")
	}
}
