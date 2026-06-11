package wsaa

import (
	"errors"
	"fmt"
	"testing"

	afipkit "github.com/lukcba-developers/go-afip-kit"
	"github.com/lukcba-developers/go-afip-kit/soap"
)

func TestIsAlreadyAuthenticated(t *testing.T) {
	fault := &soap.Fault{
		Code:   "ns1:coe.alreadyAuthenticated",
		String: "El CEE ya posee un TA valido para el acceso al WSN solicitado",
	}

	// Direct fault
	if !IsAlreadyAuthenticated(fault) {
		t.Error("direct fault not detected")
	}

	// Wrapped the way Client.Authenticate returns it
	wrapped := &afipkit.ErrWSAAAuth{Err: fmt.Errorf("LoginCms: %w", fault)}
	if !IsAlreadyAuthenticated(wrapped) {
		t.Error("wrapped fault not detected")
	}

	// Unrelated errors
	if IsAlreadyAuthenticated(errors.New("connection refused")) {
		t.Error("false positive on generic error")
	}
	if IsAlreadyAuthenticated(&soap.Fault{Code: "ns1:cms.bad", String: "CMS invalido"}) {
		t.Error("false positive on other WSAA fault")
	}
	if IsAlreadyAuthenticated(nil) {
		t.Error("false positive on nil")
	}
}
