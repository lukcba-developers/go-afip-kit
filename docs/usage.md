# Guia de uso de go-afip-kit

## Instalacion

```bash
go get github.com/lukcba-developers/go-afip-kit
```

## Ejemplo completo: Emitir factura

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    afipkit "github.com/lukcba-developers/go-afip-kit"
    "github.com/lukcba-developers/go-afip-kit/cert"
    "github.com/lukcba-developers/go-afip-kit/fiscal"
    "github.com/lukcba-developers/go-afip-kit/wsaa"
    "github.com/lukcba-developers/go-afip-kit/wsfev1"
)

func main() {
    ctx := context.Background()
    production := false // homologacion

    // 1. Cargar certificado y clave privada
    certPEM, _ := os.ReadFile("mi_certificado.crt")
    keyPEM, _ := os.ReadFile("mi_clave.key")

    // 2. Validar certificado
    result, err := cert.Validate(certPEM, keyPEM, "20123456786")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Certificado valido, vence en %d dias\n", result.DaysUntilExpiry)

    // 3. Parsear cert+key para autenticacion
    x509Cert, rsaKey, err := cert.ParseCertAndKey(certPEM, keyPEM)
    if err != nil {
        log.Fatal(err)
    }

    // 4. Autenticar con WSAA (con cache en memoria)
    wsaaClient := wsaa.NewClient()
    cachedAuth := wsaa.NewCachedClient(wsaaClient, wsaa.NewMemoryStore())

    token, err := cachedAuth.Authenticate(ctx, x509Cert, rsaKey,
        20123456786, "wsfe", production)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Token obtenido, expira: %s\n", token.Expiration)

    // 5. Crear cliente WSFEv1
    wsfe := wsfev1.NewClient(production)

    // 6. Verificar estado de AFIP
    health, err := wsfe.HealthCheck(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if !health.IsOK() {
        log.Fatalf("AFIP no disponible: %+v", health)
    }

    // 7. Obtener ultimo comprobante autorizado
    cuit := int64(20123456786)
    ptoVta := 1
    tipoCbte := afipkit.CbteFacturaB

    ultimo, err := wsfe.UltimoAutorizado(ctx, token, cuit, ptoVta, tipoCbte)
    if err != nil {
        log.Fatal(err)
    }
    siguiente := ultimo + 1

    // 8. Construir solicitud
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

    // 9. Emitir con reproceso automatico (idempotente)
    caeResult, err := wsfe.SolicitarCAEConReproceso(ctx, token, cuit, solicitud)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("CAE: %s (vto: %s)\n", caeResult.CAE, caeResult.CAEFchVto)
    if caeResult.Reproceso {
        fmt.Println("(recuperado por reproceso)")
    }
}
```

## Uso del KeyVault (encriptar claves privadas)

```go
// Crear vault con master key de variable de entorno
vault, err := cert.NewKeyVault(os.Getenv("AFIP_MASTER_KEY"))
if err != nil {
    log.Fatal(err)
}

// Encriptar para almacenar en DB
encrypted, err := vault.EncryptToBase64(keyPEM)
// encrypted es un string base64 seguro para DB

// Desencriptar al usar
decrypted, err := vault.DecryptFromBase64(encrypted)
// decrypted == keyPEM original
```

## Reglas fiscales

```go
// Determinar tipo de comprobante automaticamente
tipoCbte, err := fiscal.DeterminarTipoComprobante(
    fiscal.ResponsableInscripto, // emisor
    fiscal.ResponsableInscripto, // receptor
)
// tipoCbte == afipkit.CbteFacturaA (1)

// RI -> Monotributista = Factura A (RG 5003) + leyenda obligatoria
if fiscal.RequiereLeyendaRG5003(tipoCbte, afipkit.CondIVAMonotributo) {
    // imprimir fiscal.LeyendaRG5003 en el PDF
}

// Identificacion del receptor (DNI, consumidor final, umbral de ARCA)
docTipo, docNro, err := fiscal.DeterminarDocumento(
    afipkit.CbteFacturaC, afipkit.DocDNI, 30111222, 15000.00, umbralIdentificacion)

// Condicion IVA del receptor (RG 5616) admitida por la clase de comprobante
err = fiscal.ValidarCondicionIVAReceptor(afipkit.CbteFacturaB, afipkit.CondIVAConsumidorFinal)

// Validar cuadratura de importes
err = fiscal.ValidarCuadratura(fiscal.Importes{
    ImpNeto:  100000.00,
    ImpIVA:   21000.00,
    ImpTotal: 121000.00,
})

// Calcular IVA
iva, err := fiscal.CalcularIVA(100000.00, afipkit.IVA21)
// iva == 21000.00

// Validar CUIT
err = afipkit.ValidarCUIT(20123456786)

// Validar fechas AFIP
err = fiscal.ValidarFechaAFIP("20260318")
fecha, err := fiscal.ParseFechaAFIP("20260318")
str := fiscal.FormatFechaAFIP(time.Now())
```

## Implementar TokenStore con Redis

```go
type RedisTokenStore struct {
    client *redis.Client
}

func (r *RedisTokenStore) Get(ctx context.Context, key string) (*wsaa.TokenAcceso, error) {
    data, err := r.client.Get(ctx, key).Bytes()
    if err == redis.Nil {
        return nil, nil
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
    ttl := time.Until(token.Expiration) - 30*time.Minute
    return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *RedisTokenStore) Delete(ctx context.Context, key string) error {
    return r.client.Del(ctx, key).Err()
}
```

## Nota de credito

```go
// Obtener tipo de NC para una factura
tipoNC, err := fiscal.DeterminarTipoNC(afipkit.CbteFacturaA)
// tipoNC == afipkit.CbteNotaCreditoA (3)

// Emitir NC referenciando comprobante original
solicitud := &wsfev1.FECAESolicitarRequest{
    FeCAEReq: wsfev1.FeCAEReq{
        FeCabReq: wsfev1.FeCabReq{
            CantReg:  1,
            PtoVta:   1,
            CbteTipo: tipoNC,
        },
        FeDetReq: []wsfev1.FECAEDetRequest{{
            // ... mismos campos que factura ...
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
