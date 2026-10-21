# Guía para contribuir a go-afip-kit

Esta guía describe los estándares técnicos, de seguridad y arquitectura para colaborar en `go-afip-kit`.

> Documentación relacionada: [Arquitectura](architecture.md) · [Guía de uso](usage.md) · [README](../README.md) · [Changelog](../CHANGELOG.md)

---

## Requisitos de desarrollo

- **Go 1.26.10+** (la versión mínima requerida para evitar vulnerabilidades de la biblioteca estándar).
- `govulncheck` instalado (`go install golang.org/x/vuln/cmd/govulncheck@latest`).

---

## Privacidad y seguridad del repositorio

> [!IMPORTANT]
> **El repositorio es público.** La librería opera como passthrough puro: cada aplicación consumidora es responsable de proveer sus propios certificados, claves privadas, CUITs y almacenamiento de tokens.
>
> **Bajo ninguna circunstancia se deben commitear:**
> - Certificados digitales (`.crt`, `.cer`, `.pem`, `.p12`, `.pfx`) ni claves privadas (`.key`).
> - CUITs reales de personas físicas o jurídicas (los tests deben usar CUITs ficticios válidos con algoritmo módulo 11, como `20123456786` o `30111111118`).
> - Nombres de proyectos, empresas o aplicaciones privadas en código, tests, commits o documentación.
> - Variables de entorno reales o archivos `.env*`.

---

## Cómo agregar un nuevo web service de AFIP

La librería está diseñada para que extender nuevos servicios (ej. `wsfex` para facturas de exportación, o `wscpe` para cartas de porte) sea mecánico y consistente.

### Paso 1: Crear el paquete

```
go-afip-kit/
└── wsfex/                     ← nuevo paquete
    ├── client.go              ← cliente y métodos públicos
    ├── types_request.go       ← structs XML de solicitud
    ├── types_response.go      ← structs XML de respuesta
    └── client_test.go         ← tests con httptest.Server
```

### Paso 2: Definir types (request / response)

Seguir el patrón de `wsfev1/types_request.go`:

```go
package wsfex

import "encoding/xml"

type FEXAuthorizeRequest struct {
    XMLName xml.Name `xml:"FEXAuthorize"`
    XMLNS   string   `xml:"xmlns,attr"`
    Auth    Auth     `xml:"ClsFEXAuthRequest"`
    Cmp     CmpData  `xml:"Cmp"`
}
```

**Reglas obligatorias para structs XML:**
1. Los nombres de tags y campos XML deben coincidir **exactamente** con el WSDL y manual oficial de AFIP.
2. Importes monetarios siempre como `string` (nunca `float64`), para evitar pérdida de precisión en serialización.
3. Fechas en formato `string` (usualmente `YYYYMMDD` o `YYYYMMDDHHmmss` según el manual).
4. Usar `omitempty` únicamente para campos opcionales del esquema.
5. El campo `XMLNS` debe contener el namespace XML del servicio.

### Paso 3: Implementar el Client

Seguir el patrón de `wsfev1/client.go`:

```go
package wsfex

import (
    "context"

    afipkit "github.com/lukcba-developers/go-afip-kit"
    "github.com/lukcba-developers/go-afip-kit/soap"
    "github.com/lukcba-developers/go-afip-kit/wsaa"
)

const (
    homoURL = "https://wswhomo.afip.gov.ar/wsfexv1/service.asmx"
    prodURL = "https://servicios1.afip.gov.ar/wsfexv1/service.asmx"
    wsfexNS = "http://ar.gov.afip.dif.fexv1/"
)

type Client struct {
    soap       *soap.Client
    production bool
    baseURL    string // para tests
}

func NewClient(production bool, opts ...soap.Option) *Client {
    return &Client{
        soap:       soap.NewClient(opts...),
        production: production,
    }
}

func (c *Client) url() string {
    if c.baseURL != "" {
        return c.baseURL
    }
    if c.production {
        return prodURL
    }
    return homoURL
}
```

**Flujo en cada método público:**
1. Validar parámetros requeridos localmente.
2. Armar el request con las credenciales de `Auth` (`Token`, `Sign`, `Cuit`).
3. Ejecutar `c.soap.Call(ctx, c.url(), soapAction, &req, &resp)`. En SOAP 1.1, `soap.Client` envía siempre el header `SOAPAction`; si no se requiere acción específica se envía como `""`.
4. Mapear errores de AFIP retornando `*afipkit.AFIPError` o `afipkit.ErrIncompleteResponse`.
5. Retornar el resultado fuertemente tipado.

### Paso 4: Mapear errores a los tipos estándar

Reutilizar `*afipkit.AFIPError` y `*afipkit.ErrSOAP`:

```go
func errorsToAFIP(ea *ErrorsArray) *afipkit.AFIPError {
    if ea == nil || len(ea.Err) == 0 {
        return nil
    }
    details := make([]afipkit.AFIPErrorDetail, len(ea.Err))
    for i, e := range ea.Err {
        details[i] = afipkit.AFIPErrorDetail{Code: e.Code, Msg: e.Msg}
    }
    return afipkit.NewAFIPError(details)
}
```

### Paso 5: Agregar constantes

Si el servicio introduce nuevos tipos de comprobante o conceptos, agregarlos a `afip.go`:

```go
const CbteFacturaE = 19
```

### Paso 6: Tests con httptest.Server

Todos los tests unitarios deben ejecutarse de forma aislada y determinista, sin depender de la conectividad real con AFIP:

```go
func TestFEXAuthorize_Approved(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verificar que SOAPAction esté presente
        if r.Header.Get("SOAPAction") == "" {
            http.Error(w, "missing SOAPAction", http.StatusInternalServerError)
            return
        }
        w.Header().Set("Content-Type", "text/xml; charset=utf-8")
        _, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <FEXAuthorizeResponse xmlns="http://ar.gov.afip.dif.fexv1/">
      <!-- ... -->
    </FEXAuthorizeResponse>
  </soap:Body>
</soap:Envelope>`))
    }))
    defer server.Close()

    // Configurar el cliente con la URL del servidor falso
}
```

---

## Agregar reglas fiscales

Nuevas resoluciones generales y reglas impositivas van en el paquete `fiscal/`:
1. **Modelado y tipos:** `fiscal/types.go`
2. **Determinación de tipos de comprobantes y documentos:** `fiscal/rules.go`
3. **Receptores, identificaciones y regímenes especiales:** `fiscal/receptor.go`
4. **Validaciones numéricas y fechas:** `fiscal/validation.go`
5. **Aritmética:** Usar siempre centavos enteros (`int64`) para cálculos monetarios.

---

## Checklist para Pull Requests

Antes de enviar un PR, verificá que todos los puntos se cumplan:

- [ ] `go build ./...` compila limpiamente.
- [ ] `go vet ./...` no reporta advertencias.
- [ ] `go test -race ./...` pasa al 100% en todas las suites.
- [ ] `govulncheck ./...` no reporta vulnerabilidades conocidas alcanzables.
- [ ] Sin dependencias externas adicionales innecesarias.
- [ ] Las funciones y tipos públicos poseen comentarios explicativos en formato GoDoc.
- [ ] Los importes monetarios en structs XML usan `string`, nunca `float64`.
- [ ] No existen variables mutables a nivel de paquete ni singletons con estado global.
- [ ] Todos los CUITs en tests y fixtures son ficticios y pasan la validación de `afipkit.ValidarCUIT`.
- [ ] No se incluyen claves privadas, certificados reales ni datos sensibles en los commits.

---

## Referencia de correspondencia con pyafipws

El referente estándar comunitario para servicios web de AFIP es `reingart/pyafipws` (Python). Al diseñar o extender servicios en `go-afip-kit`, podés consultar la correspondencia:

| Componente pyafipws | go-afip-kit | Estado |
|---|---|---|
| `wsaa.py` | `wsaa/` | Implementado |
| `wsfev1.py` | `wsfev1/` | Implementado |
| `pyqr.py` | `qr/` | Implementado |
| `wsfexv1.py` | `wsfex/` | Planificado |
| `wsmtxca.py` | `wsmtxca/` | Planificado |
| `wsfecred.py` | `wsfecred/` | Planificado |
| `wscpe.py` | `wscpe/` | Planificado |
| `padron.py` | `padron/` | Planificado |
