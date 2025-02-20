package transfer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"

	quic "github.com/quic-go/quic-go"
)

// TODO: Do something about the EOF stuff that gets returned whenever a peer has disconnected but not
// 		 yet timed out
// TODO: Make constructors for sender and listener

// Probably a stupid name
type Listener struct {
	Addr      net.UDPAddr
	ReadyChan chan int
}

type Sender struct {
	DataChan  chan int
	QuitChan  chan int
	ReadyChan chan int
	Addr      net.UDPAddr // I guess this is the target?
	// Raddr     net.UDPAddr
}

// type Receiver struct {
// 	listener  *Server
// 	QuitChan  chan int
// 	ReadyChan chan int
// }

func (p *Sender) Send() {
	conn, err := quic.DialAddr(context.Background(), p.Addr.String(), generateTLSConfig(), nil)
	if err != nil {
		fmt.Println("Error when setting up QUIC connection")
		fmt.Println(err)
		return
	}
	defer conn.CloseWithError(0, "")

	fmt.Println("---- SENDER CONNECTED ----")
	// fmt.Printf("Ports: %d ----> %d\n", p.Laddr.Port, p.Raddr.Port)
	// stream, err
	// TODO: Golden streams
	p.ReadyChan <- 1

	for {
		select {
		case <-p.QuitChan:
			fmt.Println("Closing Send connection...")
			// Close the connection peacefully and stop the goroutine
			return
		case <-p.DataChan:
			fmt.Println("Sending data to port ", p.Addr.Port)

			_, err := conn.Write([]byte("Test message. Did i arrive?\n"))
			if err != nil {
				fmt.Println("Could not send TCP data")
				fmt.Println(err)
				continue
			}
		}
	}
}

func (l *Listener) Listen() {
	listener, err := quic.ListenAddr(l.Addr.String(), generateTLSConfig(), nil)
	if err != nil {
		println("Quick listenaddr failed")
		log.Fatal(err)
	}
	defer listener.Close()

	fmt.Println("Listener ready on port", l.Addr)
	l.ReadyChan <- 1

	// Start accepting connections
	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			fmt.Println("Failed to accept connection:", err)
			continue
		}

		fmt.Println("---- LISTENER CONNECTED ----")
		go HandleConnection(conn)
	}
}

func HandleConnection(conn quic.Connection) {
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		fmt.Println("Could not handle stream ;)", err)
		return
	}

	buffer := make([]byte, 1024)
	for {
		n, err := stream.Read(buffer)
		if err != nil {
			fmt.Println("Failed to read from stream", err)
			continue
		}

		fmt.Println("Received message", buffer[:n])
	}
}

// func (lc *Receiver) Listen() error {
// 	// quic.Transport
// 	conn, err := lc.listener.listener.Accept()
// 	fmt.Println("passed?")
// 	if err != nil {
// 		fmt.Println("Error when connecting listener over TCP")
// 		fmt.Println(err)
// 		return err
// 	}

// 	fmt.Println("---- LISTENER CONNECTED ----")
// 	lc.ReadyChan <- 1

// 	for {
// 		select {
// 		case <-lc.QuitChan:
// 			// Graceful shutdown
// 			fmt.Println("Closing Listener connection...")
// 			return nil
// 		default:
// 			fmt.Print("")
// 			data, err := bufio.NewReader(conn).ReadString('\n')
// 			if err != nil {
// 				fmt.Println("Encountered error when reading data: ")
// 				fmt.Println(err)
// 				continue
// 			}
// 			// Send into channl
// 			fmt.Println("Received data: ")
// 			fmt.Println(data)
// 		}
// 	}
// }

func (s Sender) String() string {
	return fmt.Sprintln(
		"P2P sender object\n",
		fmt.Sprintf("~ host address: %s\n", &s.Laddr),
		fmt.Sprintf("~ peer address: %s", &s.Raddr),
	)
}

// func (l Receiver) String() string {
// 	return fmt.Sprintln(
// 		"P2P listener object",
// 		fmt.Sprintf("~ listening on: %s", &l.listener.Addr),
// 	)
// }

func NewSender(hostAddr net.UDPAddr, peerAddr net.UDPAddr) Sender {
	return Sender{
		DataChan:  make(chan int),
		QuitChan:  make(chan int),
		ReadyChan: make(chan int),
		Laddr:     hostAddr,
		Raddr:     peerAddr,
	}
}

// func NewListenConnection(listener *Server) Receiver {
// 	return Receiver{
// 		listener:  listener,
// 		QuitChan:  make(chan int),
// 		ReadyChan: make(chan int),
// 	}
// }

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
