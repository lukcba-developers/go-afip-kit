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

func TestFaultHelpers(t *testing.T) {
	wrap := func(code string) error {
		return &afipkit.ErrWSAAAuth{Err: fmt.Errorf("LoginCms: %w", &soap.Fault{Code: "ns1:" + code, String: "x"})}
	}
	tests := []struct {
		code string
		fn   func(error) bool
	}{
		{FaultNotAuthorized, IsNotAuthorized},
		{FaultCertUntrusted, IsCertUntrusted},
		{FaultCertExpired, IsCertExpired},
		{FaultSignInvalid, IsSignatureInvalid},
		{FaultGenerationTimeInvalid, IsClockError},
		{FaultExpirationTimeExpired, IsClockError},
		{FaultWSAAUnavailable, IsUnavailable},
		{FaultWSAAInternalError, IsUnavailable},
		{FaultServiceUnavailable, IsUnavailable},
	}
	for _, tt := range tests {
		err := wrap(tt.code)
		if got := FaultCode(err); got != tt.code {
			t.Errorf("FaultCode = %q, want %q", got, tt.code)
		}
		if !tt.fn(err) {
			t.Errorf("helper for %s returned false", tt.code)
		}
		if tt.fn(wrap(FaultCMSBad)) {
			t.Errorf("helper for %s matched cms.bad", tt.code)
		}
	}
	if FaultCode(errors.New("x")) != "" || FaultCode(nil) != "" {
		t.Error("FaultCode of a non-fault must be empty")
	}
	if got := FaultCode(&soap.Fault{Code: "coe.notAuthorized"}); got != FaultNotAuthorized {
		t.Errorf("code without namespace: %q", got)
	}
}
