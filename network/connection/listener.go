package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	quic "github.com/quic-go/quic-go"
)

const (
	bufSize     = 1024
	readTimeout = time.Millisecond * 1000
)

type Listener struct {
	listener      *quic.Listener
	LostPeersLock *sync.Mutex
	Addr          net.UDPAddr
	DataChan      chan Message
	LostPeersChan chan string
	LostPeers     map[string]bool
	lostPeersLock *sync.Mutex
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

	// Problem: Sometimes when disconnecting a peer (when does this really happen?)
	// the sending for some reason becomes really really slow, however note
	// that the other things are not slow

	go func() {
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
	}()

	for lostPeer := range l.LostPeersChan {
		fmt.Println("Losing peer")
		l.lostPeersLock.Lock()
		l.LostPeers[lostPeer] = true
		l.lostPeersLock.Unlock()
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
		l.lostPeersLock.Lock()
		if l.LostPeers[connectionId] {
			fmt.Println("Closing listener connection from", conn.RemoteAddr())
			l.LostPeers[connectionId] = false
			l.lostPeersLock.Unlock()
			return
		}
		l.lostPeersLock.Unlock()

		stream.SetReadDeadline(time.Now().Add(readTimeout))
		n, err := stream.Read(buffer)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}

			if ierr, ok := err.(*quic.ApplicationError); ok {
				// TODO: Find out why we get an application error every time we close
				// the program
				fmt.Println(`Application error encountered, probably an error on the 
					sender side:`, err)
				panic(ierr)
			}

			if errors.Is(err, io.EOF) {
				fmt.Println("Exiting due to EOF")
				return
			}

			fmt.Println("Failed to read from stream", stream.StreamID())
			fmt.Println(err)
			continue
		}

		if string(buffer[0:len(InitMessage)]) == InitMessage {
			connectionId = string(buffer[len(InitMessage):n])
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

// TODO: Check out go's context thingy
// func (l *Listener) readFromStream(stream quic.ReceiveStream, idChan chan string, streamQuitChan chan int) {

// 	}
// }

// func (l *Listener) handleConnection(conn quic.Connection) {
// 	var connectionId string // ID of the peer that is sending data to this connection
// 	stream, err := conn.AcceptUniStream(context.Background())
// 	if err != nil {
// 		fmt.Println("Could not open data stream from:", conn.RemoteAddr())
// 		log.Fatal("Error:", err)
// 	}

// 	idChan := make(chan string)
// 	streamQuitChan := make(chan int)

// 	go l.readFromStream(stream, idChan, streamQuitChan)

// 	// Problem: When an id is sent into the idChan, we have no way of deciding which
// 	// connection it should be sent to
// 	// This may require shared variables as there is otherwise no way to share

// 	for {
// 		select {
// 		case id := <-idChan:
// 			connectionId = id
// 			fmt.Println("This stream is listening to", connectionId)
// 		default:
// 			l.lostPeersLock.Lock()
// 			if l.LostPeers[connectionId] {
// 				fmt.Println("Closing listener connection from", conn.RemoteAddr())
// 				l.LostPeers[connectionId] = false
// 				streamQuitChan <- 1
// 				return
// 			}
// 			l.lostPeersLock.Unlock()
// 		}
// 	}
// }

// // TODO: Check out go's context thingy
// func (l *Listener) readFromStream(stream quic.ReceiveStream, idChan chan string, streamQuitChan chan int) {
// 	buffer := make([]byte, bufSize)

// 	for {
// 		select {
// 		case <-streamQuitChan:
// 			fmt.Println("Exitiing stream read loop")
// 			return
// 		default:
// 			stream.SetReadDeadline(time.Now().Add(readTimeout))
// 			n, err := stream.Read(buffer)
// 			if err != nil {
// 				if errors.Is(err, os.ErrDeadlineExceeded) {
// 					continue
// 				}

// 				if ierr, ok := err.(*quic.ApplicationError); ok {
// 					// TODO: Find out why we get an application error every time we close
// 					// the program
// 					fmt.Println(`Application error encountered, probably an error on the
// 					sender side:`, err)
// 					panic(ierr)
// 				}

// 				if errors.Is(err, io.EOF) {
// 					fmt.Println("Exiting due to EOF")
// 					return
// 				}

// 				fmt.Println("Failed to read from stream", stream.StreamID())
// 				fmt.Println(err)
// 				continue
// 			}

// 			if string(buffer[0:len(InitMessage)]) == InitMessage {
// 				idChan <- string(buffer[len(InitMessage):n])
// 				continue
// 			}
// 			var result Message
// 			err = json.Unmarshal(buffer[0:n], &result)
// 			if err != nil {
// 				fmt.Println("Error when unmarshaling network message")
// 				continue
// 			}

// 			l.DataChan <- result
// 		}
// 	}
// }

func NewListener(addr net.UDPAddr) *Listener {
	return &Listener{
		LostPeersLock: &sync.Mutex{},
		LostPeersChan: make(chan string),
		LostPeers:     make(map[string]bool),
		Addr:          addr,
		DataChan:      make(chan Message),
		lostPeersLock: &sync.Mutex{},
	}
}
