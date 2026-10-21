# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Go library for Argentina's AFIP (ARCA) electronic invoicing web services. Covers WSAA authentication, WSFEv1 invoicing, certificate management, and fiscal rules. Pure library — no framework, no database, no CLI.

The repository is public. The library is a pass-through: each consuming app provides its own certificate, key, CUIT, punto de venta and stores. Never commit certificates, keys, real CUITs, personal data or names of consuming projects; tests use generated certificates and fictitious CUITs.

## Commands

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./soap/
go test ./wsaa/
go test ./wsfev1/
go test ./fiscal/
go test ./cert/

# Run a single test
go test ./fiscal/ -run TestValidarCuadratura

# Build (verify compilation)
go build ./...

# Vet
go vet ./...
```

## Architecture

The module is `github.com/lukcba-developers/go-afip-kit`. Only external dependency is `go.mozilla.org/pkcs7` (for CMS signing).

**Package layering (bottom-up):**

1. **`soap/`** — Minimal SOAP 1.1 HTTP client. Handles envelope wrapping/unwrapping, fault detection. No WSDL — callers provide typed request/response structs via `xml` tags. All AFIP communication goes through `soap.Client.Call()`.

2. **`wsaa/`** — WSAA authentication (LoginCms). Signs a TRA (Ticket de Requerimiento de Acceso) with PKCS#7/CMS, sends it to WSAA, gets back a `TokenAcceso` (token + sign + expiration). `CachedClient` wraps `Client` with a `TokenStore` interface for automatic token caching/renewal. `MemoryStore` is the built-in in-memory implementation; users implement `TokenStore` for Redis etc. Login coalescing is per process; with several replicas, `WithLocker` (a `Locker`, e.g. Redis `SET NX PX`) coalesces across processes. On AFIP error 600 use `Renew`/`CallWithAuthRetry` — never delete the stored token blindly. The TRA is signed with SHA-256 and falls back to SHA-1 on `cms.sign.invalid` (the WSAA spec documents SHA1+RSA). WSAA fault helpers live in `wsaa/faults.go`.

3. **`wsfev1/`** — WSFEv1 electronic invoicing client. `SolicitarCAE` emits comprobantes, `SolicitarCAEConReproceso` adds idempotent retry: on error 10016 it calls `Recuperar`, which queries `FECompConsultar` and returns the CAE only if the registered comprobante matches the one sent (otherwise `ErrComprobanteNoCoincide`). `Recuperar` is also the way to confirm a comprobante after an indeterminate failure (`afipkit.IsIndeterminate`). Also exposes parameter queries (tipos de comprobante, IVA, monedas, puntos de venta, cotizacion).

4. **`cert/`** — Certificate utilities: PEM parsing (PKCS#1 and PKCS#8), validation (expiry, CUIT extraction, TLS pairing), and `KeyVault` for AES-256-GCM encryption of private keys at rest.

5. **`fiscal/`** — Argentine fiscal rules: `DeterminarTipoComprobante` (RI→RI=A, RI→Mono=A per RG 5003, RI→CF=B, Mono/Exento→C), receptor identification (`DeterminarDocumento`), receptor IVA condition per comprobante class (RG 5616), amount validation (`ValidarCuadratura` uses integer centavo arithmetic to avoid float issues), IVA calculation, AFIP date format helpers (YYYYMMDD).

6. **Root package (`afipkit`)** — Constants (comprobante types, document types, IVA codes, receptor IVA conditions `CondIVA*`, currencies, environments) and shared error types (`AFIPError` with well-known codes like 10016 and 602, `ErrSOAP`, `ErrWSAAAuth`, `ErrCertificate`, `ErrIncompleteResponse`) plus `IsIndeterminate`.

**Key patterns:**
- All SOAP types use `encoding/xml` struct tags matching AFIP's WSDL exactly
- Monetary amounts in WSFEv1 requests are `string` (not float) for XML precision
- Errors from AFIP are parsed into `AFIPError` with code-checking helpers (`IsAlreadyRegistered`, `IsInvalidPtoVta`, etc.)
- Tests use `httptest.Server` with canned XML responses — no live AFIP calls
- Functional options pattern (`soap.WithTimeout`, `wsaa.WithRenewalMargin`)
- All public API methods take `context.Context` as first parameter
- Environment switching: `production bool` flag on clients selects homo vs prod URLs

## Domain Notes

- AFIP dates are YYYYMMDD strings, not `time.Time`
- Comprobante = invoice/receipt. CAE = authorization code returned by AFIP
- Error 10016 = "CbteDesde/CbteFch is not the next to authorize" (per the ARCA manual), NOT "already registered". It may mean a previous attempt was authorized — confirm with `Recuperar` and never reuse a CAE without checking the data matches
- Error 602 = FECompConsultar found no record
- `CondicionIVA` determines invoice letter: RI→RI = A, RI→Monotributista = A (RG 5003), RI→consumer = B, Monotributo = C
- WSAA tokens are cached per environment: key `wsaa:{homo|prod}:{cuit}:{service}`
- Homologacion = AFIP's testing/sandbox environment
