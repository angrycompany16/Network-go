package transfer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	quic "github.com/quic-go/quic-go"
)

// P2P connections written in QUIC.
// TODO: make the program retry instead of failing when a connection fails (or similar)

const (
	p2pBufferSize = 1024
)

type Listener struct {
	Addr      net.UDPAddr
	ReadyChan chan int
	DataChan  chan interface{}
}

type Sender struct {
	DataChan  chan interface{}
	QuitChan  chan int
	ReadyChan chan int
	Addr      net.UDPAddr
	Connected bool
}

func (l *Listener) Listen() {
	var listener *quic.Listener
	var err error
	for {
		listener, err = quic.ListenAddr(l.Addr.String(), generateTLSConfig(), nil)
		if err != nil {
			fmt.Println("Encountered error when setting up listener:", err)
			fmt.Println("Retrying...")
			time.Sleep(time.Second)
			continue
		}
		defer listener.Close()
		break
	}

	fmt.Println("Listener ready on port", l.Addr.Port)
	l.ReadyChan <- 1

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			fmt.Println("Error when accepting connection from", conn.RemoteAddr())
			fmt.Println("Failed to accept connection:", err)
			continue
		}

		fmt.Printf("---- LISTENER CONNECTED <- %s ----\n", conn.RemoteAddr())
		go l.handleConnection(conn)
	}
}

func (l *Listener) handleConnection(conn quic.Connection) {
	var stream quic.Stream
	var err error
	for {
		stream, err = conn.AcceptStream(context.Background())
		if err != nil {
			fmt.Println("Error when opening data stream from", conn.RemoteAddr())
			fmt.Println(err)
			fmt.Println("Retrying...")
			time.Sleep(time.Second)
			continue
		}
		break
	}

	buffer := make([]byte, p2pBufferSize)
	for {
		n, err := stream.Read(buffer)
		if err != nil {
			fmt.Println("Failed to read from stream from", conn.RemoteAddr())
			fmt.Println(err)
			continue
		}

		var result interface{}
		json.Unmarshal(buffer[0:n], &result)
		fmt.Println(result)

		l.DataChan <- result
	}
}

// NOTE: This function should always be called with target being a *pointer* to a struct
func (l *Listener) DecodeMsg(msg interface{}, target interface{}) error {
	jsonEnc, _ := json.Marshal(msg)
	err := json.Unmarshal(jsonEnc, target)

	if err != nil {
		fmt.Println("Could not parse message:", msg)
		return err
	}
	return nil
}

func (s *Sender) Send() {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}
	quicConf := quic.Config{KeepAlivePeriod: time.Second * 25}
	var conn quic.Connection
	var stream quic.Stream
	var err error
	for {
		// This is copied from https://github.com/quic-go/quic-go/blob/master/example/echo/echo.go
		conn, err = quic.DialAddr(context.Background(), s.Addr.String(), tlsConf, &quicConf)

		if err != nil {
			fmt.Println("Error when setting up QUIC connection:", err)
			fmt.Println("Retrying...")
			time.Sleep(time.Second)
			continue
		}
		defer conn.CloseWithError(0, "")

		stream, err = conn.OpenStreamSync(context.Background())
		if err != nil {
			fmt.Println("Error when making stream:", err)
			fmt.Println("Retrying...")
			time.Sleep(time.Second)
		}
		defer stream.Close()

		break
	}

	fmt.Printf("---- SENDER %s--->%s CONNECTED ----\n", conn.LocalAddr(), conn.RemoteAddr())
	s.ReadyChan <- 1

	for {
		select {
		case <-s.QuitChan:
			fmt.Printf("Closing Send connection to %s...\n", &s.Addr)
			return
		case data := <-s.DataChan:
			fmt.Println("Sending data to ", s.Addr.String())

			jsonData, err := json.Marshal(data)
			if err != nil {
				fmt.Println("Could not marshal data:", data)
				fmt.Println("Error:", err)
				continue
			}

			if len(jsonData) > p2pBufferSize {
				fmt.Printf(
					"Tried to send a message longer than the buffer size (length: %d, buffer size: %d)\n\t'%s'\n"+
						"Either send smaller packets, or go to network/transfer/transferQUIC.go and increase the buffer size",
					len(jsonData), bufSize, string(jsonData))
				continue
			}

			_, err = stream.Write(jsonData)
			if err != nil {
				fmt.Println("Could not send data over stream")
				fmt.Println(err)
				continue
			}
		}
	}
}

func (s Sender) String() string {
	return fmt.Sprintf("P2P sender object \n ~ peer address: %s", &s.Addr)
}

func NewListener(addr net.UDPAddr) Listener {
	return Listener{
		DataChan:  make(chan interface{}),
		ReadyChan: make(chan int),
		Addr:      addr,
	}
}

func NewSender(addr net.UDPAddr) Sender {
	return Sender{
		DataChan:  make(chan interface{}),
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
