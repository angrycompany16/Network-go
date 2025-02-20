package transfer

import (
	"bufio"
	"fmt"
	"log"
	"net"
)

// TODO: Do something about the EOF stuff that gets returned whenever a peer has disconnected but not
// 		 yet timed out
// TODO: Make constructors for sender and listener

// Probably a stupid name
type ListenSocket struct {
	listener  *net.TCPListener
	Addr      net.TCPAddr
	ReadyChan chan int
}

type Sender struct {
	DataChan  chan int
	QuitChan  chan int
	ReadyChan chan int
	HostAddr  net.TCPAddr
	PeerAddr  net.TCPAddr
}

type Receiver struct {
	listener  *ListenSocket
	QuitChan  chan int
	ReadyChan chan int
}

func (p *Sender) Send() {
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

func (l *ListenSocket) Listen() {
	listener, err := net.ListenTCP("tcp4", &l.Addr)
	if err != nil {
		println("Listener fucked up")
		log.Fatal(err)
	}

	defer listener.Close()
	fmt.Println("Listener ready on port", l.Addr.Port)
	l.listener = listener
	l.ReadyChan <- 1
	for {
	}
}

func (lc *Receiver) Listen() error {
	conn, err := lc.listener.listener.Accept()
	fmt.Println("passed?")
	if err != nil {
		fmt.Println("Error when connecting listener over TCP")
		fmt.Println(err)
		return err
	}

	fmt.Println("---- LISTENER CONNECTED ----")
	lc.ReadyChan <- 1

	for {
		select {
		case <-lc.QuitChan:
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

func (s Sender) String() string {
	return fmt.Sprintln(
		"P2P sender object\n",
		fmt.Sprintf("~ host address: %s\n", &s.HostAddr),
		fmt.Sprintf("~ peer address: %s", &s.PeerAddr),
	)
}

func (l Receiver) String() string {
	return fmt.Sprintln(
		"P2P listener object",
		fmt.Sprintf("~ listening on: %s", &l.listener.Addr),
	)
}

func NewSender(hostAddr net.TCPAddr, peerAddr net.TCPAddr) Sender {
	return Sender{
		DataChan:  make(chan int),
		QuitChan:  make(chan int),
		ReadyChan: make(chan int),
		HostAddr:  hostAddr,
		PeerAddr:  peerAddr,
	}
}

func NewListenConnection(listener *ListenSocket) Receiver {
	return Receiver{
		listener:  listener,
		QuitChan:  make(chan int),
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
