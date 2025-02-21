package transfer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	quic "github.com/quic-go/quic-go"
)

// TODO: make the program retry instead of failing when a connection fails (or similar)
// TODO: Add arbitrary struct sending

type Listener struct {
	Addr      net.UDPAddr
	ReadyChan chan int
}

type Sender struct {
	DataChan  chan int
	QuitChan  chan int
	ReadyChan chan int
	Addr      net.UDPAddr
	Connected bool
}

func (s *Sender) Send() {
	// This is copied from https://github.com/quic-go/quic-go/blob/master/example/echo/echo.go
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	quicConf := quic.Config{KeepAlivePeriod: time.Second * 25}
	conn, err := quic.DialAddr(context.Background(), s.Addr.String(), tlsConf, &quicConf)
	if err != nil {
		fmt.Println("Error when setting up QUIC connection:", err)
		return
	}
	defer conn.CloseWithError(0, "")

	fmt.Printf("---- SENDER %s--->%s CONNECTED ----\n", conn.LocalAddr(), conn.RemoteAddr())
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		fmt.Println("Error when making stream:", err)
		return
	}
	defer stream.Close()

	// s.Connected = true
	s.ReadyChan <- 1

	for {
		select {
		case <-s.QuitChan:
			fmt.Printf("Closing Send connection to %s...\n", &s.Addr)
			return
		case <-s.DataChan:
			fmt.Println("Sending data to ", s.Addr.String())

			_, err := stream.Write([]byte("Test message. Did i arrive?"))
			if err != nil {
				fmt.Println("Could not send data over stream")
				fmt.Println(err)
				continue
			}
		}
	}
}

func (l *Listener) Listen() {
	listener, err := quic.ListenAddr(l.Addr.String(), generateTLSConfig(), nil)
	if err != nil {
		fmt.Println("Encountered error when setting up listener:", err)
		return
	}
	defer listener.Close()

	fmt.Println("Listener ready on port", l.Addr.Port)
	l.ReadyChan <- 1

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			fmt.Println("Error when accepting connection from", conn.RemoteAddr())
			fmt.Println("Failed to accept connection:", err)
			continue
		}

		fmt.Println("---- LISTENER CONNECTED ----")
		go HandleConnection(conn)
		l.ReadyChan <- 1
	}
}

func HandleConnection(conn quic.Connection) {
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		fmt.Println("Error when opening data stream from", conn.RemoteAddr())
		fmt.Println(err)
		fmt.Println("Closing connection...")
		return
	}

	buffer := make([]byte, 1024)
	for {
		n, err := stream.Read(buffer)
		if err != nil {
			fmt.Println("Failed to read from stream from", conn.RemoteAddr())
			fmt.Println(err)
			continue
		}

		fmt.Println("Received message", string(buffer[:n]))
	}

}

func (s Sender) String() string {
	return fmt.Sprintf("P2P sender object \n ~ peer address: %s", &s.Addr)
}

func NewSender(addr net.UDPAddr) Sender {
	return Sender{
		DataChan:  make(chan int),
		QuitChan:  make(chan int),
		ReadyChan: make(chan int),
		Addr:      addr,
		Connected: false,
	}
}

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
		NextProtos:   []string{"quic-echo-example"},
	}
}
