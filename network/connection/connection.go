package connection

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"time"

	quic "github.com/quic-go/quic-go"
)

// P2P connections written in QUIC.
// TODO: Make better variable names
// TODO: Better timing with disconnect of peer connections and UDP disconnect
// TODO: Rework which variables are public/private

const (
	InitMessage      = "INITIALIZE"
	applicationError = 0x2468
)

// For better packet loss handling
var (
	quicConfig = quic.Config{
		InitialStreamReceiveWindow:     10 * 1024 * 1024,
		MaxStreamReceiveWindow:         10 * 1024 * 1024,
		InitialConnectionReceiveWindow: 15 * 1024 * 1024,
		MaxConnectionReceiveWindow:     15 * 1024 * 1024,

		MaxIdleTimeout: 30 * time.Second,

		EnableDatagrams: true,

		KeepAlivePeriod: time.Second * 25,
	}
)

func GetAvailablePort() int {
	addr, err := net.ResolveTCPAddr("tcp4", "localhost:0")
	if err != nil {
		return 0
	}

	listener, err := net.ListenTCP("tcp4", addr)
	if err != nil {
		return 0
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

// Copied from official example https://github.com/quic-go/quic-go/blob/master/example/echo/echo.go
// Setup a bare-bones TLS config for the server
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"foo"},
	}
}
