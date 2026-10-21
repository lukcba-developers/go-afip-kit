# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Go library for Argentina's AFIP (ARCA) electronic invoicing web services. Covers WSAA authentication, WSFEv1 invoicing, certificate management, and fiscal rules. Pure library — no framework, no database, no CLI.

The repository is public. The library is a pass-through: each consuming app provides its own certificate, key, CUIT, punto de venta and stores. Never commit certificates, keys, real CUITs, personal data or names of consuming projects; tests use generated certificates and fictitious CUITs.

## Requirements

- **Go 1.26.10+** (minimum version without known standard-library vulnerabilities).

## Commands

```bash
# Run all tests with race detector
go test -race ./...

# Run tests for a specific package
go test ./soap/
go test ./wsaa/
go test ./wsfev1/
go test ./fiscal/
go test ./cert/
go test ./qr/

# Run a single test
go test ./fiscal/ -run TestValidarCuadratura

# Build (verify compilation)
go build ./...

# Vet
go vet ./...

# Vulnerability scan
govulncheck ./...
```

## Architecture

The module is `github.com/lukcba-developers/go-afip-kit`. Only external dependency is `go.mozilla.org/pkcs7` (v0.10.0, for CMS signing).

**Package layering (bottom-up):**

1. **`soap/`** — Minimal SOAP 1.1 HTTP client. Handles envelope wrapping/unwrapping, fault detection. No WSDL — callers provide typed request/response structs via `xml` tags. All AFIP communication goes through `soap.Client.Call()`. The `SOAPAction` header is always sent (`""` for empty actions, as required by Axis/SOAP 1.1). Redirects are disabled to avoid leaking credentials. Configurable retries (`soap.WithRetry`) and max payload size cap (10 MiB).

2. **`wsaa/`** — WSAA authentication (LoginCms). Signs a TRA (Ticket de Requerimiento de Acceso) with PKCS#7/CMS, sends it to WSAA, gets back a `TokenAcceso` (token + sign + expiration). The TRA `uniqueId` is a 32-bit uint (`xs:unsignedInt` per schema) and monotonic within the process. The TRA is signed with SHA-256 and falls back to SHA-1 on `cms.sign.invalid` (the WSAA spec documents SHA1+RSA). `CachedClient` wraps `Client` with a `TokenStore` interface for automatic token caching/renewal per environment (`wsaa:{homo|prod}:{cuit}:{service}`). Login coalescing is per process; with several replicas, `WithLocker` (a `Locker`, e.g. Redis `SET NX PX`) coalesces across processes. On AFIP error 600 use `Renew`/`CallWithAuthRetry` — never delete the stored token blindly. Under delegation, `CachedClient` uses the platform's CUIT while `wsfev1` methods receive the represented taxpayer's CUIT. WSAA fault helpers live in `wsaa/faults.go`.

3. **`wsfev1/`** — WSFEv1 electronic invoicing client. `SolicitarCAE` emits comprobantes (validated locally first with `Validate()`). `SolicitarCAEConReproceso` adds idempotent retry: on error 10016 it calls `Recuperar`, which queries `FECompConsultar` and returns the CAE only if the registered comprobante matches the one sent (otherwise `ErrComprobanteNoCoincide`). `Recuperar` is also the way to confirm a comprobante after an indeterminate failure (`afipkit.IsIndeterminate`). Also exposes parameter queries (tipos de comprobante, IVA, monedas, puntos de venta, cotización, condiciones IVA del receptor per RG 5616).

4. **`cert/`** — Certificate utilities: PEM parsing (PKCS#1 and PKCS#8), validation (`Validate`, `ValidateCertOnly`, expiry, CUIT extraction, TLS pairing), `CUITDelCertificado` (extracts CUIT even from expired certs), and `KeyVault` for AES-256-GCM encryption of private keys at rest with Authenticated Additional Data (`EncryptWithAAD` / `DecryptWithAAD` for multi-tenant isolation).

5. **`fiscal/`** — Argentine fiscal rules: `DeterminarTipoComprobante` (RI→RI=A, RI→Mono=A per RG 5003, RI→CF=B, Mono/Exento→C), receptor identification (`DeterminarDocumento` with dynamic identification threshold), receptor IVA condition per comprobante class (RG 5616), mandatory RG 5003 legend helpers (`LeyendaRG5003`, `RequiereLeyendaRG5003`), amount validation (`ValidarCuadratura` uses integer centavo arithmetic to avoid float issues), IVA calculation (`CalcularIVA`), and AFIP date format helpers (YYYYMMDD).

6. **`qr/`** — Mandatory AFIP QR code URL generator (RG 4892 / RG 5614/24). Encodes the required JSON payload into base64. CAE is parsed to numeric `codAut` with `qr.ParseCAE`.

7. **Root package (`afipkit`)** — Constants (comprobante types, document types, IVA codes, receptor IVA conditions `CondIVA*`, currencies, environments), normalization (`NormalizarCUIT`, `ValidarCUIT` using mod-11), and shared error types (`AFIPError`, `ErrSOAP`, `ErrWSAAAuth`, `ErrCertificate`, `ErrIncompleteResponse`) plus error helpers (`IsIndeterminate`, `IsTokenRechazado`, `IsCUITNoRepresentado`, `IsPuntoVentaInvalido`, `IsCondicionIVAReceptorInvalida`).

**Key patterns:**
- All SOAP types use `encoding/xml` struct tags matching AFIP's WSDL exactly
- Monetary amounts in WSFEv1 requests are `string` (not float) for XML precision
- Errors from AFIP are parsed into `AFIPError` with code-checking helpers (`IsAlreadyRegistered`, `IsInvalidPtoVta`, etc.)
- Tests use `httptest.Server` with canned XML responses — no live AFIP calls
- Functional options pattern (`soap.WithTimeout`, `wsaa.WithRenewalMargin`, `wsaa.WithLocker`)
- All public API methods take `context.Context` as first parameter
- Environment switching: `production bool` flag on clients selects homo vs prod URLs

## Domain Notes

- AFIP dates are YYYYMMDD strings, not `time.Time`
- Comprobante = invoice/receipt. CAE = authorization code returned by AFIP
- Error 10016 = "CbteDesde/CbteFch is not the next to authorize" (per the ARCA manual), NOT "already registered". It may mean a previous attempt was authorized — confirm with `Recuperar` and never reuse a CAE without checking the data matches
- Error 600 = WSAA token rejected by business service; retry with `CallWithAuthRetry`
- Error 601 = CUIT not represented by token; check delegation in Administrador de Relaciones
- Error 602 = FECompConsultar found no record
- `CondicionIVA` determines invoice letter: RI→RI = A, RI→Monotributista = A (RG 5003), RI→consumer = B, Monotributo = C
- WSAA tokens are cached per environment: key `wsaa:{homo|prod}:{cuit}:{service}`
- Homologacion = AFIP's testing/sandbox environment
