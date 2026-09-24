//go:build minimal || nolego

package mylego

import (
	"errors"
	"testing"
)

func TestMinimalRejectsAutomaticCertificates(t *testing.T) {
	for _, mode := range []string{"dns", "http", "tls"} {
		t.Run(mode, func(t *testing.T) {
			client, err := New(&CertConfig{CertMode: mode})
			if client != nil || !errors.Is(err, ErrAutoCertificateDisabled) {
				t.Fatalf("New returned %v, %v", client, err)
			}
		})
	}
	client := &LegoCMD{}
	if cert, key, err := client.DNSCert(); cert != "" || key != "" || !errors.Is(err, ErrAutoCertificateDisabled) {
		t.Fatal("DNS issuance was not rejected")
	}
	if cert, key, err := client.HTTPCert(); cert != "" || key != "" || !errors.Is(err, ErrAutoCertificateDisabled) {
		t.Fatal("HTTP/TLS issuance was not rejected")
	}
	if cert, key, renewed, err := client.RenewCert(); cert != "" || key != "" || renewed || !errors.Is(err, ErrAutoCertificateDisabled) {
		t.Fatal("renewal was not rejected")
	}
	if err := client.Run(); !errors.Is(err, ErrAutoCertificateDisabled) {
		t.Fatal(err)
	}
	if renewed, err := client.Renew(); renewed || !errors.Is(err, ErrAutoCertificateDisabled) {
		t.Fatal("direct renewal was not rejected")
	}
}
