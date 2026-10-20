package wsaa

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"

	"go.mozilla.org/pkcs7"
)

// signCMS signs the given data using PKCS#7/CMS (detached=false) with a
// SHA-256 digest. This produces the signed CMS blob that WSAA's LoginCms
// expects. pkcs7.NewSignedData defaults to SHA-1, which is deprecated for
// signatures, so the digest is set explicitly.
func signCMS(data []byte, cert *x509.Certificate, key *rsa.PrivateKey) ([]byte, error) {
	signedData, err := pkcs7.NewSignedData(data)
	if err != nil {
		return nil, fmt.Errorf("pkcs7 new signed data: %w", err)
	}
	signedData.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)

	if err := signedData.AddSigner(cert, key, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, fmt.Errorf("pkcs7 add signer: %w", err)
	}

	cms, err := signedData.Finish()
	if err != nil {
		return nil, fmt.Errorf("pkcs7 finish: %w", err)
	}

	return cms, nil
}
