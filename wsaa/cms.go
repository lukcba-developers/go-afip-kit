package wsaa

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"fmt"

	"go.mozilla.org/pkcs7"
)

// signCMS signs the given data using PKCS#7/CMS (detached=false) with the
// given digest algorithm (pkcs7.OIDDigestAlgorithmSHA256 or ...SHA1). This
// produces the signed CMS blob that WSAA's LoginCms expects.
func signCMS(data []byte, cert *x509.Certificate, key *rsa.PrivateKey, digest asn1.ObjectIdentifier) ([]byte, error) {
	signedData, err := pkcs7.NewSignedData(data)
	if err != nil {
		return nil, fmt.Errorf("pkcs7 new signed data: %w", err)
	}
	signedData.SetDigestAlgorithm(digest)

	if err := signedData.AddSigner(cert, key, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, fmt.Errorf("pkcs7 add signer: %w", err)
	}

	cms, err := signedData.Finish()
	if err != nil {
		return nil, fmt.Errorf("pkcs7 finish: %w", err)
	}

	return cms, nil
}
