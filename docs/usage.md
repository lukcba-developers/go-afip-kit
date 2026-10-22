# Guía de uso de go-afip-kit

Guía práctica completa para la integración con los servicios web de facturación electrónica de **AFIP (ARCA)** en Go.

> Documentación relacionada: [Arquitectura](architecture.md) · [Guía para contribuir](contributing.md) · [README](../README.md) · [Changelog](../CHANGELOG.md)

## Instalación y requerimientos

```bash
go get github.com/lukcba-developers/go-afip-kit@v0.6.0
```

- **Go 1.26.10+** (versión mínima sin vulnerabilidades conocidas en la biblioteca estándar).
- Certificado digital X.509 (`.crt`) y clave privada RSA (`.key`) emitidos para el CUIT emisor o representante.
- Punto de venta dado de alta en AFIP de tipo **Web Services (RECE)**.

---

## Ejemplo completo: Emitir una Factura

El siguiente ejemplo cubre el ciclo de vida completo: validación de certificados, autenticación con WSAA (con caché), chequeo de estado de AFIP, consulta de correlativo, validación local y emisión con reproceso automático.

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
    production := false // false = homologación, true = producción

    // 1. Cargar y validar certificado + clave privada
    certPEM, err := os.ReadFile("mi_certificado.crt")
    if err != nil {
        log.Fatal(err)
    }
    keyPEM, err := os.ReadFile("mi_clave.key")
    if err != nil {
        log.Fatal(err)
    }

    cuitEmisor := int64(20123456786)
    valResult, err := cert.Validate(certPEM, keyPEM, fmt.Sprintf("%d", cuitEmisor))
    if err != nil {
        log.Fatalf("Certificado inválido: %v", err)
    }
    fmt.Printf("Certificado válido para CUIT %d (vence en %d días)\n", valResult.CUIT, valResult.DaysUntilExpiry)

    // 2. Parsear certificado y clave RSA
    x509Cert, rsaKey, err := cert.ParseCertAndKey(certPEM, keyPEM)
    if err != nil {
        log.Fatal(err)
    }

    // 3. Autenticación con WSAA (con caché de tokens)
    wsaaClient := wsaa.NewClient()
    cachedAuth := wsaa.NewCachedClient(wsaaClient, wsaa.NewMemoryStore())

    token, err := cachedAuth.Authenticate(ctx, x509Cert, rsaKey, cuitEmisor, "wsfe", production)
    if err != nil {
        log.Fatalf("Error de autenticación WSAA: %v", err)
    }

    // 4. Crear cliente WSFEv1 y verificar disponibilidad de AFIP
    wsfe := wsfev1.NewClient(production)

    health, err := wsfe.HealthCheck(ctx)
    if err != nil {
        log.Fatalf("Error al verificar estado de AFIP: %v", err)
    }
    if !health.IsOK() {
        log.Fatalf("Servicios de AFIP no disponibles: App=%s, DB=%s, Auth=%s",
            health.AppServer, health.DbServer, health.AuthServer)
    }

    // 5. Obtener el próximo número a autorizar
    ptoVta := 1
    tipoCbte := afipkit.CbteFacturaA

    ultimo, err := wsfe.UltimoAutorizado(ctx, token, cuitEmisor, ptoVta, tipoCbte)
    if err != nil {
        log.Fatalf("Error al consultar último autorizado: %v", err)
    }
    siguiente := ultimo + 1

    // 6. Construir la solicitud de CAE
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
                DocNro:       30111111118, // CUIT del receptor (Responsable Inscripto)
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
                // RG 5616: Condición frente al IVA del receptor (obligatoria)
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

    // 7. Emitir comprobante con reproceso automático
    caeResult, err := wsfe.SolicitarCAEConReproceso(ctx, token, cuitEmisor, solicitud)
    if afipkit.IsIndeterminate(err) {
        // Timeout, error de red o error 5xx interno de AFIP: el comprobante puede haberse autorizado.
        // NO marcar como rechazado; mantener en estado pendiente y confirmar con wsfe.Recuperar.
        log.Fatalf("Resultado indeterminado (pendiente de conciliación): %v", err)
    }
    if err != nil {
        log.Fatalf("Rechazo de AFIP o error de validación: %v", err)
    }

    fmt.Printf("Comprobante autorizado exitosamente!\n")
    fmt.Printf("CAE: %s | Vto: %s\n", caeResult.CAE, caeResult.CAEFchVto)
    if caeResult.Reproceso {
        fmt.Println("(Recuperado automáticamente por reproceso tras error 10016)")
    }
}
```

---

## Códigos QR obligatorios (RG 4892)

Todo comprobante impreso o en PDF debe incluir el código QR de AFIP/ARCA según la RG 4892 (actualizada por RG 5614/24). El paquete `qr` genera la URL oficial lista para renderizar con cualquier biblioteca de QR:

```go
import (
    afipkit "github.com/lukcba-developers/go-afip-kit"
    "github.com/lukcba-developers/go-afip-kit/qr"
)

// El CAE se devuelve como string de 14 dígitos, pero la especificación del QR
// exige que codAut sea numérico:
codAut, err := qr.ParseCAE(caeResult.CAE)
if err != nil {
    log.Fatal(err)
}

qrURL, err := qr.URL(qr.Comprobante{
    Fecha:      "2026-06-11", // Formato YYYY-MM-DD
    CUIT:       20123456786,  // CUIT del emisor
    PtoVta:     1,
    TipoCmp:    afipkit.CbteFacturaA,
    NroCmp:     siguiente,
    Importe:    121000.00,
    TipoDocRec: afipkit.DocCUIT,
    NroDocRec:  30111111118,
    CodAut:     codAut,
})
if err != nil {
    log.Fatal(err)
}

// qrURL = "https://www.afip.gob.ar/fe/qr/?p=<base64 JSON>"
```

Los siguientes valores se completan con sus defaults reglamentarios si se omiten:
- `Ver`: `1`
- `TipoCodAut`: `"E"` (CAE)
- `Moneda`: `"PES"`
- `Ctz`: `1` (para pesos)
- `TipoDocRec` / `NroDocRec`: se omiten automáticamente del JSON si se pasan en `0` (consumidor final sin identificar).

---

## Reintentos de transporte y configuración SOAP

Las conexiones con AFIP presentan cortes y timeouts transitorios. `soap.NewClient` permite configurar timeouts, reintentos con backoff exponencial, tamaño máximo de respuesta y logging:

```go
import (
    "log/slog"
    "time"

    "github.com/lukcba-developers/go-afip-kit/soap"
    "github.com/lukcba-developers/go-afip-kit/wsfev1"
)

client := wsfev1.NewClient(production,
    soap.WithTimeout(30*time.Second),
    soap.WithRetry(2, 1*time.Second),       // 2 intentos adicionales: espera +1s, +2s
    soap.WithMaxResponseSize(10 << 20),      // tope de seguridad: 10 MiB (default)
    soap.WithLogger(slog.Default()),         // loguea reintentos no fatales
)
```

**Reglas de reintento de transporte:**
- Solo se reintentan errores de red (I/O, timeouts) y HTTP 5xx que no contengan un SOAP Fault.
- Errores HTTP 4xx y SOAP Faults **nunca** se reintentan.
- El cliente SOAP por defecto **no sigue redirecciones HTTP** (301/302/307/308) para evitar que las credenciales (token y sign en el body XML) se envíen a un host no autorizado.
- Si usas reintentos de transporte con `FECAESolicitar`, utiliza siempre `SolicitarCAEConReproceso`: si el primer intento llegó a AFIP pero la respuesta se perdió, el reintento recibirá el error 10016 y recuperará el CAE automáticamente si los datos coinciden.

---

## Recuperación tras cortes o timeouts

Cuando un request a `FECAESolicitar` falla por timeout o error de red, no se sabe si AFIP autorizó el comprobante antes del corte. La función `afipkit.IsIndeterminate(err)` categoriza el resultado:

| `IsIndeterminate(err)` | Tipo de error | Significado y acción |
|---|---|---|
| `true` | `*afipkit.ErrSOAP` (red, timeout, 5xx), `context.DeadlineExceeded`, `context.Canceled`, `afipkit.ErrIncompleteResponse`, `*AFIPError` (500, 501, 502) | **Resultado desconocido:** el comprobante debe quedar en estado "pendiente" y verificarse más tarde con `wsfe.Recuperar`. Nunca rechazar ni descartar. |
| `false` | `*AFIPError` de negocio (10016, 10048, 10015, etc.), validación local (`Validate()`), `wsfev1.ErrComprobanteNoCoincide`, `nil` | **Resultado definitivo:** AFIP respondió rechazando la solicitud o el error ocurrió antes de enviar el paquete. |

### Confirmar un comprobante pendiente con `Recuperar`

`Recuperar` consulta `FECompConsultar` y compara los campos esenciales (`DocTipo`, `DocNro`, `ImpTotal` al centavo y `CbteFch` si fue enviada) contra el detalle enviado:

```go
res, found, err := wsfe.Recuperar(ctx, token, cuit, ptoVta, tipoCbte, nro, &detalleEnviado)
switch {
case errors.Is(err, wsfev1.ErrComprobanteNoCoincide):
    // El número está autorizado en AFIP pero con OTROS datos (p. ej. emitido desde el portal de AFIP).
    // Jamás reutilizar su CAE; requiere revisión manual de discrepancias.
    var noCoincide *wsfev1.ComprobanteNoCoincideError
    if errors.As(err, &noCoincide) {
        fmt.Println("Diferencias encontradas:", noCoincide.Diferencias)
    }

case afipkit.IsIndeterminate(err):
    // AFIP sigue sin responder o continúa caído; reintentar más tarde.

case err != nil:
    // Otro error de AFIP al consultar.

case found:
    // Estaba autorizado: res.CAE, res.CAEFchVto disponibles.

default:
    // No existe en AFIP (error 602): es seguro reintentar la emisión con el mismo número.
}
```

### Qué persistir antes de llamar a AFIP

Para poder reconciliar un comprobante ante una falla imprevista, guardá en tu base de datos antes de llamar a `SolicitarCAE`:
- Ambiente (`homo` / `prod`)
- CUIT emisor
- `PtoVta`, `CbteTipo`, número (`CbteDesde`)
- El JSON completo de `FECAEDetRequest`:

```go
// Serialización estable garantizada por tests
rawDetalle, _ := json.Marshal(solicitud.FeCAEReq.FeDetReq[0])
// ... persistir con estado "enviando" ...

// Para recuperar posteriormente:
var detalle wsfev1.FECAEDetRequest
_ = json.Unmarshal(rawDetalle, &detalle)
caeRes, found, err := wsfe.Recuperar(ctx, token, cuit, ptoVta, tipoCbte, nro, &detalle)
```

> **Nota sobre el error 10016:** En el manual de ARCA, el error 10016 significa *"el número o fecha no corresponde al próximo a autorizar"*, no "ya registrado". Puede surgir porque el comprobante anterior se autorizó, pero también si se omitió un número o la fecha está fuera del rango permitido.

---

## Token Caching y tolerancia a fallos en WSAA

Los tokens de acceso de WSAA tienen una validez de hasta 12 horas. AFIP prohíbe emitir un nuevo ticket mientras haya uno vigente, respondiendo con el fault `coe.alreadyAuthenticated`.

El `CachedClient` gestiona la vigencia del token y lo renueva automáticamente 30 minutos antes de su expiración:

```go
wsaaClient := wsaa.NewClient()
store := wsaa.NewMemoryStore() // Para tests o una única instancia
cached := wsaa.NewCachedClient(wsaaClient, store,
    wsaa.WithRenewalMargin(30*time.Minute),
)
```

Las claves se almacenan bajo el formato `wsaa:{homo|prod}:{cuit}:{service}:{sha256-del-certificado}`: cada entorno y cada certificado tienen su propia entrada, y un token solo se devuelve al certificado que lo obtuvo, aunque varios certificados (por ejemplo, uno por tenant) compartan el store.

### Implementación distribuida con Redis (`TokenStore`)

Para arquitecturas con múltiples instancias o contenedores, implementá la interfaz `wsaa.TokenStore`:

```go
type RedisTokenStore struct {
    client *redis.Client
}

func (r *RedisTokenStore) Get(ctx context.Context, key string) (*wsaa.TokenAcceso, error) {
    data, err := r.client.Get(ctx, key).Bytes()
    if err == redis.Nil {
        return nil, nil // No encontrado
    }
    if err != nil {
        return nil, err
    }
    var token wsaa.TokenAcceso
    if err := json.Unmarshal(data, &token); err != nil {
        return nil, err
    }
    return &token, nil
}

func (r *RedisTokenStore) Set(ctx context.Context, key string, token *wsaa.TokenAcceso) error {
    data, _ := json.Marshal(token)
    // El TTL en Redis se calcula con margen de seguridad
    ttl := time.Until(token.Expiration) - 30*time.Minute
    return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *RedisTokenStore) Delete(ctx context.Context, key string) error {
    return r.client.Del(ctx, key).Err()
}
```

### Múltiples réplicas: Lock distribuido (`wsaa.Locker`)

Si varias réplicas consultan el store ante un arranque simultáneo, ambas sufrirán un miss de caché y llamarán a `LoginCms` a la vez. WSAA rechazará a la segunda con `coe.alreadyAuthenticated`.

Configurando un `wsaa.Locker`, una sola réplica ejecutará `LoginCms` y las demás esperarán y reutilizarán el token obtenido:

```go
var unlockScript = redis.NewScript(`
    if redis.call("GET", KEYS[1]) == ARGV[1] then
        return redis.call("DEL", KEYS[1])
    end
    return 0
`)

type RedisLocker struct {
    client *redis.Client
}

func (r *RedisLocker) TryLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
    return r.client.SetNX(ctx, key, owner, ttl).Result()
}

func (r *RedisLocker) Unlock(ctx context.Context, key, owner string) error {
    return unlockScript.Run(ctx, r.client, []string{key}, owner).Err()
}

// Configuración en el cliente:
cached := wsaa.NewCachedClient(wsaaClient, store,
    wsaa.WithLocker(&RedisLocker{client: redisClient}),
    wsaa.WithLockTTL(60*time.Second), // Debe superar la duración máxima de LoginCms
)
```

La clave del lock es la clave del token más `:lock` (ej. `wsaa:prod:20123456786:wsfe:<sha256-del-certificado>:lock`). Si no se usa un lock distribuido, `wsaa.NewMemoryLocker()` permite coordinar goroutines dentro del mismo proceso.

### Renovación ante Token Rechazado (Error 600)

Si WSFEv1 responde con el error 600 (*"no se corresponden token y firma"*), **nunca borres el token de la base con `Invalidate`**: si otra réplica lo acaba de renovar, borrarías su token y el siguiente login fallará con `coe.alreadyAuthenticated`.

Usá `CallWithAuthRetry`, que renueva atómicamente el token con `Renew` solo si falló y reintenta una sola vez:

```go
var caeResult *wsfev1.CAEResult
err := cached.CallWithAuthRetry(ctx, x509Cert, rsaKey, cuit, "wsfe", production, func(tok *wsaa.TokenAcceso) error {
    var err error
    caeResult, err = wsfe.SolicitarCAEConReproceso(ctx, tok, cuit, solicitud)
    return err
})
```

`Renew` verifica si el store ya tiene un token nuevo emitido por otra réplica; si no, ejecuta `LoginCms` y solo sobreescribe el caché en caso de éxito.

### Facturación por cuenta de terceros (Delegación)

Cuando un contribuyente te autoriza su servicio `wsfe` mediante el **Administrador de Relaciones** de AFIP:
- Autenticás ante WSAA con **tu** certificado y **tu** CUIT (CUIT de la plataforma / representante).
- Envías el **CUIT representado** como parámetro a las operaciones de `wsfev1`.

```go
cuitPlataforma := int64(20123456786)   // CUIT del certificado
cuitContribuyente := int64(30111111118) // CUIT del emisor real de la factura

err := cached.CallWithAuthRetry(ctx, x509Cert, rsaKey, cuitPlataforma, "wsfe", production, func(tok *wsaa.TokenAcceso) error {
    var err error
    // Se envía tok (emitido a cuitPlataforma) pero con el CUIT del contribuyente:
    caeResult, err = wsfe.SolicitarCAEConReproceso(ctx, tok, cuitContribuyente, solicitud)
    return err
})

if afipkit.IsCUITNoRepresentado(err) {
    // Error 601: la delegación no existe o se creó después de que se generó el TA.
    // Como el TA sigue siendo válido para AFIP, renovar no resolverá el problema;
    // se debe aguardar a que expire el TA actual o verificar la delegación en AFIP.
}
```

Un único TA sirve para todos los CUITs representados por esa plataforma.

---

## Gestión y cifrado de certificados

### Validación e inspección

```go
// 1. Validación integral de par certificado y clave privada
valResult, err := cert.Validate(certPEM, keyPEM, "20-12345678-6")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Vence: %s (%d días restantes)\n", valResult.NotAfter, valResult.DaysUntilExpiry)
fmt.Printf("Fingerprint SHA-256: %s\n", valResult.Fingerprint)

// 2. Validación de certificado sin clave privada (ej. al subir a un dashboard)
valCert, err := cert.ValidateCertOnly(certPEM, "20-12345678-6")

// 3. Extracción de CUIT sin validar vigencia (útil para auditoría o certificados vencidos)
cuit, err := cert.CUITDelCertificado(certPEM)

// 4. Normalización estricta de CUIT (limpia guiones, valida 11 dígitos y dígito verificador mod-11)
cuitNormalizado, err := afipkit.NormalizarCUIT("20-12345678-6") // 20123456786
```

### Cifrado de claves privadas en reposo (`KeyVault` con AAD)

Para almacenar claves privadas de forma segura en bases de datos multi-tenant, usá `KeyVault` con AES-256-GCM y **Authenticated Additional Data (AAD)**. El AAD vincula el texto cifrado al CUIT del contribuyente; si el registro es copiado a la fila de otro cliente, el descifrado fallará:

```go
// Master key de 32 bytes (64 caracteres hexadecimales)
vault, err := cert.NewKeyVault(os.Getenv("AFIP_MASTER_KEY"))

// Cifrar con AAD vinculado al CUIT del contribuyente
aad := []byte(fmt.Sprintf("%d", cuitContribuyente))
ciphertextBase64, err := vault.EncryptToBase64WithAAD(keyPEM, aad)

// Descifrar exigiendo el mismo AAD
decryptedKeyPEM, err := vault.DecryptFromBase64WithAAD(ciphertextBase64, aad)
```

---

## Reglas fiscales argentinas

### Determinación automática del tipo de comprobante

```go
tipoCbte, err := fiscal.DeterminarTipoComprobante(
    fiscal.ResponsableInscripto, // Emisor
    fiscal.Monotributista,       // Receptor
)
// tipoCbte == afipkit.CbteFacturaA (RG 5003)

// Matriz de reglas:
// RI -> RI               = Factura A (1)
// RI -> Monotributista   = Factura A (1) [Requiere Leyenda RG 5003]
// RI -> Consumidor Final = Factura B (6)
// RI -> Exento / No Resp = Factura B (6)
// Monotributo -> Cualquier receptor = Factura C (11)
// Exento -> Cualquier receptor      = Factura C (11)

// Comprobación de leyenda obligatoria RG 5003 (Ley 27.618):
if fiscal.RequiereLeyendaRG5003(tipoCbte, afipkit.CondIVAMonotributo) {
    fmt.Println(fiscal.LeyendaRG5003)
}
```

### Identificación del receptor y umbrales

```go
// DeterminarDocumento normaliza y valida DocTipo y DocNro según la clase y el importe.
// El umbral para Consumidor Final sin identificar lo establece ARCA (RG 5700: $10.000.000)
// y se pasa como parámetro:
umbralIdentificacion := 10000000.00

docTipo, docNro, err := fiscal.DeterminarDocumento(
    afipkit.CbteFacturaB,
    afipkit.DocSinIdentificar,
    0,
    150000.00, // Total comprobante
    umbralIdentificacion,
)
if errors.Is(err, fiscal.ErrReceptorNoIdentificado) {
    // El importe supera el umbral: se debe solicitar CUIT, CUIL o DNI al comprador
}
```

### Validación de condición frente al IVA del receptor (RG 5616)

```go
// Validar que la condición sea admitida para la clase de comprobante:
err := fiscal.ValidarCondicionIVAReceptor(afipkit.CbteFacturaA, afipkit.CondIVAConsumidorFinal)
// Error: Consumidor Final no puede recibir Factura A

permitidas := fiscal.CondicionesIVAReceptorPermitidas("B")
// [CondIVAExento, CondIVAConsumidorFinal, CondIVANoCategorizado, ...]

// Obtener código de RG 5616 a partir del enum:
codigo, err := fiscal.ResponsableInscripto.CondicionIVAReceptorID() // 1
```

### Validación de cuadratura y cálculo de IVA

AFIP rechaza estrictamente los comprobantes cuyos importes no cuadran al centavo (error 10048). `go-afip-kit` utiliza aritmética entera de centavos:

```go
// 1. Validar cuadratura general
err := fiscal.ValidarCuadratura(fiscal.Importes{
    ImpNeto:      100000.00,
    ImpIVA:       21000.00,
    ImpTributos:  0,
    ImpNoGravado: 0,
    ImpExento:    0,
    ImpTotal:     121000.00,
})

// 2. Calcular IVA según código de alícuota AFIP
iva, err := fiscal.CalcularIVA(100000.00, afipkit.IVA21) // 21000.00

// 3. Validar desglose de alícuotas contra los totales
err = fiscal.ValidarAlicuotasIVA([]fiscal.AlicuotaIVA{
    {ID: afipkit.IVA21, BaseImp: 100000.00, Importe: 21000.00},
}, 100000.00, 21000.00)
```

---

## Notas de Crédito y Débito

Para emitir una Nota de Crédito o Débito, se determina el tipo correspondiente y se referencian los comprobantes originales en el array `CbtesAsoc`:

```go
tipoNC, err := fiscal.DeterminarTipoNC(afipkit.CbteFacturaA)
// tipoNC == afipkit.CbteNotaCreditoA (3)

tipoND, err := fiscal.DeterminarTipoND(afipkit.CbteFacturaA)
// tipoND == afipkit.CbteNotaDebitoA (2)

solicitudNC := &wsfev1.FECAESolicitarRequest{
    FeCAEReq: wsfev1.FeCAEReq{
        FeCabReq: wsfev1.FeCabReq{
            CantReg:  1,
            PtoVta:   1,
            CbteTipo: tipoNC,
        },
        FeDetReq: []wsfev1.FECAEDetRequest{{
            Concepto:               afipkit.ConceptoServicios,
            DocTipo:                afipkit.DocCUIT,
            DocNro:                 30111111118,
            CbteDesde:              ultimoNC + 1,
            CbteHasta:              ultimoNC + 1,
            CbteFch:                fiscal.FormatFechaAFIP(time.Now()),
            ImpTotal:               "121000.00",
            ImpTotConc:             "0",
            ImpNeto:                "100000.00",
            ImpOpEx:                "0",
            ImpIVA:                 "21000.00",
            ImpTrib:                "0",
            FchServDesde:           "20260301",
            FchServHasta:           "20260331",
            FchVtoPago:             "20260418",
            MonId:                  afipkit.MonedaPesos,
            MonCotiz:               "1",
            CondicionIVAReceptorId: afipkit.CondIVAResponsableInscripto,
            // Referencia obligatoria a la factura que anula o ajusta:
            CbtesAsoc: &wsfev1.CbtesAsocArray{
                CbteAsoc: []wsfev1.CbteAsocItem{{
                    Tipo:    afipkit.CbteFacturaA,
                    PtoVta:  1,
                    Nro:     siguiente,
                    Cuit:    cuitEmisor,
                    CbteFch: "20260321",
                }},
            },
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
```

---

## Consultas de parámetros de WSFEv1

El cliente de `wsfev1` expone métodos para consultar las tablas maestras de AFIP:

```go
tiposCbte, _ := wsfe.TiposComprobante(ctx, token, cuit)
alicuotas, _  := wsfe.AlicuotasIVA(ctx, token, cuit)
ptosVenta, _  := wsfe.PuntosVenta(ctx, token, cuit)
tiposDoc, _   := wsfe.TiposDocumento(ctx, token, cuit)
monedas, _    := wsfe.TiposMonedas(ctx, token, cuit)
cotiz, _      := wsfe.Cotizacion(ctx, token, cuit, afipkit.MonedaDolar)
maxCbts, _    := wsfe.MaxComprobantesPerRequest(ctx, token, cuit)

// Condiciones IVA del receptor admitidas para una clase dada (ej. "A", "B", "C"):
condsIva, _   := wsfe.CondicionesIVAReceptor(ctx, token, cuit, "A")
```

---

## Guía de referencia: Manejo de errores

### Helpers funcionales para inspeccionar errores

No requieren desempaquetar con `errors.As`:

| Función | Código / Causa | Acción recomendada |
|---|---|---|
| `afipkit.IsIndeterminate(err)` | Red, timeout, SOAP fault, 500, 501, 502 | El estado es incierto: no marcar rechazado; confirmar luego con `Recuperar`. |
| `afipkit.IsTokenRechazado(err)` | Error 600 (token vencido o no corresponde firma) | Reintentar una vez con `cached.CallWithAuthRetry`. |
| `afipkit.IsCUITNoRepresentado(err)` | Error 601 (Auth.Cuit no representado por el token) | La relación de delegación no existe o no estaba vigente al emitir el TA actual. |
| `afipkit.IsPuntoVentaInvalido(err)` | Error 10004 / 10005 | Dar de alta el punto de venta en AFIP como **Web Services (RECE)**. |
| `afipkit.IsCondicionIVAReceptorInvalida(err)` | Error 10242, 10243, 10246 | Corregir `CondicionIVAReceptorId` conforme a la RG 5616. |
| `wsaa.IsAlreadyAuthenticated(err)` | Fault `coe.alreadyAuthenticated` | Hay un TA vigente en AFIP: usar un store persistente o `WithLocker`. |
| `wsaa.IsNotAuthorized(err)` | Fault `coe.notAuthorized` | Asociar el certificado al servicio en el Administrador de Relaciones. |
| `wsaa.IsCertUntrusted(err)` | Fault `cms.cert.untrusted` | Certificado de homologación usado en producción (o viceversa). |
| `wsaa.IsCertExpired(err)` | Fault `cms.cert.expired` | Renovar el certificado digital vencido en AFIP. |
| `wsaa.IsClockError(err)` | Faults `xml.generationTime.invalid` o `xml.expirationTime.expired` | Sincronizar el reloj del servidor con NTP. |
| `wsaa.IsUnavailable(err)` | Faults `wsaa.*`, `wsn.unavailable` | Falla temporal de AFIP: reintentar luego de 60 segundos. |

### Inspección de `*afipkit.AFIPError`

```go
if afipErr, ok := afipkit.IsAFIPError(err); ok {
    fmt.Printf("Código principal de error: %d\n", afipErr.FirstCode())
    for _, d := range afipErr.Errors {
        fmt.Printf("- [%d] %s\n", d.Code, d.Msg)
    }

    if afipErr.IsAlreadyRegistered() {
        // Error 10016: número o fecha no es el próximo
    }
    if afipErr.IsInvalidPtoVta() {
        // Error 10004 / 10005
    }
    if afipErr.IsAmountMismatch() {
        // Error 10048: importes no cuadran
    }
    if afipErr.IsNotFound() {
        // Error 602 en consultas
    }
    if afipErr.IsInternal() {
        // Error 500, 501, 502 interno de AFIP
    }
}
```
