# go-afip-kit

Go client library for Argentina's **AFIP (ARCA)** electronic invoicing web services.

Covers the full invoicing lifecycle: certificate management, WSAA authentication, WSFEv1 electronic invoicing, and Argentine fiscal rules — all in a standalone library with no framework or database dependencies.

```bash
go get github.com/lukcba-developers/go-afip-kit@v0.4.0
```

> Upgrading from v0.2.x? See [CHANGELOG.md](CHANGELOG.md) — v0.3.0 has breaking changes
> (`CachedClient.Invalidate`, token cache keys, RI→Monotributista is now Factura A).

## Features

- **WSAA Authentication** — CMS/PKCS#7 signed login with automatic token caching and renewal, cached separately per environment (homologacion/produccion). Concurrent logins for the same CUIT+service are coalesced into a single LoginCms (WSAA rejects duplicates with `coe.alreadyAuthenticated`)
- **WSFEv1 Electronic Invoicing** — Emit invoices (Facturas A/B/C), credit notes, debit notes, and obtain CAE codes
- **Idempotent Retries** — `afipkit.IsIndeterminate` tells "AFIP may have authorized it" apart from a rejection; `wsfev1.Recuperar` confirms a comprobante after an outage (verifying it matches what was sent); `SolicitarCAEConReproceso` recovers the CAE on error 10016 only when the registered comprobante matches. Plus opt-in transport retries with exponential backoff (`soap.WithRetry`)
- **Local Request Validation** — `FECAESolicitarRequest.Validate()` catches cuadratura mismatches (error 10048), missing service dates, and malformed amounts before spending an AFIP round-trip
- **QR Codes (RG 4892)** — `qr.URL()` builds the mandatory AFIP QR payload with spec-correct types (numeric `codAut`)
- **Certificate Management** — PEM parsing (PKCS#1/PKCS#8), validation, expiry checks, CUIT extraction, and AES-256-GCM encryption at rest
- **Fiscal Rules** — Automatic comprobante type determination (A/B/C, RG 5003), receptor identification (DNI, consumidor final, identification threshold), receptor IVA condition (RG 5616), amount validation with integer centavo arithmetic, IVA calculation, CUIT validation
- **Homologacion & Production** — Single `production bool` flag switches between sandbox and live environments

## Packages

| Package | Description |
|---------|-------------|
| `afipkit` (root) | Constants (comprobante types, IVA codes, currencies, documents) and shared error types |
| `soap/` | Minimal SOAP 1.1 client — envelope wrapping, fault detection, opt-in retries, no WSDL |
| `wsaa/` | WSAA authentication — `Client`, `CachedClient`, `TokenStore` interface, `IsAlreadyAuthenticated` |
| `wsfev1/` | WSFEv1 invoicing — `SolicitarCAE`, `Recuperar`, `ConsultarComprobante`, request validation, parameter queries |
| `cert/` | Certificate parsing, validation, and `KeyVault` for encrypted private key storage |
| `fiscal/` | Argentine fiscal rules — comprobante determination, amount validation, IVA calculation |
| `qr/` | AFIP QR code URL builder (RG 4892) for printed/PDF comprobantes |

## Quick Start

### Emit an Invoice

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    afipkit "github.com/lukcba-developers/go-afip-kit"
    "github.com/lukcba-developers/go-afip-kit/cert"
    "github.com/lukcba-developers/go-afip-kit/fiscal"
    "github.com/lukcba-developers/go-afip-kit/wsaa"
    "github.com/lukcba-developers/go-afip-kit/wsfev1"
)

func main() {
    ctx := context.Background()
    production := false // homologacion

    // 1. Load and validate certificate
    certPEM, _ := os.ReadFile("mi_certificado.crt")
    keyPEM, _ := os.ReadFile("mi_clave.key")

    result, err := cert.Validate(certPEM, keyPEM, "20123456786")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Certificate valid, expires in %d days\n", result.DaysUntilExpiry)

    // 2. Parse cert + key
    x509Cert, rsaKey, err := cert.ParseCertAndKey(certPEM, keyPEM)
    if err != nil {
        log.Fatal(err)
    }

    // 3. Authenticate with WSAA (cached)
    wsaaClient := wsaa.NewClient()
    cachedAuth := wsaa.NewCachedClient(wsaaClient, wsaa.NewMemoryStore())

    token, err := cachedAuth.Authenticate(ctx, x509Cert, rsaKey,
        20123456786, "wsfe", production)
    if err != nil {
        log.Fatal(err)
    }

    // 4. Create WSFEv1 client and check AFIP health
    wsfe := wsfev1.NewClient(production)

    health, err := wsfe.HealthCheck(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if !health.IsOK() {
        log.Fatalf("AFIP unavailable: %+v", health)
    }

    // 5. Get last authorized comprobante number
    cuit := int64(20123456786)
    ptoVta := 1
    tipoCbte := afipkit.CbteFacturaA

    ultimo, err := wsfe.UltimoAutorizado(ctx, token, cuit, ptoVta, tipoCbte)
    if err != nil {
        log.Fatal(err)
    }
    siguiente := ultimo + 1

    // 6. Build and submit the invoice
    solicitud := &wsfev1.FECAESolicitarRequest{
        FeCAEReq: wsfev1.FeCAEReq{
            FeCabReq: wsfev1.FeCabReq{
                CantReg:  1,
                PtoVta:   ptoVta,
                CbteTipo: tipoCbte,
            },
            FeDetReq: []wsfev1.FECAEDetRequest{{
                Concepto:     afipkit.ConceptoServicios,
                DocTipo:      afipkit.DocCUIT,
                DocNro:       30123456789,
                CbteDesde:    siguiente,
                CbteHasta:    siguiente,
                CbteFch:      fiscal.FormatFechaAFIP(time.Now()),
                ImpTotal:     "121000.00",
                ImpTotConc:   "0",
                ImpNeto:      "100000.00",
                ImpOpEx:      "0",
                ImpIVA:       "21000.00",
                ImpTrib:      "0",
                FchServDesde: "20260301",
                FchServHasta: "20260331",
                FchVtoPago:   "20260418",
                MonId:        afipkit.MonedaPesos,
                MonCotiz:     "1",
                // RG 5616: condicion IVA del receptor (obligatoria)
                CondicionIVAReceptorId: afipkit.CondIVAResponsableInscripto,
                Iva: &wsfev1.IvaArray{
                    AlicIva: []wsfev1.AlicIvaItem{{
                        Id:      afipkit.IVA21,
                        BaseImp: "100000.00",
                        Importe: "21000.00",
                    }},
                },
            }},
        },
    }

    // SolicitarCAEConReproceso recovers the CAE on error 10016 when the
    // comprobante was already authorized with the same data.
    caeResult, err := wsfe.SolicitarCAEConReproceso(ctx, token, cuit, solicitud)
    if afipkit.IsIndeterminate(err) {
        // Unknown outcome (timeout, network, AFIP internal error): do NOT mark
        // it as rejected. Keep it pending and confirm later with wsfe.Recuperar.
        log.Fatalf("pendiente de confirmacion: %v", err)
    }
    if err != nil {
        log.Fatal(err) // rejected by AFIP or invalid request
    }

    fmt.Printf("CAE: %s (expires: %s)\n", caeResult.CAE, caeResult.CAEFchVto)
    if caeResult.Reproceso {
        fmt.Println("(recovered via reprocessing)")
    }
}
```

## QR Codes (RG 4892)

Every printed/PDF comprobante must include an AFIP QR. Build the URL and render it with any QR library:

```go
import "github.com/lukcba-developers/go-afip-kit/qr"

codAut, _ := qr.ParseCAE(result.CAE) // CAE string → numeric (spec requires a number)
url, err := qr.URL(qr.Comprobante{
    Fecha:      "2026-06-11", // YYYY-MM-DD
    CUIT:       20111111112,  // emisor
    PtoVta:     3,
    TipoCmp:    afipkit.CbteFacturaB,
    NroCmp:     1234,
    Importe:    12100.00,
    TipoDocRec: afipkit.DocCUIT,
    NroDocRec:  20222222223,
    CodAut:     codAut,
})
// url = "https://www.afip.gob.ar/fe/qr/?p=<base64 JSON>"
```

Defaults are applied for `Ver` (1), `TipoCodAut` ("E"), `Moneda` ("PES") and `Ctz` (1 for PES).

## Transport Retries

AFIP's services fail transiently (timeouts, 5xx). Enable retries with exponential backoff:

```go
client := wsfev1.NewClient(production,
    soap.WithTimeout(30*time.Second),
    soap.WithRetry(2, time.Second),      // 2 extra attempts: +1s, +2s
    soap.WithLogger(slog.Default()),     // optional: log each retry
)
```

SOAP faults and 4xx responses are never retried — only network errors and 5xx without a fault. For `FECAESolicitar`, always pair retries with `SolicitarCAEConReproceso`: if the original request did reach AFIP, the retry gets error 10016 and the CAE is recovered automatically (after checking that the registered comprobante matches the one sent).

## Recovering After an Outage

When `FECAESolicitar` times out or the connection drops, AFIP may or may not
have authorized the comprobante. `afipkit.IsIndeterminate(err)` tells the two
cases apart:

| `IsIndeterminate` | Errors | Meaning |
|---|---|---|
| `true` | `*afipkit.ErrSOAP` (network, timeout, HTTP error, SOAP fault, malformed XML), `context.DeadlineExceeded` / `Canceled`, `afipkit.ErrIncompleteResponse`, `*AFIPError` 500/501/502 | Outcome unknown — keep it pending |
| `false` | `*AFIPError` with business codes (header `Errors` or `Observaciones` with Resultado `R`), local validation errors, `wsfev1.ErrComprobanteNoCoincide`, `nil` | AFIP answered (or nothing was sent) |

Confirm a pending comprobante with `Recuperar`:

```go
res, found, err := wsfe.Recuperar(ctx, token, cuit, ptoVta, tipoCbte, nro, &detalleEnviado)
switch {
case errors.Is(err, wsfev1.ErrComprobanteNoCoincide):
    // The number is registered with OTHER data (emitted outside the system).
    // Never reuse its CAE — needs manual review.
case err != nil:
    // Could not check (afipkit.IsIndeterminate(err) → try again later).
case found:
    // Authorized: res.CAE, res.CAEFchVto
default:
    // Not registered (AFIP error 602): emit again with the same number.
}
```

`Recuperar` compares `DocTipo`, `DocNro`, `ImpTotal` (to the centavo) and
`CbteFch` (when it was sent). `UltimoAutorizado` returns the last number
authorized for a punto de venta + tipo.

> AFIP error **10016** means "the number or date is not the next one to
> authorize" — not "already registered". It shows up when a previous attempt
> was authorized, but also with a skipped number or an out-of-range date.

## Fiscal Rules

```go
// Determine comprobante type from IVA conditions
tipoCbte, _ := fiscal.DeterminarTipoComprobante(
    fiscal.ResponsableInscripto, // emisor (seller)
    fiscal.ResponsableInscripto, // receptor (buyer)
)
// tipoCbte == afipkit.CbteFacturaA (1)

// RI -> RI                        = Factura A
// RI -> Mono                      = Factura A (RG 5003, needs fiscal.LeyendaRG5003)
// RI -> CF/Exento/NoResp          = Factura B
// Mono/Exento -> any              = Factura C

if fiscal.RequiereLeyendaRG5003(tipoCbte, afipkit.CondIVAMonotributo) {
    pdf.AddText(fiscal.LeyendaRG5003)
}
```

### Receptor Identification

```go
// Validates and normalizes DocTipo/DocNro. Class A/M needs a valid CUIT;
// B/C accepts CUIT/CUIL, DNI (96) or an unidentified consumidor final (99, 0)
// below the threshold. The threshold is set by ARCA (RG 5700/2025: $10.000.000)
// and is passed in, never hardcoded.
docTipo, docNro, err := fiscal.DeterminarDocumento(
    afipkit.CbteFacturaC, afipkit.DocSinIdentificar, 0, 15000.00, umbralIdentificacion)
if errors.Is(err, fiscal.ErrReceptorNoIdentificado) {
    // ask the buyer for CUIT/CUIL/DNI
}
```

### Receptor IVA Condition (RG 5616)

```go
// CondicionIVAReceptorId codes: afipkit.CondIVAResponsableInscripto (1),
// CondIVAExento (4), CondIVAConsumidorFinal (5), CondIVAMonotributo (6), ...
err := fiscal.ValidarCondicionIVAReceptor(afipkit.CbteFacturaA, afipkit.CondIVAConsumidorFinal)
// error: Consumidor Final cannot receive Factura A

id, _ := fiscal.Monotributista.CondicionIVAReceptorID() // 6
permitidas := fiscal.CondicionesIVAReceptorPermitidas("B") // [4 5 7 8 9 10 15]
```

### Amount Validation

```go
// Validate that ImpTotal == sum of all parts (uses integer centavo math)
err := fiscal.ValidarCuadratura(fiscal.Importes{
    ImpNeto:  100000.00,
    ImpIVA:   21000.00,
    ImpTotal: 121000.00,
})

// Calculate IVA from base amount
iva, _ := fiscal.CalcularIVA(100000.00, afipkit.IVA21)
// iva == 21000.00

// Validate IVA breakdown matches totals
err = fiscal.ValidarAlicuotasIVA(
    []fiscal.AlicuotaIVA{{ID: afipkit.IVA21, BaseImp: 100000, Importe: 21000}},
    100000.00, // impNeto
    21000.00,  // impIVA
)
```

### Dates

```go
fecha := fiscal.FormatFechaAFIP(time.Now())     // "20260321"
parsed, _ := fiscal.ParseFechaAFIP("20260321")  // time.Time
err := fiscal.ValidarFechaAFIP("20260321")       // nil (valid)
```

## Credit Notes

```go
// Get credit note type for an invoice type
tipoNC, _ := fiscal.DeterminarTipoNC(afipkit.CbteFacturaA)
// tipoNC == afipkit.CbteNotaCreditoA (3)

// Reference the original comprobante
solicitud := &wsfev1.FECAESolicitarRequest{
    FeCAEReq: wsfev1.FeCAEReq{
        FeCabReq: wsfev1.FeCabReq{CantReg: 1, PtoVta: 1, CbteTipo: tipoNC},
        FeDetReq: []wsfev1.FECAEDetRequest{{
            // ... same fields as invoice ...
            CbtesAsoc: &wsfev1.CbtesAsocArray{
                CbteAsoc: []wsfev1.CbteAsocItem{{
                    Tipo:    afipkit.CbteFacturaA,
                    PtoVta:  1,
                    Nro:     42,
                    Cuit:    20123456786,
                    CbteFch: "20260318",
                }},
            },
        }},
    },
}
```

## Certificate Management

### Parsing & Validation

```go
// Parse certificate and private key from PEM
x509Cert, rsaKey, err := cert.ParseCertAndKey(certPEM, keyPEM)

// Full validation: expiry, CUIT match, key pairing
result, err := cert.Validate(certPEM, keyPEM, "20123456786")
fmt.Printf("Expires: %s (%d days)\n", result.NotAfter, result.DaysUntilExpiry)
fmt.Printf("Fingerprint: %s\n", result.Fingerprint)

// Certificate-only validation (no private key needed)
result, err := cert.ValidateCertOnly(certPEM, "20123456786")
```

### Encrypt Private Keys at Rest

```go
// Create vault with 32-byte master key (64 hex chars)
vault, err := cert.NewKeyVault(os.Getenv("AFIP_MASTER_KEY"))

// Encrypt for database storage, bound to its owner (AAD): a ciphertext copied
// to another tenant's row fails to decrypt.
encrypted, _ := vault.EncryptToBase64WithAAD(keyPEM, []byte(cuit))

// Decrypt when needed, with the same owner
decrypted, _ := vault.DecryptFromBase64WithAAD(encrypted, []byte(cuit))

// EncryptToBase64 / DecryptFromBase64 (no AAD) keep working for existing data.
```

## Token Caching

Tokens are cached under `wsaa:{homo|prod}:{cuit}:{service}`, so homologacion
and produccion never share a token. `Invalidate(ctx, cuit, service, production)`
drops the token of one environment only.

The library includes `MemoryStore` for single-instance deployments. For multi-instance services, implement the `TokenStore` interface:

```go
type TokenStore interface {
    Get(ctx context.Context, key string) (*wsaa.TokenAcceso, error)
    Set(ctx context.Context, key string, token *wsaa.TokenAcceso) error
    Delete(ctx context.Context, key string) error
}
```

### Redis Example

```go
type RedisTokenStore struct {
    client *redis.Client
}

func (r *RedisTokenStore) Get(ctx context.Context, key string) (*wsaa.TokenAcceso, error) {
    data, err := r.client.Get(ctx, key).Bytes()
    if err == redis.Nil {
        return nil, nil // not found
    }
    if err != nil {
        return nil, err
    }
    var token wsaa.TokenAcceso
    _ = json.Unmarshal(data, &token)
    return &token, nil
}

func (r *RedisTokenStore) Set(ctx context.Context, key string, token *wsaa.TokenAcceso) error {
    data, _ := json.Marshal(token)
    ttl := time.Until(token.Expiration) - 30*time.Minute
    return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *RedisTokenStore) Delete(ctx context.Context, key string) error {
    return r.client.Del(ctx, key).Err()
}

// Usage
store := &RedisTokenStore{client: redisClient}
cached := wsaa.NewCachedClient(wsaaClient, store)
```

### Several Replicas: Distributed Lock

`CachedClient` coalesces concurrent logins only **within a process**. With
several replicas sharing the store, two of them can miss the cache at the same
time, both call LoginCms, and WSAA rejects the second with
`coe.alreadyAuthenticated`. Add a `wsaa.Locker` so only one replica logs in and
the others reuse its token:

```go
var unlockScript = redis.NewScript(
    `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`)

type RedisLocker struct{ client *redis.Client }

func (r *RedisLocker) TryLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
    return r.client.SetNX(ctx, key, owner, ttl).Result()
}

func (r *RedisLocker) Unlock(ctx context.Context, key, owner string) error {
    return unlockScript.Run(ctx, r.client, []string{key}, owner).Err()
}

cached := wsaa.NewCachedClient(wsaaClient, store,
    wsaa.WithLocker(&RedisLocker{client: redisClient}),
    wsaa.WithLockTTL(60*time.Second), // optional; must exceed the LoginCms duration
)
```

Lock keys are the token key plus `:lock` (e.g. `wsaa:prod:20123456786:wsfe:lock`).
Waiting replicas poll the store until the token appears, the lock expires (the
holder died), or their context ends. `wsaa.NewMemoryLocker()` is an in-process
implementation for tests. Independently of the lock, if LoginCms answers
`coe.alreadyAuthenticated` and the store already has a valid token (stored by
another process), `Authenticate` returns that token.

> **Protect the store.** Token and sign are bearer credentials valid for up to
> 12 hours for that CUIT and service. Use a Redis with AUTH and TLS (or a
> private network), or encrypt the value in your `TokenStore` (e.g. with
> `cert.KeyVault`).

> **Use a persistent store in production.** WSAA refuses to issue a new TA while a
> previous one is still valid (fault `coe.alreadyAuthenticated`). With `MemoryStore`,
> a process restart loses the token but AFIP still considers it active — authentication
> then fails for up to 12 hours. A Redis- or database-backed `TokenStore` survives
> restarts and avoids this entirely. Detect the condition with
> `wsaa.IsAlreadyAuthenticated(err)`.

## Error Handling

AFIP errors are typed and inspectable:

```go
caeResult, err := wsfe.SolicitarCAE(ctx, token, cuit, solicitud)
if err != nil {
    if afipErr, ok := afipkit.IsAFIPError(err); ok {
        // AFIP business error
        fmt.Println("Code:", afipErr.FirstCode())

        if afipErr.IsAlreadyRegistered() {
            // Error 10016: number/date is not the next to authorize.
            // May mean it was already authorized: confirm with wsfe.Recuperar
        }
        if afipErr.IsInvalidPtoVta() {
            // Error 10005: punto de venta not configured
        }
        if afipErr.IsAmountMismatch() {
            // Error 10048: ImpTotal != sum of parts
        }
    }

    // Infrastructure errors
    var soapErr *afipkit.ErrSOAP
    if errors.As(err, &soapErr) {
        // Network/timeout/malformed XML
    }

    var authErr *afipkit.ErrWSAAAuth
    if errors.As(err, &authErr) {
        // Authentication failed
    }
}
```

## Constants Reference

### Comprobante Types

| Constant | Code | Description |
|----------|------|-------------|
| `CbteFacturaA` | 1 | Factura A |
| `CbteNotaDebitoA` | 2 | Nota de Debito A |
| `CbteNotaCreditoA` | 3 | Nota de Credito A |
| `CbteFacturaB` | 6 | Factura B |
| `CbteNotaCreditoB` | 8 | Nota de Credito B |
| `CbteFacturaC` | 11 | Factura C |
| `CbteFacturaE` | 19 | Factura E (Exportacion) |
| `CbteFCEFacturaA` | 201 | FCE MiPyME Factura A |

### IVA Rates

| Constant | Code | Rate |
|----------|------|------|
| `IVANoGravado` | 1 | No gravado |
| `IVAExento` | 2 | Exento |
| `IVA0` | 3 | 0% |
| `IVA25` | 9 | 2.5% |
| `IVA5` | 8 | 5% |
| `IVA105` | 4 | 10.5% |
| `IVA21` | 5 | 21% |
| `IVA27` | 6 | 27% |

### Document Types

| Constant | Code | Description |
|----------|------|-------------|
| `DocCUIT` | 80 | CUIT |
| `DocCUIL` | 86 | CUIL |
| `DocDNI` | 96 | DNI |
| `DocSinIdentificar` | 99 | Sin identificar |

### Receptor IVA Condition (RG 5616)

| Constant | Code | Admitted classes |
|----------|------|------------------|
| `CondIVAResponsableInscripto` | 1 | A, M, C |
| `CondIVAExento` | 4 | B, C |
| `CondIVAConsumidorFinal` | 5 | B, C |
| `CondIVAMonotributo` | 6 | A, M, C |
| `CondIVANoCategorizado` | 7 | B, C |
| `CondIVAProveedorExterior` | 8 | B, C |
| `CondIVAClienteExterior` | 9 | B, C |
| `CondIVALiberadoLey19640` | 10 | B, C |
| `CondIVAMonotributistaSocial` | 13 | A, M, C |
| `CondIVANoAlcanzado` | 15 | B, C |
| `CondIVAMonotributoTIP` | 16 | A, M, C |

### Currencies

| Constant | Code |
|----------|------|
| `MonedaPesos` | PES |
| `MonedaDolar` | DOL |
| `MonedaEuro` | 060 |

## WSFEv1 Parameter Queries

```go
tipos, _ := wsfe.TiposComprobante(ctx, token, cuit)
ivas, _ := wsfe.AlicuotasIVA(ctx, token, cuit)
ptos, _ := wsfe.PuntosVenta(ctx, token, cuit)
docs, _ := wsfe.TiposDocumento(ctx, token, cuit)
monedas, _ := wsfe.TiposMonedas(ctx, token, cuit)
cotiz, _ := wsfe.Cotizacion(ctx, token, cuit, afipkit.MonedaDolar)
maxReg, _ := wsfe.MaxComprobantesPerRequest(ctx, token, cuit)
conds, _ := wsfe.CondicionesIVAReceptor(ctx, token, cuit, "C") // RG 5616
```

## Requirements

- Go 1.26.9+ (the minimum patch without known standard-library vulnerabilities)
- AFIP certificate (.crt) and private key (.key) for the CUIT to operate
- Punto de venta configured in AFIP for electronic invoicing

## License

MIT
