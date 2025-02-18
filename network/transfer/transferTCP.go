package transfer

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"syscall"
)

// TODO: Make constructors for sender and listener

type P2PSender struct {
	DataChan  chan int
	QuitChan  chan int
	ReadyChan chan int
	HostAddr  net.TCPAddr
	PeerAddr  net.TCPAddr
}

type P2PListener struct {
	QuitChan  chan int
	Addr      net.TCPAddr
	ReadyChan chan int
}

// Send data to the peer
func (p *P2PSender) Send() {
	conn, err := net.DialTCP("tcp4", &p.HostAddr, &p.PeerAddr)
	if err != nil {
		fmt.Println("Error when connecting via TCP")
		fmt.Println(err)
		return
	}
	defer conn.Close()

	fmt.Println("---- SENDER CONNECTED ----")
	fmt.Printf("Ports: %d ----> %d\n", p.HostAddr.Port, p.PeerAddr.Port)
	p.ReadyChan <- 1

	for {
		select {
		case <-p.QuitChan:
			fmt.Println("Closing Send connection...")
			// Close the connection peacefully and stop the goroutine
			return
		case <-p.DataChan:
			fmt.Println("Sending data to port ", p.PeerAddr.Port)

			_, err := conn.Write([]byte("Test message. Did i arrive?\n"))
			if err != nil {
				fmt.Println("Could not send TCP data")
				fmt.Println(err)
				continue
			}
		}
	}
}

func (p *P2PListener) Listen() error {
	// We might not need this actually
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				err := syscall.SetsockoptInt(
					int(fd),
					syscall.SOL_SOCKET,
					syscall.SO_REUSEADDR,
					1,
				)
				if err != nil {
					fmt.Printf("Failed to set SO_REUSEADDR: %v", err)
				}
			})
		},
	}
	// TODO: This should be the normal mode of operation. Make this the default
	// If a port fails, simply try again with a new port
	var listener net.Listener
	var err error
	for {
		listener, err = lc.Listen(context.Background(), "tcp4", p.Addr.String())
		if err != nil {
			port, _ := GetAvailablePort()
			p.Addr.Port = port
			fmt.Println("Error when setting up listener over TCP")
			fmt.Println(err)
			return err
		} else {
			break
		}
	}
	defer listener.Close()
	fmt.Println("Listening on port", p.Addr.Port)
	p.ReadyChan <- 1
	// p.Ready = true

	// TODO: Turn this into a for loop in case things go wrong
	conn, err := listener.Accept()
	if err != nil {
		fmt.Println("Error when connecting listener over TCP")
		fmt.Println(err)
		return err
	}

	fmt.Println("---- LISTENER CONNECTED ----")
	p.ReadyChan <- 1

	for {
		select {
		case <-p.QuitChan:
			// Graceful shutdown
			fmt.Println("Closing Listener connection...")
			return nil
		default:
			fmt.Print("")
			data, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				fmt.Println("Encountered error when reading data: ")
				fmt.Println(err)
				continue
			}
			// Send into channl
			fmt.Println("Received data: ")
			fmt.Println(data)
		}
	}
}

/*
	type P2PConnection struct {
		DataChan       chan int
		QuitChan       chan int
		ReadyChan      chan int
		hostSendAddr   *net.TCPAddr
		hostListenAddr *net.TCPAddr
		peerListenAddr *net.TCPAddr
	}

// Send data to the peer

	func (p *P2PConnection) Send() {
		conn, err := net.DialTCP("tcp4", p.hostSendAddr, p.peerListenAddr)
		if err != nil {
			fmt.Println("Error when connecting via TCP")
			fmt.Println(err)
			return
		}
		defer conn.Close()

		fmt.Println("---- CONNECTIONG INITIALIZED SENDER SIDE ----")
		p.ReadyChan <- 1

		for {
			select {
			case <-p.QuitChan:
				fmt.Println("Closing Send connection...")
				// Close the connection peacefully and stop the goroutine
				return
			case <-p.DataChan:
				fmt.Println("Sending data to port ", p.peerListenAddr.Port)

				_, err := conn.Write([]byte("Test message. Did i arrive?\n"))
				if err != nil {
					fmt.Println("Could not send TCP data")
					fmt.Println(err)
					continue
				}
			}
		}
	}

// Listen to what the peer sends back

	func (p *P2PConnection) Recv() error {
		// addr := net.TCPAddr{
		// 	IP: p.peerAddr.IP,
		// }
		listener, err := net.ListenTCP("tcp4", p.hostListenAddr)
		if err != nil {
			fmt.Println("Error when setting up listener over TCP")
			fmt.Println(err)
			return err
		}
		defer listener.Close()
		fmt.Println("Listening on port", p.hostListenAddr.Port)
		p.ReadyChan <- 1

		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error when connecting listener over TCP")
			fmt.Println(err)
			return err
		}

		fmt.Println("---- CONNECTION INITIALIZED LISTENER SIDE ----")
		p.ReadyChan <- 1

		buffer := make([]byte, 1024)

		for {
			select {
			case <-p.QuitChan:
				// Graceful shutdown
				fmt.Println("Closing Receive connection...")
				return nil
			default:
				n, err := conn.Read(buffer)
				// data, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil {
					fmt.Println("Encountered error when reading data: ")
					fmt.Println(err)
					continue
				}
				// Send into channl
				fmt.Println("Received data: ")
				fmt.Println(string(buffer[:n]))
			}
		}
	}

// Should not really need the listenAddr, at some point it must be replaced by
// port 0, but idk how to make it work :^)

	func NewConnection(hostSendAddr *net.TCPAddr, hostListenAddr *net.TCPAddr, peerListenAddr *net.TCPAddr) *P2PConnection {
		fmt.Println("-------------------------------------------")
		fmt.Printf("Setting up new TCP connection between %s and %s", hostSendAddr.String(), peerListenAddr.String())
		fmt.Println("-------------------------------------------")
		return &P2PConnection{
			DataChan:       make(chan int),
			QuitChan:       make(chan int),
			ReadyChan:      make(chan int),
			hostListenAddr: hostListenAddr,
			hostSendAddr:   hostSendAddr,
			peerListenAddr: peerListenAddr,
		}
	}
*/
func GetAvailablePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp4", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}
