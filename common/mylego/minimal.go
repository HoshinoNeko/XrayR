//go:build minimal || nolego

package mylego

import "errors"

const AutoCertificatesAvailable = false

// ErrAutoCertificateDisabled is returned without contacting an ACME service or
// writing account/certificate files. Existing certificates use CertMode: file.
var ErrAutoCertificateDisabled = errors.New("minimal/nolego build disables automatic certificate issuance and renewal (dns/http/tls); use CertMode: file with CertFile and KeyFile, or install the full build")

func New(_ *CertConfig) (*LegoCMD, error) {
	return nil, ErrAutoCertificateDisabled
}

func (*LegoCMD) DNSCert() (string, string, error) {
	return "", "", ErrAutoCertificateDisabled
}

func (*LegoCMD) HTTPCert() (string, string, error) {
	return "", "", ErrAutoCertificateDisabled
}

func (*LegoCMD) RenewCert() (string, string, bool, error) {
	return "", "", false, ErrAutoCertificateDisabled
}

func (*LegoCMD) Run() error { return ErrAutoCertificateDisabled }

func (*LegoCMD) Renew() (bool, error) { return false, ErrAutoCertificateDisabled }
