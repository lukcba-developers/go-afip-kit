# Arquitectura de go-afip-kit

> Documentación relacionada: [Guía de uso](usage.md) · [Guía para contribuir](contributing.md) · [README](../README.md) · [Changelog](../CHANGELOG.md)

## Visión general

`go-afip-kit` es una librería Go independiente y modular para interactuar con los servicios web de AFIP/ARCA (Argentina). No tiene dependencias de frameworks web, bases de datos ni ORMs — únicamente la biblioteca estándar de Go más `go.mozilla.org/pkcs7` para la firma criptográfica CMS/PKCS#7.

```
go-afip-kit/
├── afip.go            Constantes AFIP (comprobantes, IVA, docs, monedas), NormalizarCUIT, ValidarCUIT
├── errors.go          Errores tipados (AFIPError, ErrSOAP, ErrWSAAAuth, ErrCertificate, IsIndeterminate, helpers)
│
├── soap/              Cliente SOAP 1.1 genérico
│   ├── client.go      SOAP Client con functional options, retries exponenciales, límite de payload y SOAPAction
│   └── envelope.go    Marshal/unmarshal de SOAP Envelopes
│
├── wsaa/              Autenticación WSAA (firma CMS/PKCS#7)
│   ├── client.go      Client con fallback automático de SHA-256 a SHA-1 y uniqueId de 32 bits
│   ├── cms.go         Firma CMS con go.mozilla.org/pkcs7
│   ├── cached_client.go  CachedClient con coalescencia intra-proceso, Renew y CallWithAuthRetry
│   ├── locker.go      Interfaz Locker para lock distribuido entre réplicas y MemoryLocker
│   ├── token_store.go Interfaz TokenStore y MemoryStore con RWMutex
│   ├── faults.go      Taxonomía de faults WSAA (FaultCode, IsAlreadyAuthenticated, IsNotAuthorized, etc.)
│   └── types.go       TRA (Ticket de Requerimiento de Acceso), TA y TokenAcceso
│
├── wsfev1/            Facturación electrónica nacional WSFEv1
│   ├── client.go      Métodos WSFEv1: SolicitarCAE, SolicitarCAEConReproceso, Recuperar, ConsultarComprobante, FEParamGet*
│   ├── validate.go    Validación local previa de FECAESolicitarRequest (cuadratura, fechas, alícuotas)
│   ├── types_request.go   Structs XML de request
│   └── types_response.go  Structs XML de response
│
├── cert/              Certificados X.509 y cifrado de claves
│   ├── parser.go      ParseCertificatePEM, ParsePrivateKeyPEM (soporte PKCS#1 y PKCS#8)
│   ├── validator.go   Validate, ValidateCertOnly, CUITDelCertificado (incluso si expiró)
│   └── vault.go       KeyVault AES-256-GCM con soporte de Authenticated Additional Data (AAD)
│
├── fiscal/            Reglas fiscales argentinas
│   ├── types.go       CondicionIVA (enum con IsValid, Slug, Parse)
│   ├── rules.go       DeterminarTipoComprobante (RG 5003), DeterminarTipoNC/ND, DeterminarTipoDocumento
│   ├── receptor.go    DeterminarDocumento (umbral consumidor final), ValidarCondicionIVAReceptor (RG 5616), LeyendaRG5003
│   └── validation.go  Cuadratura de importes (aritmética de centavos), cálculo de IVA, validación de fechas YYYYMMDD
│
├── qr/                Código QR oficial de AFIP (RG 4892)
│   └── qr.go          Constructor de URL con payload JSON en base64 y ParseCAE
│
└── docs/              Documentación de arquitectura, uso y contribución
```

---

## Principios de diseño

### 1. Paquetes independientes y desacoplados

Cada subpaquete posee una responsabilidad única y delimitada:
- Un proyecto que solo requiera autenticación ante AFIP para otros servicios importa `go-afip-kit/wsaa` sin arrastrar `wsfev1` ni `fiscal`.
- La capa de transporte `soap` no conoce los esquemas de negocio; solo serializa structs Go a XML y detecta faults SOAP.
- Las reglas de negocio en `fiscal` y `qr` son funciones puras sin dependencias de red.

### 2. Interfaces en las fronteras de integración

La librería no impone drivers de base de datos ni clientes de caché:
- `wsaa.TokenStore`: la aplicación implementa el almacenamiento en Redis, PostgreSQL, DynamoDB, etc.
- `wsaa.Locker`: la aplicación implementa la sincronización distribuida (ej. Redis `SET NX PX`, locks consultivos de SQL).
- `soap.Option`: permite inyectar clientes HTTP personalizados, configurar timeouts y habilitar retries.

### 3. Errores tipados con `errors.As` / `errors.Is` y helpers funcionales

Todo error devuelto por la librería conserva su causa subyacente (`Unwrap`):
```go
var afipErr *afipkit.AFIPError
if errors.As(err, &afipErr) {
    if afipErr.IsAlreadyRegistered() { // Error 10016
        // Reproceso o verificación
    }
}
```
Además, se proveen helpers funcionales directos (`afipkit.IsIndeterminate(err)`, `afipkit.IsTokenRechazado(err)`, `afipkit.IsCUITNoRepresentado(err)`, `wsaa.IsNotAuthorized(err)`) que no requieren desempaquetar structs manualmente.

### 4. Precisión monetaria estricta

- Todos los cálculos fiscales internos (`ValidarCuadratura`, `CalcularIVA`, `ValidarAlicuotasIVA`) utilizan **aritmética entera en centavos** (`int64`) para eliminar discrepancias de punto flotante de IEEE 754.
- Los structs de request/response XML usan `string` para importes y alícuotas (ej. `"121000.00"`), preservando la representación decimal requerida por AFIP sin redondeos imprevistos.

### 5. Inmutabilidad y ausencia de estado global

- No existen singletons, variables mutables a nivel de paquete ni funciones `init()` que alteren el comportamiento global.
- Toda configuración y dependencia se inyecta explícitamente vía constructores (`NewClient`, `NewCachedClient`, `NewKeyVault`).

---

## Dependencias

| Dependencia | Versión | Propósito |
|---|---|---|
| `go.mozilla.org/pkcs7` | `v0.10.0` | Firma digital CMS/PKCS#7 para los tickets de requerimiento de acceso (TRA) de WSAA |

**Requerimiento de Go:** **Go 1.26.10+**. Las versiones anteriores a 1.26.10 contienen vulnerabilidades conocidas en la biblioteca estándar (`crypto/x509`, `net/http`).

Todo lo demás proviene de la biblioteca estándar de Go:
- `encoding/xml` — serialización y deserialización SOAP.
- `crypto/x509`, `crypto/rsa` — parseo y verificación de certificados y claves RSA.
- `crypto/aes`, `crypto/cipher` — cifrado autenticado AES-256-GCM.
- `crypto/sha256` — fingerprints de certificados.
- `net/http` — transporte HTTP seguro sin redirecciones automáticas.

---

## Flujo de datos y arquitectura de capas

```
                    ┌─────────────────────────────────────────┐
                    │           Tu aplicación                 │
                    │      (servicio backend / worker)        │
                    └──────────┬──────────────────────────────┘
                               │
          ┌────────────────────┼──────────────────────────────┐
          │                    │                              │
    ┌─────▼─────┐       ┌─────▼──────────┐             ┌─────▼─────┐
    │   cert/   │       │     wsaa/      │             │  wsfev1/  │
    │ Validar   │       │ CachedClient   │             │ Emitir    │
    │ Encriptar │       │ (TokenStore +  │             │ Consultar │
    │ (con AAD) │       │    Locker)     │             │ Recuperar │
    └───────────┘       └─────┬──────────┘             └─────┬─────┘
                              │ (TokenAcceso:                │
                              │  token + sign)               │
                              └──────────────┬───────────────┘
                                             │
                                       ┌─────▼─────┐
                                       │   soap/   │
                                       │ Client    │
                                       │ Retries   │
                                       └─────┬─────┘
                                             │ HTTPS POST (XML)
                                  ┌──────────▼──────────┐
                                  │   AFIP / ARCA       │
                                  │   WSAA + WSFEv1     │
                                  └─────────────────────┘
```

---

## Arquitectura de autenticación (WSAA)

WSAA opera con tickets de acceso firmados digitalmente (CMS/PKCS#7). La arquitectura de `wsaa` aborda los siguientes desafíos técnicos:

1. **Algoritmo de firma y fallback dinámico:**
   - La especificación histórica de WSAA documenta SHA1+RSA, mientras que entornos modernos admiten SHA-256.
   - `wsaa.Client` firma los TRA con **SHA-256**. Si el servidor WSAA rechaza la firma con el fault `cms.sign.invalid`, el cliente conmuta de forma atómica a **SHA-1**, reintenta inmediatamente y memoriza el algoritmo para requests posteriores.
2. **Identificador único de 32 bits:**
   - Según el schema `loginTicketRequest.xsd`, el campo `uniqueId` del TRA es de tipo `xs:unsignedInt` (32 bits). Un valor de 64 bits (como nanosegundos) genera el fault `xml.bad`.
   - `wsaa` utiliza una secuencia atómica basada en segundos Unix de 32 bits (`atomic.Uint32`), garantizando que dos tickets nunca compartan identificador dentro del mismo proceso ni en reintentos rápidos.
3. **Coalescencia y prevención de `coe.alreadyAuthenticated`:**
   - AFIP prohíbe obtener un nuevo TA si el anterior aún está vigente.
   - `CachedClient` agrupa llamadas concurrentes para una misma clave `(ambiente, CUIT, servicio)`: un solo hilo ejecuta `LoginCms` mientras los demás aguardan.
   - Para despliegues con **múltiples réplicas**, `wsaa.WithLocker` permite conectar un lock distribuido (Redis, DB) para que solo una réplica ejecute `LoginCms` a nivel cluster.
4. **Renovación segura ante error 600:**
   - Cuando WSFEv1 responde con error 600 ("token y firma no se corresponden"), `CachedClient.CallWithAuthRetry` y `Renew` gestionan la renovación sin purgar incondicionalmente el token de la base compartida.
5. **Arquitectura para delegación:**
   - En plataformas donde múltiples contribuyentes delegan su facturación a un CUIT central, el `CachedClient` se parametriza con el CUIT del certificado (el que posee la relación en AFIP), mientras que las llamadas a `wsfev1` envían el CUIT del contribuyente representado.

---

## Concurrencia y Thread Safety

Todos los componentes principales están diseñados para ser utilizados de forma segura y concurrente desde múltiples goroutines:

- `soap.Client`: inmutable tras su construcción; el `http.Client` subyacente es thread-safe.
- `wsaa.Client`: stateless y seguro para uso concurrente.
- `wsaa.CachedClient`: coordina internamente el acceso mediante mutex por clave y lock distribuido opcional (`Locker`).
- `wsaa.MemoryStore`: thread-safe mediante `sync.RWMutex`.
- `wsaa.MemoryLocker`: thread-safe mediante `sync.Mutex`.
- `wsfev1.Client`: stateless y concurrente.
- `cert.KeyVault`: seguro para lectura concurrente (master key inmutable).
- `fiscal.*` y `qr.*`: funciones puras sin estado.

---

## Jerarquía y clasificación de errores

```
error
├── *afipkit.AFIPError                 Errores de negocio de AFIP (Errors y Observaciones)
│   ├── IsAlreadyRegistered()         → Error 10016: número/fecha no es el próximo a autorizar
│   ├── IsInvalidPtoVta()             → Error 10004 / 10005: punto de venta no habilitado o fuera de rango
│   ├── IsAmountMismatch()            → Error 10048: importes no cuadran al centavo
│   ├── IsNotFound()                  → Error 602: comprobante no encontrado en FECompConsultar
│   ├── IsInternal()                  → Errores 500, 501, 502: fallas internas de infraestructura de AFIP
│   └── IsRetryable()                 → Clasifica si el código amerita reintento
│
├── *afipkit.ErrSOAP                   Fallas de transporte SOAP, I/O de red, timeouts o HTTP 5xx
│   └── *soap.Fault                   SOAP Fault devuelto por el servidor (con código y descripción)
│
├── *afipkit.ErrWSAAAuth               Errores originados en el proceso de autenticación de WSAA
├── *afipkit.ErrCertificate            Errores de validación de formato, fechas o emparejamiento de certificados
├── afipkit.ErrIncompleteResponse      AFIP respondió con HTTP 200 pero omitió el resultado utilizable
└── *wsfev1.ComprobanteNoCoincideError (errors.Is wsfev1.ErrComprobanteNoCoincide)
                                       Discrepancia entre el comprobante registrado y el esperado
```

### Clasificación de indeterminación (`afipkit.IsIndeterminate`)

Distingue cuándo el estado de un comprobante es **desconocido** (amerita mantenerlo pendiente y conciliar con `wsfev1.Recuperar`) de cuándo fue un **rechazo definitivo**:

- **Indeterminado (`true`):**
  - Cualquier `*afipkit.ErrSOAP` (corte de conexión, timeout, HTTP 5xx, SOAP fault).
  - Cancelación de contexto (`context.DeadlineExceeded`, `context.Canceled`).
  - `afipkit.ErrIncompleteResponse` (ej. aprobación sin CAE retornado).
  - `*afipkit.AFIPError` con códigos de infraestructura (500, 501, 502).
- **Determinado (`false`):**
  - Comprobante aprobado con CAE (`nil`).
  - Rechazos de negocio de AFIP (resultado "R", error 10016, 10048, 10015, etc.).
  - Errores de validación previa local (`solicitud.Validate()`).
  - `wsfev1.ErrComprobanteNoCoincide`.
