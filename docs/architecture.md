# Arquitectura de go-afip-kit

## Vision general

`go-afip-kit` es una libreria Go independiente para interactuar con los web services de AFIP/ARCA (Argentina). No tiene dependencias de frameworks, bases de datos ni ORMs — solo Go stdlib + `go.mozilla.org/pkcs7`.

```
go-afip-kit/
├── afip.go            Constantes AFIP + helpers (codigos cbte, IVA, doc, moneda, CUIT)
├── errors.go          Errores tipados (AFIPError, ErrSOAP, ErrWSAAAuth, ErrCertificate)
│
├── soap/              Cliente SOAP 1.1 generico
│   ├── client.go      SOAPClient con options, SOAP fault detection
│   └── envelope.go    Marshal/unmarshal de SOAP envelopes
│
├── wsaa/              Autenticacion WSAA (firma CMS/PKCS#7)
│   ├── client.go      WSAAClient.Authenticate()
│   ├── cms.go         Firma CMS con go.mozilla.org/pkcs7
│   ├── cached_client.go  CachedWSAAClient (TokenStore interface)
│   ├── token_store.go    Interface TokenStore + MemoryStore (concurrency-safe)
│   └── types.go       TRA, TA, TokenAcceso
│
├── wsfev1/            Facturacion electronica WSFEv1
│   ├── client.go      Todos los metodos: SolicitarCAE, UltimoAutorizado, etc.
│   ├── types_request.go   Structs XML de request
│   └── types_response.go  Structs XML de response + FEParamGet*
│
├── cert/              Certificados y encriptacion
│   ├── parser.go      ParseCertificatePEM, ParsePrivateKeyPEM (PKCS#1 + PKCS#8)
│   ├── validator.go   Validate(certPEM, keyPEM, CUIT) con fingerprint
│   └── vault.go       KeyVault AES-256-GCM (encrypt/decrypt claves privadas)
│
├── fiscal/            Reglas fiscales argentinas
│   ├── types.go       CondicionIVA (enum con IsValid, Slug, Parse)
│   ├── rules.go       DeterminarTipoComprobante, DeterminarTipoNC/ND, TipoDocumento
│   └── validation.go  Cuadratura de importes, calculo IVA, validacion fechas YYYYMMDD
│
└── docs/              Documentacion
```

## Principios de diseno

### 1. Paquetes separados, no mega-package

Cada sub-paquete tiene una responsabilidad clara. Un proyecto que solo necesite WSAA importa `go-afip-kit/wsaa` sin arrastrar WSFEv1 o fiscal.

### 2. Interfaces en boundaries

- `wsaa.TokenStore` — la app implementa con Redis, DB, etc.
- `soap.Option` — configura timeout, HTTP client

La libreria NO depende de Redis, GORM, ni ningun framework.

### 3. Errores tipados con errors.As/Is

```go
var afipErr *afipkit.AFIPError
if errors.As(err, &afipErr) {
    if afipErr.IsAlreadyRegistered() {
        // reproceso 10016
    }
    if !afipErr.IsRetryable() {
        // error permanente, no reintentar
    }
}

var soapFault *soap.Fault
if errors.As(err, &soapFault) {
    // SOAP-level fault (servicio no disponible, etc.)
}
```

### 4. Precision monetaria

- Los calculos fiscales usan **aritmetica de centavos** (int64) internamente
- Los structs XML usan `string` para importes (no float64) para evitar perdida de precision en serializacion
- La app es responsable de la conversion decimal <-> string

### 5. Sin estado global

- No hay singletons, variables de paquete mutables, ni init()
- Todo se inyecta via constructores

## Dependencias

| Dependencia | Version | Proposito |
|---|---|---|
| `go.mozilla.org/pkcs7` | v0.9.0 | Firma CMS/PKCS#7 para WSAA |

Todo lo demas es Go stdlib:
- `encoding/xml` — serializacion SOAP
- `crypto/x509`, `crypto/rsa` — certificados
- `crypto/aes`, `crypto/cipher` — AES-256-GCM
- `net/http` — transporte HTTP
- `crypto/sha256` — fingerprints

## Flujo de datos

```
                    ┌─────────────────────────────────────────┐
                    │           Tu aplicacion                  │
                    │  (Tramo, u otro proyecto Go)             │
                    └──────────┬──────────────────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                    │
    ┌─────▼─────┐       ┌─────▼─────┐       ┌─────▼─────┐
    │   cert/    │       │   wsaa/   │       │  wsfev1/  │
    │ Validar    │       │ Autenticar│       │ Emitir    │
    │ Encriptar  │       │ Token     │       │ Consultar │
    └────────────┘       └─────┬─────┘       └─────┬─────┘
                               │                    │
                         ┌─────▼─────┐              │
                         │   soap/   │◄─────────────┘
                         │ HTTP+XML  │
                         └─────┬─────┘
                               │
                    ┌──────────▼──────────┐
                    │   AFIP/ARCA         │
                    │   WSAA + WSFEv1     │
                    └─────────────────────┘
```

## Thread safety

- `soap.Client` — safe (stateless, usa `http.Client` que es safe)
- `wsaa.Client` — safe (stateless)
- `wsaa.CachedClient` — safe si el `TokenStore` es safe
- `wsaa.MemoryStore` — safe (usa `sync.RWMutex`)
- `wsfev1.Client` — safe (stateless)
- `cert.KeyVault` — safe (read-only master key)
- `fiscal.*` — todas funciones puras, safe

## Manejo de errores AFIP

### Jerarquia

```
error
├── *afipkit.AFIPError      Errores de AFIP (Errors / Observaciones)
│   ├── IsAlreadyRegistered()  → 10016: verificar con wsfev1.Recuperar
│   ├── IsNotFound()           → 602: FECompConsultar sin datos
│   ├── IsInternal()           → 500/501/502: resultado indeterminado
│   ├── IsRetryable()          → puede reintentar (ej: 10016, 10013)
│   └── !IsRetryable()        → error permanente (ej: 10048, 10005)
│
├── *afipkit.ErrSOAP        Error de transporte SOAP
├── *afipkit.ErrWSAAAuth    Error de autenticacion WSAA
├── *afipkit.ErrCertificate Error de certificado
├── *soap.Fault             SOAP Fault (siempre envuelto en ErrSOAP)
├── afipkit.ErrIncompleteResponse   AFIP respondio sin resultado utilizable
└── *wsfev1.ComprobanteNoCoincideError  (errors.Is ErrComprobanteNoCoincide)
                            el numero esta registrado con otros datos
```

### Resultado indeterminado

`afipkit.IsIndeterminate(err)` devuelve `true` cuando no se sabe si AFIP
autorizo el comprobante: `ErrSOAP` (red, timeout, HTTP, SOAP fault),
`context.DeadlineExceeded`/`Canceled`, `ErrIncompleteResponse` y `AFIPError`
500/501/502. En esos casos el comprobante queda pendiente y se confirma con
`wsfev1.Recuperar`, que compara DocTipo, DocNro, ImpTotal y CbteFch con lo
enviado. Devuelve `false` para rechazos de negocio, errores de validacion local
y `ErrComprobanteNoCoincide`.

### Errores permanentes vs retryables

| Codigo | Nombre | Retryable | Accion |
|--------|--------|-----------|--------|
| 10016 | Numero/fecha no es el proximo a autorizar | Si (idempotente) | Verificar con Recuperar: si coincide, recuperar CAE |
| 10013 | Invalid number | Si | Resincronizar con UltimoAutorizado |
| 10005 | Invalid PV | No | Corregir config |
| 10000 | Unauthorized | No | Verificar certificado/CUIT |
| 10048 | Amount mismatch | No | Corregir calculos |
| 10015 | Receiver not ID | No | Completar datos receptor |
