//go:build minimal

package controller

import (
	"errors"
	"testing"

	"github.com/HoshinoNeko/XrayR/common/mylego"
)

func TestMinimalCertificateModes(t *testing.T) {
	cert, key, err := getCertFile(&mylego.CertConfig{CertMode: "file", CertFile: "existing.crt", KeyFile: "existing.key"})
	if err != nil || cert != "existing.crt" || key != "existing.key" {
		t.Fatalf("existing certificate paths not preserved: %q %q %v", cert, key, err)
	}
	if _, _, err := getCertFile(&mylego.CertConfig{CertMode: "file"}); err == nil {
		t.Fatal("empty file paths accepted")
	}
	for _, mode := range []string{"dns", "http", "tls"} {
		if _, _, err := getCertFile(&mylego.CertConfig{CertMode: mode}); !errors.Is(err, mylego.ErrAutoCertificateDisabled) {
			t.Fatalf("%s: expected minimal-build error, got %v", mode, err)
		}
	}
}
