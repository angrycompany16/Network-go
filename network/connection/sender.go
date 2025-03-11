package connection

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	quic "github.com/quic-go/quic-go"
)

type Sender struct {
	stream   quic.SendStream
	conn     quic.Connection
	id       string
	Addr     net.UDPAddr
	DataChan chan interface{}
	QuitChan chan int
}

func (s *Sender) Init() {
	// TODO: ensure that communication between computers actually works (Critical!)
	// This is copied from https://github.com/quic-go/quic-go/blob/master/example/echo/echo.go
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"foo"},
	}

	conn, err := quic.DialAddr(context.Background(), s.Addr.String(), tlsConf, &quicConfig)

	if err != nil {
		log.Fatal("Error when setting up QUIC connection:", err)
	}
	s.conn = conn

	stream, err := s.makeStream(conn)
	if err != nil {
		log.Fatal("Error when making stream:", err)
	}

	s.stream = stream
	fmt.Printf("---- SENDER %s--->%s CONNECTED ----\n", conn.LocalAddr(), conn.RemoteAddr())
}

func (s *Sender) Send() {
	defer s.stream.Close()
	defer s.conn.CloseWithError(applicationError, "Application error")
	for {
		select {
		case <-s.QuitChan:
			fmt.Printf("Closing Send connection to %s...\n", &s.Addr)
			s.stream.Close()
			return
		case data := <-s.DataChan:
			fmt.Println("Sending data to ", s.Addr.String())

			jsonData, err := json.Marshal(NewMessage(data))
			if err != nil {
				fmt.Println("Could not marshal data:", data)
				fmt.Println("Error:", err)
				continue
			}

			if len(jsonData) > bufSize {
				fmt.Printf(
					"Tried to send a message longer than the buffer size (length: %d, buffer size: %d)\n\t'%s'\n"+
						"Either send smaller packets, or go to network/transfer/transferQUIC.go and increase the buffer size",
					len(jsonData), bufSize, string(jsonData))
				continue
			}

			_, err = s.stream.Write(jsonData)
			if err != nil {
				fmt.Println("Could not send data over stream")
				fmt.Println(err)
				if errors.Is(err, os.ErrPermission) {
					fmt.Println("The unrecoverable error has been encountered. Time to die!")
					panic(err)
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
	stream.Write([]byte(fmt.Sprintf("%s%s", InitMessage, s.id)))
	return stream, nil
}

func NewSender(addr net.UDPAddr, id string) Sender {
	return Sender{
		id:       id,
		Addr:     addr,
		DataChan: make(chan interface{}),
		QuitChan: make(chan int, 1),
	}
}
