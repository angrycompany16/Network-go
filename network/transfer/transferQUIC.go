package transfer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"

	quic "github.com/quic-go/quic-go"
)

// P2P connections written in QUIC.
// TODO: Make better variable names
// TODO: Better timing with disconnect of peer connections and UDP disconnect
// TODO: Rework which variables are public/private

const (
	p2pBufferSize    = 1024
	InitMessage      = "INITIALIZE"
	applicationError = 0x2468
)

type Listener struct {
	Addr           net.UDPAddr
	ReadyChan      chan int
	QuitChan       chan string
	DataChan       chan interface{}
	ConnectionChan chan net.Addr
}

type Sender struct {
	Addr      net.UDPAddr
	id        string
	FromAddr  net.Addr
	Connected bool
	DataChan  chan interface{}
	QuitChan  chan int
	ReadyChan chan int
}

func (l *Listener) Listen() {
	var listener *quic.Listener
	var err error
	listenConfig := quic.Config{KeepAlivePeriod: time.Second * 5}
	for {
		listener, err = quic.ListenAddr(l.Addr.String(), generateTLSConfig(), &listenConfig)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		conn, err := listener.Accept(ctx)
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
	var id string
	shouldQuit := false
	var stream quic.ReceiveStream
	var err error
	for {
		stream, err = conn.AcceptUniStream(context.Background())
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

	go func() {
		for _id := range l.QuitChan {
			fmt.Println(_id)
			fmt.Println(id)
			if _id == id {
				shouldQuit = true
			}
		}
	}()

	for {
		if shouldQuit {
			fmt.Println("Closing connection...")
			return
		}
		stream.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := stream.Read(buffer)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				fmt.Println("Timed out")
				continue
			}

			// TODO: handle other kind of timeout as well

			if ierr, ok := err.(*quic.ApplicationError); ok {
				fmt.Println(ierr)
				fmt.Println("Closing from application error (Might be due to high packet loss)")
				return
			}

			fmt.Println("Failed to read from stream from", conn.RemoteAddr())
			fmt.Println(err)
			continue
		}

		if string(buffer[0:len(InitMessage)]) == InitMessage {
			id = string(buffer[len(InitMessage):n])
			continue
		}
		var result interface{}
		json.Unmarshal(buffer[0:n], &result)

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

// take id as parameter
func (s *Sender) Send() {
	// This is copied from https://github.com/quic-go/quic-go/blob/master/example/echo/echo.go
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"foo"},
	}
	quicConf := quic.Config{
		InitialStreamReceiveWindow:     10 * 1024 * 1024,
		MaxStreamReceiveWindow:         10 * 1024 * 1024,
		InitialConnectionReceiveWindow: 15 * 1024 * 1024,
		MaxConnectionReceiveWindow:     15 * 1024 * 1024,

		MaxIdleTimeout: 30 * time.Second,

		EnableDatagrams: true,

		KeepAlivePeriod: time.Second * 25,
	}
	var conn quic.Connection
	var stream quic.SendStream
	var err error

	// BIG problem: When there is packet loss on a port we get the (seemingly unavoidable)
	// permission error. However, we cannot simply send to a different port as this port
	// will not be the same one that the system is listening to

	// That's no good...

	// TODO: We have process pairs. Use process pairs (don't be scared to crash if something
	// is not recoverable)
	for {
		conn, err = quic.DialAddr(context.Background(), s.Addr.String(), tlsConf, &quicConf)

		if err != nil {
			fmt.Println("Error when setting up QUIC connection:", err)
			fmt.Println("Retrying...")
			time.Sleep(time.Second)
			continue
		}
		defer conn.CloseWithError(applicationError, "Application error")

		stream, err = s.makeStream(conn)
		if err != nil {
			fmt.Println("Error when making stream, retrying....")
			time.Sleep(time.Second)
			continue
		}

		break
	}

	s.FromAddr = conn.LocalAddr()
	fmt.Printf("---- SENDER %s--->%s CONNECTED ----\n", conn.LocalAddr(), conn.RemoteAddr())
	s.ReadyChan <- 1

	// Problem: We get the permission denied error message
	for {
		select {
		case <-s.QuitChan:
			fmt.Printf("Closing Send connection to %s...\n", &s.Addr)
			stream.Close()
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
				if errors.Is(err, os.ErrPermission) {
					// fmt.Println("Time to")
					for {
					}
					// stream.CancelWrite(quic.StreamErrorCode(quic.NoError))
					// s.QuitChan <- 1
					continue
				}
				continue
			}
		}
	}
}

func (s *Sender) makeStream(conn quic.Connection) (quic.SendStream, error) {
	stream, err := conn.OpenUniStream()
	if err != nil {
		return stream, err
	}
	stream.Write([]byte(fmt.Sprintf("%s%s", InitMessage, s.id))) // Replace with id message
	return stream, nil
}

func (s Sender) String() string {
	return fmt.Sprintf("P2P sender object \n ~ peer address: %s", &s.Addr)
}

func NewListener(addr net.UDPAddr) Listener {
	return Listener{
		Addr:      addr,
		ReadyChan: make(chan int),
		QuitChan:  make(chan string),
		DataChan:  make(chan interface{}),
	}
}

func NewSender(addr net.UDPAddr, id string) Sender {
	return Sender{
		id:        id,
		Addr:      addr,
		Connected: false,
		DataChan:  make(chan interface{}),
		QuitChan:  make(chan int, 1),
		ReadyChan: make(chan int),
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
		NextProtos:   []string{"foo"},
	}
}
