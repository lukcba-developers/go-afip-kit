# Changelog

## v0.7.1

- **wsaa (migración desde v0.6.x):** al actualizar, si WSAA responde
  `coe.alreadyAuthenticated` a un certificado y bajo la clave anterior
  (`wsaa:{homo|prod}:{cuit}:{service}`) hay un TA vigente emitido a ese mismo
  certificado, se reutiliza y se guarda bajo la clave nueva. "Ese mismo" quiere
  decir que el `uid` del token, el DN del certificado que lo obtuvo según el
  Manual del Desarrollador de WSAA, tiene el mismo CUIT y el mismo alias. Así
  la actualización no deja a los certificados sin token hasta que vence su TA.
  Ante cualquier duda (token ilegible, otro CUIT u otro alias, certificado que
  WSAA no acepta) no hay fallback y se devuelve el error de WSAA. Este camino
  de migración se va a quitar en una versión futura.

## v0.7.0

- **wsaa (seguridad):** la caché de tokens WSAA distingue el certificado. La
  clave pasa a ser `wsaa:{homo|prod}:{cuit}:{service}:{cert-sha256}`, con la
  huella SHA-256 del DER del certificado. Un token solo se devuelve al
  certificado que lo obtuvo, aunque varios certificados compartan el
  `TokenStore`. El lock distribuido (clave del token + `:lock`) también pasa a
  ser por certificado.
- **Cambio incompatible:** `CachedClient.Invalidate(ctx, cert, cuit, service,
  production)` ahora recibe el certificado.
- `Authenticate`, `Renew`, `CallWithAuthRetry` e `Invalidate` devuelven
  `wsaa.ErrNilCertificate` si el certificado es nil.
- **Migración:** las claves anteriores quedan huérfanas y vencen solas (como
  máximo 12 h). Al actualizar, el primer login de cada certificado que todavía
  tenga un TA vigente puede recibir `coe.alreadyAuthenticated` hasta que ese TA
  venza. Conviene actualizar cuando no haya emisión en curso o fuera de
  horario. Si un `TokenStore` filtra por el formato de la clave, hay que
  revisarlo.

## v0.6.0

- **afipkit:** `IsCUITNoRepresentado` (error 601): el `Auth.Cuit` enviado no
  está entre los CUIT que representa el token. Bajo delegación significa que
  la relación no existe o todavía no está en el TA. Renovar no sirve mientras
  el TA siga vigente.
- **docs:** facturación por delegación (una plataforma emite por cuenta de
  contribuyentes que le delegaron `wsfe`). El `cuit` de `CachedClient`
  identifica el TA, que es el CUIT del certificado. El de los métodos de
  `wsfev1` es el CUIT representado.

## v0.5.2

- **wsaa (corrección crítica):** `LoginCms` respondía `xml.bad` ("No se ha
  podido interpretar el XML contra el SCHEMA"). El `uniqueId` del TRA era
  `time.UnixNano()` (~1,7·10¹⁸), pero `loginTicketRequest.xsd` lo declara
  `xs:unsignedInt` (32 bits). Ahora es el Unix time en segundos (válido hasta
  2106) y nunca se repite dentro del proceso, ni siquiera en el reintento con
  SHA-1 del mismo segundo.
- Tests: el WSAA falso valida el TRA como el schema (orden, `uniqueId` de 32
  bits, fechas `xs:dateTime`) y responde `xml.bad` si no cumple.

## v0.5.1

- **wsaa/soap (corrección crítica):** `LoginCms` fallaba siempre contra WSAA
  real con el fault `Client.NoSOAPAction` ("no SOAPAction header!").
  `soap.Client` omitía el header `SOAPAction` cuando la acción venía vacía,
  que es el caso de LoginCms, y Axis lo exige. Ahora se envía siempre; sin
  acción va como `SOAPAction: ""` (SOAP 1.1). WSFEv1 no cambia: sigue
  mandando la acción de cada operación.
- Tests: el WSAA falso de los tests rechaza los requests sin `SOAPAction`,
  igual que Axis.

## v0.5.0

Mejoras a partir del feedback de proyectos que usan la librería y de una
revisión de la especificación técnica de WSAA.

- **wsaa (corrección importante):** la especificación técnica de WSAA indica
  que el TRA se firma con SHA1+RSA. Seguimos firmando con SHA-256, pero si
  LoginCms responde `cms.sign.invalid` el `Client` reintenta una vez con
  SHA-1 y lo recuerda. `Client.SignatureDigest()` informa cuál usa. Así el
  login funciona sin importar qué acepte WSAA.
- **wsaa:** `CachedClient.Renew` y `CachedClient.CallWithAuthRetry` para el
  error 600 (token rechazado). Renuevan el token sin borrar el guardado y
  reintentan una sola vez. Con `WithLocker` son atómicos entre réplicas.
- **wsaa:** clasificación de faults. `FaultCode(err)`, las constantes
  `Fault*` de la especificación, e `IsNotAuthorized`, `IsCertUntrusted`,
  `IsCertExpired`, `IsSignatureInvalid`, `IsClockError` e `IsUnavailable`.
- **afipkit:** `NormalizarCUIT(s) (int64, error)`, e `IsTokenRechazado` (600),
  `IsPuntoVentaInvalido` (10004/10005) e `IsCondicionIVAReceptorInvalida`
  (10242/10243/10246), que reciben un error. Se agregan las constantes
  `ErrCodeTokenRejected`, `ErrCodeCUITNotInToken`, `ErrCodePtoVtaOutOfRange` y
  `ErrCodeCondIVAReceptor*`. `AFIPError.IsInvalidPtoVta` incluye el 10004.
- **cert:** `CUITDelCertificado(certPEM)` devuelve el CUIT sin mirar el
  vencimiento, y `ValidationResult.CUIT` queda cargado aunque la validación
  falle.
- **Comportamiento:** `cert.Validate` / `ValidateCertOnly` normalizan el CUIT
  esperado con `NormalizarCUIT`. Un CUIT esperado con el dígito verificador
  inválido ahora da error.
- **Docs:** qué persistir para `Recuperar` (`FECAEDetRequest` como JSON, con
  un test que fija el formato) y que `IsIndeterminate` vale para todas las
  consultas de wsfev1.

- **go.mod:** el mínimo pasa a **Go 1.26.10**. Con 1.26.9, govulncheck marca
  GO-2026-6809 (`crypto/x509`), que la librería usa al parsear certificados. El
  CI también prueba con 1.26.10.

Sin cambios en firmas existentes.

## v0.4.0

- **wsaa:** lock distribuido opcional para coalescer los LoginCms entre
  procesos. Antes, con varias réplicas compartiendo el `TokenStore`, dos de
  ellas podían hacer LoginCms a la vez y WSAA rechazaba la segunda con
  `coe.alreadyAuthenticated`.
  - Nueva interfaz `wsaa.Locker` (`TryLock` / `Unlock` con owner y TTL), con
    ejemplo de Redis en el README.
  - Opciones `wsaa.WithLocker` y `wsaa.WithLockTTL`, y la constante
    `wsaa.DefaultLockTTL` (60 s).
  - `wsaa.MemoryLocker` / `NewMemoryLocker`, implementación en proceso para
    tests.
  - La clave del lock es la clave del token más `:lock`.
- **wsaa:** si LoginCms responde `coe.alreadyAuthenticated` y el store ya
  tiene un token válido guardado por otro proceso, `CachedClient.Authenticate`
  devuelve ese token en lugar del error.

Sin cambios de firmas existentes. Sin `WithLocker` el comportamiento es el de
v0.3.2.

## v0.3.2

- **deps:** `go.mozilla.org/pkcs7` pasa de v0.9.0 a v0.10.0. La única
  diferencia son controles de límites en el parser BER (`ber2der`) para
  datos malformados. La librería solo firma, no parsea, así que no cambia
  el comportamiento.
- **CI:** govulncheck se instala a mano y la instalación y
  `go mod download` se reintentan ante cortes transitorios del proxy de Go
  (`.github/scripts/retry.sh`).

Sin cambios de API. La firma del TRA con SHA-256 (v0.3.1) sigue pendiente de
verificar contra el WSAA real de homologación.

## v0.3.1

Correcciones de seguridad surgidas de la auditoría del 20/10/2026.

> La firma del TRA con SHA-256 tiene tests, pero todavía no se probó contra el
> WSAA real de homologación. Si WSAA la rechazara, `Authenticate` fallaría con
> `ErrWSAAAuth`. En ese caso se publica una versión de parche.

- **wsaa:** el TRA se firma con SHA-256. Antes usaba SHA-1, el default de
  `pkcs7.NewSignedData`.
- **soap:** las respuestas se leen con un tope de 10 MiB
  (`DefaultMaxResponseSize`, configurable con `soap.WithMaxResponseSize`).
- **soap:** el cliente HTTP por defecto no sigue redirects, para que el token
  y el sign no se reenvíen a otro host. Un `WithHTTPClient` propio conserva su
  política.
- **cert:** `Validate` y `ValidateCertOnly` devuelven error si `expectedCUIT`
  no es un CUIT de 11 dígitos. Antes salteaban el chequeo en silencio. Con
  `""` se sigue salteando.
- **cert:** nuevos `KeyVault.EncryptWithAAD` / `DecryptWithAAD` (y sus
  variantes base64), que atan la clave cifrada a su dueño (por ejemplo el
  CUIT). Los métodos sin AAD no cambian.
- **go.mod:** el mínimo pasa de Go 1.24.5 a **Go 1.26.9**. Go 1.24 ya no tiene
  soporte y con versiones anteriores a 1.26.9 govulncheck marca
  vulnerabilidades alcanzables de la stdlib (net/http, HTTP/2, crypto/tls) en
  el cliente SOAP. Con `GOTOOLCHAIN=auto` (el default), Go descarga la
  toolchain necesaria.
- **CI:** build, vet y tests con `-race` en Go 1.26.9 y en la última estable,
  más govulncheck en cada push, en cada PR y una vez por semana. Dependabot
  actualiza también las GitHub Actions.

## v0.3.0

### Breaking changes

- **`wsaa.CachedClient.Invalidate`** ahora recibe el ambiente:
  `Invalidate(ctx, cuit, service, production bool)`.
- **Claves del `TokenStore`**: pasan de `wsaa:{cuit}:{service}` a
  `wsaa:{homo|prod}:{cuit}:{service}`. Homologación y producción ya no comparten
  el token. Al actualizar, la primera llamada de cada ambiente hace un LoginCms
  nuevo; si WSAA responde `coe.alreadyAuthenticated` porque el TA anterior
  sigue vigente, borrá o renombrá la clave vieja en el store. Las claves viejas
  expiran solas por TTL (≤ 12 h).
- **`fiscal.DeterminarTipoComprobante(ResponsableInscripto, Monotributista)`**
  devuelve Factura A (1) en lugar de Factura B (6), según la RG 5003.
- **`wsfev1.SolicitarCAEConReproceso`** ante el error 10016 verifica que el
  comprobante registrado coincida con el enviado antes de devolver su CAE. Si
  no coincide devuelve `ErrComprobanteNoCoincide`; si no existe devuelve el
  10016 original. Antes devolvía el CAE de lo que hubiera en ese número.

### Nuevo

- `afipkit.IsIndeterminate(err)`: distingue un resultado desconocido (red,
  timeout, SOAP fault, contexto, respuesta incompleta, errores 500/501/502) de
  un rechazo de AFIP.
- `afipkit.ErrIncompleteResponse`, `AFIPError.IsNotFound()` (602),
  `AFIPError.IsInternal()` (500/501/502) y las constantes `ErrCodeNotFound` y
  `ErrCodeInternal*`.
- `wsfev1.Client.Recuperar`: confirma un comprobante con FECompConsultar y lo
  compara con el esperado. Junto con él, `ErrComprobanteNoCoincide` y
  `ComprobanteNoCoincideError`.
- `wsfev1.Client.CondicionesIVAReceptor` (FEParamGetCondicionIvaReceptor) y
  `ParamCondicionIvaReceptor`.
- Constantes `afipkit.CondIVA*` del catálogo de condición IVA del receptor de
  la RG 5616 (1, 4, 5, 6, 7, 8, 9, 10, 13, 15, 16).
- En `fiscal`: `LeyendaRG5003`, `RequiereLeyendaRG5003`, `DeterminarDocumento`,
  `ErrReceptorNoIdentificado`, `ValidarCondicionIVAReceptor`,
  `CondicionesIVAReceptorPermitidas` y `CondicionIVA.CondicionIVAReceptorID`.

### Cambios menores

- `SolicitarCAE` devuelve `ErrIncompleteResponse` cuando falta `FeDetResp` o
  cuando el Resultado es A pero no hay CAE.
- `ConsultarComprobante` devuelve `ErrIncompleteResponse` cuando falta
  `ResultGet`.
- Se corrigió la documentación del error 10016: significa "número o fecha no
  corresponde al próximo a autorizar", no "ya registrado".

## v0.2.0

- `CondicionIVAReceptorId` en `FECAEDetRequest` (RG 5616).

## v0.1.1

- Cache de tokens seguro para concurrencia, reintentos de transporte, QR
  (RG 4892) y validación de requests.
