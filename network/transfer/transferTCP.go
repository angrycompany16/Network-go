package transfer

import (
	"bufio"
	"fmt"
	"net"
)

// TODO: Do something about the EOF stuff that gets returned whenever a peer has disconnected but not
// 		 yet timed out
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
	ReadyChan chan int
	Addr      net.TCPAddr
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

// BIG problem: nothing works
// Make it fix
func (p *P2PListener) Listen() error {
	var listener net.Listener
	var err error
	for {
		listener, err = net.ListenTCP("tcp4", &p.Addr)
		if err != nil {
			// TODO: check that the error was bind: already in use
			// Problem:
			port := GetAvailablePort()
			p.Addr.Port = port
			fmt.Println("Error when setting up listener over TCP")
			fmt.Println(err)
			// return err
		} else {
			break
		}
	}

	// // We might not need this actually
	// lc := net.ListenConfig{
	// 	Control: func(network, address string, c syscall.RawConn) error {
	// 		return c.Control(func(fd uintptr) {
	// 			err := syscall.SetsockoptInt(
	// 				int(fd),
	// 				syscall.SOL_SOCKET,
	// 				syscall.SO_REUSEADDR,
	// 				1,
	// 			)
	// 			if err != nil {
	// 				fmt.Printf("Failed to set SO_REUSEADDR: %v", err)
	// 			}
	// 		})
	// 	},
	// }
	// // TODO: This should be the normal mode of operation. Make this the default
	// // If a port fails, simply try again with a new port
	// var listener net.Listener
	// var err error
	// for {
	// 	listener, err = lc.Listen(context.Background(), "tcp4", p.Addr.String())
	// 	if err != nil {
	// 		port := GetAvailablePort()
	// 		p.Addr.Port = port
	// 		fmt.Println("Error when setting up listener over TCP")
	// 		fmt.Println(err)
	// 		return err
	// 	} else {
	// 		break
	// 	}
	// }
	defer listener.Close()
	fmt.Println("Listener ready on port", p.Addr.Port)
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

func (s P2PSender) String() string {
	return fmt.Sprintln(
		"P2P sender object",
		fmt.Sprintf("~ host address: %s\n", &s.HostAddr),
		fmt.Sprintf("~ peer address: %s", &s.PeerAddr),
	)
}

func (l P2PListener) String() string {
	return fmt.Sprintln(
		"P2P listener object",
		fmt.Sprintf("~ listening on: %s", &l.Addr),
	)
}

func NewSender(hostAddr net.TCPAddr, peerAddr net.TCPAddr) P2PSender {
	return P2PSender{
		DataChan:  make(chan int),
		QuitChan:  make(chan int),
		ReadyChan: make(chan int),
		HostAddr:  hostAddr,
		PeerAddr:  peerAddr,
	}
}

func NewListener(addr net.TCPAddr) P2PListener {
	return P2PListener{
		QuitChan:  make(chan int),
		ReadyChan: make(chan int),
		Addr:      addr,
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
