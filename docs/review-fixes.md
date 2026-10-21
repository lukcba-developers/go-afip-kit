# Code Review: Hallazgos y correcciones aplicadas

Historial y registro técnico de revisiones de código, auditorías de seguridad y correcciones aplicadas en `go-afip-kit`.

---

## Revisión inicial (2026-03-21)

### Bugs corregidos

#### 1. Data race en MemoryStore (CRÍTICO)
- **Archivo:** `wsaa/token_store.go`
- **Problema:** El mapa `tokens` se accedía sin sincronización. Múltiples goroutines llamando a `Get`/`Set`/`Delete` causaban carreras de datos.
- **Fix:** Se agregó `sync.RWMutex`. `Get` utiliza `RLock()`, mientras que `Set` y `Delete` utilizan `Lock()`.

#### 2. CUIT substring match en lugar de exact match (CRÍTICO)
- **Archivo:** `cert/validator.go`
- **Problema:** `strings.Contains()` permitía que `"123456"` coincidiera con `"20123456789"`, permitiendo que un CUIT parcial superara la validación.
- **Fix:** Se reemplazó por `extractCUIT()`, que extrae exactamente 11 dígitos, realiza comparación exacta y valida longitud y formato.

#### 3. ParseCondicionIVA retornaba zero-value silenciosamente (CRÍTICO)
- **Archivo:** `fiscal/types.go`
- **Problema:** Para valores no reconocidos retornaba `CondicionIVA(0)` sin error, impidiendo distinguir entre "desconocido" y "no seteado".
- **Fix:** Se definió `CondicionDesconocida = 0` como centinela explícito y `ParseCondicionIVA` pasó a retornar `(CondicionIVA, error)`.

#### 4. Precisión float en cálculos monetarios (CRÍTICO)
- **Archivo:** `fiscal/validation.go`
- **Problema:** `ValidarCuadratura` usaba aritmética de punto flotante `float64` con una tolerancia mágica de `0.01`. `CalcularIVA` tenía fórmulas de redondeo frágiles.
- **Fix:** `ValidarCuadratura` y `CalcularIVA` se migraron a **aritmética entera de centavos** (`int64`), con redondeo estricto y comparación exacta.

#### 5. IVAAlicuotaPorcentaje retornaba 0 silenciosamente
- **Archivo:** `afip.go`
- **Problema:** Para identificadores desconocidos retornaba `0` sin reportar error.
- **Fix:** Se modificó para retornar `(float64, error)`.

### Mejoras de robustez

#### 6. Detección de SOAP Fault en respuestas HTTP
- **Archivo:** `soap/client.go`
- **Problema:** Si AFIP retornaba un SOAP Fault tanto con HTTP 200 como con HTTP 500, el unmarshaling fallaba con errores opacos de XML.
- **Fix:** Se agregó `extractFault()` que analiza el body en busca del elemento `<Fault>` y lo mapea al tipo estructurado `*soap.Fault`.

#### 7. UniqueID del TRA
- **Archivo:** `wsaa/client.go`
- **Problema:** `now.Unix()` generaba colisiones si dos solicitudes se iniciaban en el mismo segundo.
- **Fix inicial (v0.1):** Se cambió transitoriamente a `now.UnixNano()`.
- > [!IMPORTANT]
  > **Corrección posterior (v0.5.2):** La especificación XML Schema (`loginTicketRequest.xsd`) define `uniqueId` como `xs:unsignedInt` (32 bits sin signo). Los valores de `UnixNano()` (~1,7·10¹⁸) desbordaban 32 bits y causaban el rechazo de WSAA con fault `xml.bad`. En v0.5.2 se corrigió definitivamente a un contador atómico de 32 bits (`atomic.Uint32`) basado en segundos Unix que no se repite dentro del proceso.

#### 8. Errores retryable vs permanente
- **Archivo:** `errors.go`
- **Problema:** No existía distinción programática entre errores transitorios y errores de configuración permanentes.
- **Fix:** Se incorporó el método `IsRetryable()` en `AFIPError`.

#### 9. Validación de CUIT (módulo 11)
- **Archivo:** `afip.go`
- **Problema:** No existía validación de formato ni dígito verificador.
- **Fix:** Se agregó `ValidarCUIT(cuit int64) error` con el algoritmo oficial de AFIP (módulo 11).

#### 10. Validación de fechas AFIP (YYYYMMDD)
- **Archivo:** `fiscal/validation.go`
- **Problema:** Fechas pasadas como string sin comprobación de calendario ni formato.
- **Fix:** Se agregaron `ValidarFechaAFIP`, `ParseFechaAFIP` y `FormatFechaAFIP`.

---

## Revisiones de seguridad y resiliencia (v0.3.0 - v0.4.0)

### 11. Seguridad en transporte SOAP y redirecciones
- **Archivo:** `soap/client.go`
- **Problema:** Un servidor intermediario o endpoint comprometido con redirección 307/308 reenviaría el body del request (que contiene el token y sign de WSAA) a otro host.
- **Fix:** El cliente HTTP por defecto desactiva las redirecciones (`http.ErrUseLastResponse`). Además, se fijó un tope de lectura de 10 MiB (`DefaultMaxResponseSize`) para prevenir ataques de agotamiento de memoria.

### 12. Aislamiento multi-tenant en cifrado de claves (`KeyVault` con AAD)
- **Archivo:** `cert/vault.go`
- **Problema:** En arquitecturas multi-tenant, un registro cifrado de clave privada podía copiarse a la fila de otro tenant en la base de datos y ser descifrado con la misma master key.
- **Fix:** Se implementaron `EncryptWithAAD` y `DecryptWithAAD` (y variantes base64) utilizando AES-256-GCM con AAD (Authenticated Additional Data). Se asocia la clave al CUIT del contribuyente; cualquier intento de descifrado con un CUIT distinto falla.

### 13. Prevención de `coe.alreadyAuthenticated` entre réplicas
- **Archivo:** `wsaa/cached_client.go`, `wsaa/locker.go`
- **Problema:** Cuando varias instancias o contenedores arrancan simultáneamente compartiendo Redis, sufren un miss de caché en paralelo y ambas llaman a `LoginCms`. WSAA rechaza a la segunda con `coe.alreadyAuthenticated`.
- **Fix:** Se incorporó la interfaz `wsaa.Locker` para lock distribuido (con soporte atómico en Redis vía `SET NX PX` y script Lua de desbloqueo), serializando los logins entre réplicas.

---

## Revisiones de compatibilidad con WSAA y ARCA (v0.5.0 - v0.6.0)

### 14. Fallback automático de algoritmo de firma (SHA-256 a SHA-1)
- **Archivo:** `wsaa/client.go`
- **Problema:** La especificación técnica de WSAA documenta SHA1+RSA, mientras que implementaciones modernas admiten SHA-256. En ciertos entornos de AFIP, un TRA con SHA-256 es rechazado con `cms.sign.invalid`.
- **Fix:** `wsaa.Client` firma con SHA-256; si recibe `cms.sign.invalid`, conmuta de forma atómica a SHA-1, reintenta inmediatamente y conserva el algoritmo para futuros tickets.

### 15. Header `SOAPAction` en WSAA LoginCms
- **Archivo:** `soap/client.go`
- **Problema:** El servidor Axis de WSAA real requiere estrictamente el header `SOAPAction` según SOAP 1.1. Al omitirlo para acciones vacías, fallaba con `Client.NoSOAPAction`.
- **Fix:** `soap.Client` envía siempre el header `SOAPAction`, enviando `SOAPAction: ""` cuando la acción es vacía.

### 16. Renovación atómica de tokens ante error 600
- **Archivo:** `wsaa/cached_client.go`
- **Problema:** Ante el error 600 ("no se corresponden token y firma"), una invalidación ciega (`Invalidate`) eliminaba el token de la base compartida. Si otra réplica lo acababa de renovar, perdía su token y el siguiente login fallaba con `coe.alreadyAuthenticated` por hasta 12 horas.
- **Fix:** Se crearon `Renew` y `CallWithAuthRetry`, que renuevan el token sin purgar el existente y solo lo reemplazan en caso de éxito confirmado.

### 17. Facturación por delegación y error 601
- **Archivo:** `errors.go`, `wsaa/cached_client.go`
- **Problema:** Cuando una plataforma factura por cuenta de terceros (contribuyentes delegantes), el CUIT de autenticación en WSAA es el de la plataforma, pero el CUIT en WSFEv1 es el del representado. Si la relación no existe o no estaba activa al emitirse el TA, AFIP responde con el error 601.
- **Fix:** Se agregó `afipkit.IsCUITNoRepresentado` (error 601), documentando la semántica de delegación y aclarando que renovar el TA no soluciona el error mientras el TA siga vigente.
