# Changelog

## v0.3.1 (sin publicar)

Correcciones de seguridad surgidas de la auditoría del 20/10/2026.

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
