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

// Custom config for improved handling of packet loss

type Sender struct {
	Addr      net.UDPAddr
	id        string
	DataChan  chan interface{}
	QuitChan  chan int
	ReadyChan chan int
}

func (s *Sender) Send() {
	// This is copied from https://github.com/quic-go/quic-go/blob/master/example/echo/echo.go
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"foo"},
	}
	// TODO: CLEAN.UP.

	conn, err := quic.DialAddr(context.Background(), s.Addr.String(), tlsConf, &quicConfig)

	if err != nil {
		log.Fatal("Error when setting up QUIC connection:", err)
	}
	defer conn.CloseWithError(applicationError, "Application error")

	stream, err := s.makeStream(conn)
	if err != nil {
		log.Fatal("Error when making stream:", err)
	}

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

			jsonData, err := json.Marshal(newMessage(data))
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

			_, err = stream.Write(jsonData)
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
	stream.Write([]byte(fmt.Sprintf("%s%s", InitMessage, s.id))) // Replace with id message
	return stream, nil
}

func NewSender(addr net.UDPAddr, id string) Sender {
	return Sender{
		id:        id,
		Addr:      addr,
		DataChan:  make(chan interface{}),
		QuitChan:  make(chan int, 1),
		ReadyChan: make(chan int),
	}
}
