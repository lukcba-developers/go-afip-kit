package wsfev1

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestFECAEDetRequest_JSONRoundTrip pins the JSON form of FECAEDetRequest that
// consumers persist to rebuild the expected detail for Recuperar. The field
// names are the Go field names; renaming a field would break stored rows, so
// this test fails if that happens.
func TestFECAEDetRequest_JSONRoundTrip(t *testing.T) {
	det := FECAEDetRequest{
		Concepto: 2, DocTipo: 96, DocNro: 30111222, CbteDesde: 15, CbteHasta: 15, CbteFch: "20261021",
		ImpTotal: "12100.00", ImpTotConc: "0", ImpNeto: "10000.00", ImpOpEx: "0", ImpIVA: "2100.00", ImpTrib: "0",
		FchServDesde: "20261001", FchServHasta: "20261031", FchVtoPago: "20261031",
		MonId: "PES", MonCotiz: "1", CondicionIVAReceptorId: 5,
		Iva:       &IvaArray{AlicIva: []AlicIvaItem{{Id: 5, BaseImp: "10000.00", Importe: "2100.00"}}},
		CbtesAsoc: &CbtesAsocArray{CbteAsoc: []CbteAsocItem{{Tipo: 6, PtoVta: 3, Nro: 14, Cuit: 20111111112, CbteFch: "20261020"}}},
	}

	data, err := json.Marshal(det)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"DocTipo":96`, `"DocNro":30111222`, `"CbteFch":"20261021"`, `"ImpTotal":"12100.00"`,
		`"CondicionIVAReceptorId":5`, `"CbtesAsoc":{"CbteAsoc":[`, `"Iva":{"AlicIva":[`,
	} {
		if !strings.Contains(string(data), field) {
			t.Errorf("JSON missing %s: %s", field, data)
		}
	}

	var back FECAEDetRequest
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(det, back) {
		t.Errorf("round trip changed the detail:\n got %+v\nwant %+v", back, det)
	}
}
