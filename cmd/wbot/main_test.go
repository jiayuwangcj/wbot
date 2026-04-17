package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/httpregister"
	"github.com/jiayu/wbot/internal/master"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"no args", []string{"wbot"}, 2},
		{"help flag", []string{"wbot", "-h"}, 0},
		{"help long", []string{"wbot", "--help"}, 0},
		{"help cmd", []string{"wbot", "help"}, 0},
		{"version flag", []string{"wbot", "-version"}, 0},
		{"version cmd", []string{"wbot", "version"}, 0},
		{"agent poll smoke", []string{"wbot", "agent", "-duration", "1ms", "-interval", "1ms"}, 0},
		{"agent help", []string{"wbot", "agent", "-h"}, 0},
		{"master short run", []string{"wbot", "master", "-duration", "1ms"}, 0},
		{"master tls flag mismatch", []string{"wbot", "master", "-tls-cert", "only.pem"}, 2},
		{"paper submit", []string{"wbot", "paper", "-symbol", "T.US", "-side", "sell"}, 0},
		{"paper bad side", []string{"wbot", "paper", "-side", "maybe"}, 2},
		{"agent bad flag", []string{"wbot", "agent", "-notaflag"}, 2},
		{"unknown", []string{"wbot", "nope"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestAgentMasterURL(t *testing.T) {
	mem := master.NewMemory()
	srv := httptest.NewServer(httpregister.Handler(mem))
	defer srv.Close()
	if got := run([]string{"wbot", "agent", "-duration", "5ms", "-interval", "1ms", "-master-url", srv.URL}); got != 0 {
		t.Fatalf("run() = %d; want 0", got)
	}
}

func TestMasterTLSMissingFiles(t *testing.T) {
	if got := run([]string{"wbot", "master", "-tls-cert", "/nonexistent/cert.pem", "-tls-key", "/nonexistent/key.pem", "-duration", "1ms"}); got != 1 {
		t.Fatalf("run() = %d; want 1", got)
	}
}

func TestMasterTLSShortRun(t *testing.T) {
	certPath, keyPath := writeTestCertPair(t)
	port := freeTCPPort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	argv := []string{"wbot", "master", "-listen", addr, "-tls-cert", certPath, "-tls-key", keyPath, "-duration", "1ms"}
	if got := run(argv); got != 0 {
		t.Fatalf("run() = %d; want 0", got)
	}
}

func writeTestCertPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"wbot-test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
