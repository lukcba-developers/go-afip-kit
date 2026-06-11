package wsaa

import (
	"errors"
	"strings"

	"github.com/lukcba-developers/go-afip-kit/soap"
)

// IsAlreadyAuthenticated reports whether err is the WSAA fault
// coe.alreadyAuthenticated ("El CEE ya posee un TA valido para el acceso al
// WSN solicitado").
//
// WSAA refuses to issue a new TA while a previous one is still valid for the
// same certificate + service. If your process lost the cached token (e.g. a
// restart with an in-memory TokenStore), this error means you must wait until
// the previous TA expires (up to 12h) — re-trying immediately will keep
// failing. The durable fix is a persistent TokenStore (Redis, database) so
// the TA survives restarts.
func IsAlreadyAuthenticated(err error) bool {
	var fault *soap.Fault
	if !errors.As(err, &fault) {
		return false
	}
	return strings.Contains(fault.Code, "alreadyAuthenticated") ||
		strings.Contains(fault.String, "ya posee un TA")
}
