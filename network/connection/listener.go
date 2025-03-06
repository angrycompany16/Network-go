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
	bufSize     = 1024
	readTimeout = time.Millisecond * 1000
)

type Listener struct {
	listener *quic.Listener
	// lostPeersLock *sync.Mutex
	Addr      net.UDPAddr
	DataChan  chan Message
	LostPeers map[string]bool
}

func (l *Listener) Init() {
	listenConfig := quicConfig
	listenConfig.KeepAlivePeriod = time.Second * 5
	listener, err := quic.ListenAddr(l.Addr.String(), generateTLSConfig(), &listenConfig)
	if err != nil {
		log.Fatal("Encountered error when setting up listener:", err)
	}
	l.listener = listener
	fmt.Println("Listener ready on port", l.Addr.Port)
}

func (l *Listener) Listen() {
	defer l.listener.Close()

	for {
		conn, err := l.listener.Accept(context.Background())
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
	var connectionId string // ID of the peer that is sending data to this connection
	stream, err := conn.AcceptUniStream(context.Background())
	if err != nil {
		fmt.Println("Could not open data stream from:", conn.RemoteAddr())
		log.Fatal("Error:", err)
	}

	buffer := make([]byte, bufSize)

	for {
		if l.LostPeers[connectionId] {
			fmt.Println("Closing listener connection from", conn.RemoteAddr())
			// NOTE: This may actually need a lock or something for protection as it is a shared variable
			l.LostPeers[connectionId] = false
			return
		}

		stream.SetReadDeadline(time.Now().Add(readTimeout))
		n, err := stream.Read(buffer)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}

			// TODO: handle other kind of timeout as well
			if ierr, ok := err.(*quic.ApplicationError); ok {
				fmt.Println(`Application error encountered, probably an error on the 
					sender side:`, ierr)
				return
			}

			fmt.Println("Failed to read from stream from", conn.RemoteAddr())
			fmt.Println(err)
			continue
		}

		if string(buffer[0:len(InitMessage)]) == InitMessage {
			connectionId = string(buffer[len(InitMessage):n])
			// TODO: Not beautiful... but it seems to avoid the simultaneous read and write problem
			time.Sleep(readTimeout * 2)
			// Wait for the connection from this peer to be removed if it exists
			// for {
			// 	if !l.LostPeers[connectionId] {
			// 		break
			// 	}
			// }

			continue
		}
		var result Message
		err = json.Unmarshal(buffer[0:n], &result)
		if err != nil {
			fmt.Println("Error when unmarshaling network message")
			continue
		}

		l.DataChan <- result
	}
}

func NewListener(addr net.UDPAddr) *Listener {
	return &Listener{
		// lostPeersLock: &sync.Mutex{},
		LostPeers: make(map[string]bool),
		Addr:      addr,
		DataChan:  make(chan Message),
	}
}
