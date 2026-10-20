package wsfev1

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	afipkit "github.com/lukcba-developers/go-afip-kit"
)

func TestCondicionesIVAReceptor(t *testing.T) {
	var gotBody, gotAction string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotAction = string(b), r.Header.Get("SOAPAction")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(envelope(`<FEParamGetCondicionIvaReceptorResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FEParamGetCondicionIvaReceptorResult><ResultGet>
<CondicionIvaReceptor><Id>4</Id><Desc>IVA Sujeto Exento</Desc><Cmp_Clase>B/C</Cmp_Clase></CondicionIvaReceptor>
<CondicionIvaReceptor><Id>5</Id><Desc>Consumidor Final</Desc><Cmp_Clase>B/C/49</Cmp_Clase></CondicionIvaReceptor>
</ResultGet></FEParamGetCondicionIvaReceptorResult></FEParamGetCondicionIvaReceptorResponse>`)))
	}))
	defer srv.Close()

	conds, err := testClient(srv.URL).CondicionesIVAReceptor(context.Background(), testToken, 20111111112, "B")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAction != actionCondIvaReceptor {
		t.Errorf("SOAPAction = %q", gotAction)
	}
	if !strings.Contains(gotBody, "<ClaseCmp>B</ClaseCmp>") || !strings.Contains(gotBody, "<FEParamGetCondicionIvaReceptor") {
		t.Errorf("unexpected request body: %s", gotBody)
	}
	if len(conds) != 2 || conds[1].Id != afipkit.CondIVAConsumidorFinal || conds[1].CmpClase != "B/C/49" {
		t.Errorf("unexpected result: %+v", conds)
	}
}

func TestCondicionesIVAReceptor_SinClaseNoEnviaCampo(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(envelope(`<FEParamGetCondicionIvaReceptorResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FEParamGetCondicionIvaReceptorResult>
<Errors><Err><Code>10244</Code><Msg>Clase invalida</Msg></Err></Errors></FEParamGetCondicionIvaReceptorResult></FEParamGetCondicionIvaReceptorResponse>`)))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).CondicionesIVAReceptor(context.Background(), testToken, 20111111112, "")
	if strings.Contains(gotBody, "ClaseCmp") {
		t.Errorf("empty clase must be omitted: %s", gotBody)
	}
	if afipErr, ok := afipkit.IsAFIPError(err); !ok || afipErr.FirstCode() != 10244 {
		t.Errorf("expected AFIPError 10244, got %v", err)
	}
}
