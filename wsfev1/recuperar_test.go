package wsfev1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	afipkit "github.com/lukcba-developers/go-afip-kit"
	"github.com/lukcba-developers/go-afip-kit/soap"
	"github.com/lukcba-developers/go-afip-kit/wsaa"
)

// newActionServer answers each SOAPAction with its canned body. Actions
// without a body get HTTP 500. calls counts requests per action.
func newActionServer(t *testing.T, bodies map[string]string, calls map[string]*int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("SOAPAction")
		if n, ok := calls[action]; ok {
			atomic.AddInt32(n, 1)
		}
		body, ok := bodies[action]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func envelope(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body>` + inner + `</soap:Body></soap:Envelope>`
}

const consultarOK = `<FECompConsultarResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FECompConsultarResult><ResultGet>
<Concepto>2</Concepto><DocTipo>96</DocTipo><DocNro>30111222</DocNro><CbteDesde>15</CbteDesde><CbteHasta>15</CbteHasta>
<CbteFch>20261020</CbteFch><ImpTotal>15000</ImpTotal><ImpTotConc>0</ImpTotConc><ImpNeto>15000</ImpNeto><ImpOpEx>0</ImpOpEx>
<ImpIVA>0</ImpIVA><ImpTrib>0</ImpTrib><MonId>PES</MonId><MonCotiz>1</MonCotiz><Resultado>A</Resultado>
<CodAutorizacion>76123456789012</CodAutorizacion><FchVto>20261030</FchVto><EmisionTipo>CAE</EmisionTipo>
<FchProceso>20261020101500</FchProceso><PtoVta>3</PtoVta><CbteTipo>11</CbteTipo>
</ResultGet></FECompConsultarResult></FECompConsultarResponse>`

const consultar602 = `<FECompConsultarResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FECompConsultarResult>
<Errors><Err><Code>602</Code><Msg>No existen datos en nuestros registros para los parametros ingresados.</Msg></Err></Errors>
</FECompConsultarResult></FECompConsultarResponse>`

const solicitar10016 = `<FECAESolicitarResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FECAESolicitarResult>
<FeCabResp><Cuit>20111111112</Cuit><PtoVta>3</PtoVta><CbteTipo>11</CbteTipo><FchProceso>20261020101600</FchProceso>
<CantReg>1</CantReg><Resultado>R</Resultado><Reproceso>N</Reproceso></FeCabResp>
<FeDetResp><FECAEDetResponse><Concepto>2</Concepto><DocTipo>96</DocTipo><DocNro>30111222</DocNro>
<CbteDesde>15</CbteDesde><CbteHasta>15</CbteHasta><CbteFch>20261020</CbteFch><Resultado>R</Resultado><CAE></CAE><CAEFchVto></CAEFchVto>
<Observaciones><Obs><Code>10016</Code><Msg>El numero o fecha del comprobante no se corresponde con el proximo a autorizar.</Msg></Obs></Observaciones>
</FECAEDetResponse></FeDetResp></FECAESolicitarResult></FECAESolicitarResponse>`

func esperadoFacturaC() *FECAEDetRequest {
	return &FECAEDetRequest{
		Concepto: 2, DocTipo: 96, DocNro: 30111222,
		CbteDesde: 15, CbteHasta: 15, CbteFch: "20261020",
		ImpTotal: "15000.00", ImpTotConc: "0", ImpNeto: "15000.00",
		ImpOpEx: "0", ImpIVA: "0", ImpTrib: "0",
		FchServDesde: "20261001", FchServHasta: "20261031", FchVtoPago: "20261031",
		MonId: "PES", MonCotiz: "1", CondicionIVAReceptorId: afipkit.CondIVAConsumidorFinal,
	}
}

func testClient(url string, opts ...soap.Option) *Client {
	return &Client{soap: soap.NewClient(opts...), baseURL: url}
}

var testToken = &wsaa.TokenAcceso{Token: "t", Sign: "s"}

func TestRecuperar_Found(t *testing.T) {
	srv := newActionServer(t, map[string]string{actionCompConsultar: envelope(consultarOK)}, nil)
	c := testClient(srv.URL)

	res, found, err := c.Recuperar(context.Background(), testToken, 20111111112, 3, 11, 15, esperadoFacturaC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if res.CAE != "76123456789012" || res.CAEFchVto != "20261030" || res.CbteDesde != 15 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestRecuperar_NotFound(t *testing.T) {
	srv := newActionServer(t, map[string]string{actionCompConsultar: envelope(consultar602)}, nil)
	c := testClient(srv.URL)

	res, found, err := c.Recuperar(context.Background(), testToken, 20111111112, 3, 11, 15, esperadoFacturaC())
	if err != nil || found || res != nil {
		t.Fatalf("expected (nil, false, nil), got (%v, %v, %v)", res, found, err)
	}
}

func TestRecuperar_NoCoincide(t *testing.T) {
	srv := newActionServer(t, map[string]string{actionCompConsultar: envelope(consultarOK)}, nil)
	c := testClient(srv.URL)

	esp := esperadoFacturaC()
	esp.DocNro = 99999999
	esp.ImpTotal = "15000.01"

	res, found, err := c.Recuperar(context.Background(), testToken, 20111111112, 3, 11, 15, esp)
	if res != nil || found {
		t.Fatalf("expected no result, got (%v, %v)", res, found)
	}
	if !errors.Is(err, ErrComprobanteNoCoincide) {
		t.Fatalf("expected ErrComprobanteNoCoincide, got %v", err)
	}
	var nc *ComprobanteNoCoincideError
	if !errors.As(err, &nc) {
		t.Fatalf("expected *ComprobanteNoCoincideError, got %T", err)
	}
	if len(nc.Diferencias) != 2 {
		t.Errorf("expected 2 differences (DocNro, ImpTotal), got %v", nc.Diferencias)
	}
	if nc.Registrado == nil || nc.Registrado.CodAutorizacion != "76123456789012" {
		t.Errorf("expected registered comprobante attached, got %+v", nc.Registrado)
	}
	if afipkit.IsIndeterminate(err) {
		t.Error("a mismatch is a conflict, not an indeterminate outcome")
	}
}

func TestRecuperar_ImpTotalComparesCentavos(t *testing.T) {
	// AFIP returns "15000"; the request had "15000.00" — same amount.
	srv := newActionServer(t, map[string]string{actionCompConsultar: envelope(consultarOK)}, nil)
	c := testClient(srv.URL)

	esp := esperadoFacturaC()
	esp.CbteFch = "" // AFIP assigned the date: not compared
	_, found, err := c.Recuperar(context.Background(), testToken, 20111111112, 3, 11, 15, esp)
	if err != nil || !found {
		t.Fatalf("expected found, got (%v, %v)", found, err)
	}
}

func TestRecuperar_TransportErrorIsIndeterminate(t *testing.T) {
	srv := newActionServer(t, map[string]string{}, nil) // every call → HTTP 500
	c := testClient(srv.URL)

	_, found, err := c.Recuperar(context.Background(), testToken, 20111111112, 3, 11, 15, esperadoFacturaC())
	if err == nil || found {
		t.Fatalf("expected error, got (%v, %v)", found, err)
	}
	if !afipkit.IsIndeterminate(err) {
		t.Errorf("transport error should be indeterminate: %v", err)
	}
}

func TestRecuperar_InvalidArgs(t *testing.T) {
	c := testClient("http://unused.invalid")
	if _, _, err := c.Recuperar(context.Background(), testToken, 1, 3, 11, 15, nil); err == nil {
		t.Error("expected error for nil esperado")
	}
	if _, _, err := c.Recuperar(context.Background(), testToken, 1, 3, 11, 0, esperadoFacturaC()); err == nil {
		t.Error("expected error for cbteNro=0")
	}
}

func solicitudFacturaC() *FECAESolicitarRequest {
	return &FECAESolicitarRequest{FeCAEReq: FeCAEReq{
		FeCabReq: FeCabReq{CantReg: 1, PtoVta: 3, CbteTipo: 11},
		FeDetReq: []FECAEDetRequest{*esperadoFacturaC()},
	}}
}

func TestSolicitarCAEConReproceso_RecoversMatching(t *testing.T) {
	srv := newActionServer(t, map[string]string{
		actionCAESolicitar:  envelope(solicitar10016),
		actionCompConsultar: envelope(consultarOK),
	}, nil)
	c := testClient(srv.URL)

	res, err := c.SolicitarCAEConReproceso(context.Background(), testToken, 20111111112, solicitudFacturaC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.CAE != "76123456789012" || !res.Reproceso {
		t.Errorf("expected recovered CAE with Reproceso, got %+v", res)
	}
}

func TestSolicitarCAEConReproceso_DoesNotReuseForeignCAE(t *testing.T) {
	// 10016 + the number exists but belongs to a different comprobante:
	// its CAE must NOT be returned.
	srv := newActionServer(t, map[string]string{
		actionCAESolicitar:  envelope(solicitar10016),
		actionCompConsultar: envelope(strings.Replace(consultarOK, "<DocNro>30111222</DocNro>", "<DocNro>27222333</DocNro>", 1)),
	}, nil)
	c := testClient(srv.URL)

	res, err := c.SolicitarCAEConReproceso(context.Background(), testToken, 20111111112, solicitudFacturaC())
	if res != nil {
		t.Fatalf("expected no result, got %+v", res)
	}
	if !errors.Is(err, ErrComprobanteNoCoincide) {
		t.Fatalf("expected ErrComprobanteNoCoincide, got %v", err)
	}
}

func TestSolicitarCAEConReproceso_NotRegisteredKeeps10016(t *testing.T) {
	srv := newActionServer(t, map[string]string{
		actionCAESolicitar:  envelope(solicitar10016),
		actionCompConsultar: envelope(consultar602),
	}, nil)
	c := testClient(srv.URL)

	_, err := c.SolicitarCAEConReproceso(context.Background(), testToken, 20111111112, solicitudFacturaC())
	afipErr, ok := afipkit.IsAFIPError(err)
	if !ok || !afipErr.IsAlreadyRegistered() {
		t.Fatalf("expected original 10016 AFIPError, got %v", err)
	}
	if afipkit.IsIndeterminate(err) {
		t.Error("10016 without a registered comprobante is a determinate rejection")
	}
}

func TestSolicitarCAE_IndeterminateOutcomes(t *testing.T) {
	t.Run("http 500", func(t *testing.T) {
		srv := newActionServer(t, map[string]string{}, nil)
		_, err := testClient(srv.URL).SolicitarCAE(context.Background(), testToken, 20111111112, solicitudFacturaC())
		if !afipkit.IsIndeterminate(err) {
			t.Errorf("expected indeterminate, got %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := testClient(srv.URL).SolicitarCAE(ctx, testToken, 20111111112, solicitudFacturaC())
		if !afipkit.IsIndeterminate(err) {
			t.Errorf("expected indeterminate, got %v", err)
		}
	})

	t.Run("soap fault", func(t *testing.T) {
		srv := newActionServer(t, map[string]string{actionCAESolicitar: envelope(
			`<soap:Fault><faultcode>soap:Server</faultcode><faultstring>Server was unable to process request.</faultstring></soap:Fault>`)}, nil)
		_, err := testClient(srv.URL).SolicitarCAE(context.Background(), testToken, 20111111112, solicitudFacturaC())
		if !afipkit.IsIndeterminate(err) {
			t.Errorf("expected indeterminate, got %v", err)
		}
	})

	t.Run("empty FeDetResp", func(t *testing.T) {
		srv := newActionServer(t, map[string]string{actionCAESolicitar: envelope(
			`<FECAESolicitarResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FECAESolicitarResult><FeCabResp><Resultado>A</Resultado></FeCabResp></FECAESolicitarResult></FECAESolicitarResponse>`)}, nil)
		_, err := testClient(srv.URL).SolicitarCAE(context.Background(), testToken, 20111111112, solicitudFacturaC())
		if !afipkit.IsIndeterminate(err) {
			t.Errorf("expected indeterminate, got %v", err)
		}
	})

	t.Run("afip internal error 501", func(t *testing.T) {
		srv := newActionServer(t, map[string]string{actionCAESolicitar: envelope(
			`<FECAESolicitarResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FECAESolicitarResult><Errors><Err><Code>501</Code><Msg>Error interno de base de datos</Msg></Err></Errors></FECAESolicitarResult></FECAESolicitarResponse>`)}, nil)
		_, err := testClient(srv.URL).SolicitarCAE(context.Background(), testToken, 20111111112, solicitudFacturaC())
		if !afipkit.IsIndeterminate(err) {
			t.Errorf("expected indeterminate, got %v", err)
		}
	})
}

func TestSolicitarCAE_DeterminateOutcomes(t *testing.T) {
	t.Run("rejected with observations", func(t *testing.T) {
		srv := newActionServer(t, map[string]string{actionCAESolicitar: envelope(solicitar10016)}, nil)
		res, err := testClient(srv.URL).SolicitarCAE(context.Background(), testToken, 20111111112, solicitudFacturaC())
		if err == nil || afipkit.IsIndeterminate(err) {
			t.Errorf("expected determinate rejection, got %v", err)
		}
		if res == nil || res.Resultado != "R" {
			t.Errorf("expected Resultado R, got %+v", res)
		}
	})

	t.Run("header business error", func(t *testing.T) {
		srv := newActionServer(t, map[string]string{actionCAESolicitar: envelope(
			`<FECAESolicitarResponse xmlns="http://ar.gov.afip.dif.FEV1/"><FECAESolicitarResult><Errors><Err><Code>600</Code><Msg>ValidacionDeToken: No validaron las fechas del token</Msg></Err></Errors></FECAESolicitarResult></FECAESolicitarResponse>`)}, nil)
		_, err := testClient(srv.URL).SolicitarCAE(context.Background(), testToken, 20111111112, solicitudFacturaC())
		if err == nil || afipkit.IsIndeterminate(err) {
			t.Errorf("expected determinate error, got %v", err)
		}
	})

	t.Run("local validation, nothing sent", func(t *testing.T) {
		var n int32
		srv := newActionServer(t, map[string]string{}, map[string]*int32{actionCAESolicitar: &n})
		bad := solicitudFacturaC()
		bad.FeCAEReq.FeDetReq[0].ImpTotal = "1"
		_, err := testClient(srv.URL).SolicitarCAE(context.Background(), testToken, 20111111112, bad)
		if err == nil || afipkit.IsIndeterminate(err) {
			t.Errorf("expected determinate validation error, got %v", err)
		}
		if atomic.LoadInt32(&n) != 0 {
			t.Error("validation error must not reach AFIP")
		}
	})
}
