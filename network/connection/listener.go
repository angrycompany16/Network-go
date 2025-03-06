package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	quic "github.com/quic-go/quic-go"
)

const (
	bufSize = 1024
)

type Listener struct {
	Addr           net.UDPAddr
	ReadyChan      chan int
	QuitChan       chan string
	DataChan       chan Message
	ConnectionChan chan net.Addr
}

func (l *Listener) Listen() {
	listenConfig := quicConfig
	listenConfig.KeepAlivePeriod = time.Second * 5
	listener, err := quic.ListenAddr(l.Addr.String(), generateTLSConfig(), &listenConfig)
	if err != nil {
		log.Fatal("Encountered error when setting up listener:", err)
	}
	defer listener.Close()

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
	stream, err := conn.AcceptUniStream(context.Background())
	if err != nil {
		log.Fatal("Error when opening data stream from:", conn.RemoteAddr())
	}

	buffer := make([]byte, bufSize)

	// TODO: Would be nice to find a way to rewrite this
	go func() {
		for _id := range l.QuitChan {
			if _id == id {
				shouldQuit = true
			}
		}
	}()

	for {
		if shouldQuit {
			fmt.Println("Closing connection from", conn.RemoteAddr())
			return
		}
		stream.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := stream.Read(buffer)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}

			// TODO: handle other kind of timeout as well
			if ierr, ok := err.(*quic.ApplicationError); ok {
				log.Fatal("Application error encountered:", ierr)
			}

			fmt.Println("Failed to read from stream from", conn.RemoteAddr())
			fmt.Println(err)
			continue
		}

		if string(buffer[0:len(InitMessage)]) == InitMessage {
			id = string(buffer[len(InitMessage):n])
			continue
		}
		var result Message
		err = json.Unmarshal(buffer[0:n], &result)
		if err != nil {
			fmt.Println("Error when unmarshaling network message")
		}

		l.DataChan <- result
	}
}

func NewListener(addr net.UDPAddr) Listener {
	return Listener{
		Addr:      addr,
		ReadyChan: make(chan int),
		QuitChan:  make(chan string),
		DataChan:  make(chan Message),
	}
}
