package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/angrycompany16/Network-go/network/localip"
	"github.com/angrycompany16/Network-go/network/transfer"
	"github.com/eiannone/keyboard"
)

// NOTE:
// https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes
// Read this if the buffer size warning appears
// TL;DR
// Run
// sudo sysctl -w net.core.rmem_max=7500000
// and
// sudo sysctl -w net.core.wmem_max=7500000

const (
	stateBroadcastPort = 36251 // Akkordrekke
)

var (
	timeout = time.Second * 5
)

type LifeSignal struct {
	ConnectionMap map[string]bool // Map of connection state of peers
	ListenerAddr  net.UDPAddr     // I am listening on this address, send to this
	SenderId      string
	State         ElevatorState   // State of this peer
	WorldView     []ElevatorState // State of other peers
}

type elevator struct {
	id        string
	name      string // For debugging purposes
	state     ElevatorState
	ip        net.IP
	listener  transfer.Listener
	peers     []*peer
	peersLock *sync.Mutex
}

// Any other elevator
type peer struct {
	Sender transfer.Sender // Used for sending data to this peer
	// Listener transfer.Receiver
	// Copnnection
	state    ElevatorState
	id       string
	lastSeen time.Time
}

type ElevatorState struct {
	// Connected map[string]bool
	Foo  int
	Busy bool
}

func main() {
	elevator := initElevator()

	lifeSignalChannel := make(chan LifeSignal)

	go transfer.BroadcastSender(stateBroadcastPort, lifeSignalChannel)
	go transfer.BroadcastReceiver(stateBroadcastPort, lifeSignalChannel)

	go elevator.timeout()
	go elevator.sendLifeSignal(lifeSignalChannel)
	go elevator.readLifeSignals(lifeSignalChannel)

	// Handle input and stuff for testing purposes
	for {
		if elevator.HandleDebugInput() {
			break
		}
	}
}

func (e *elevator) HandleDebugInput() bool {
	char, key, err := keyboard.GetSingleKey()
	if err != nil {
		log.Fatal(err)
	}

	if char == 'A' || char == 'a' {
		e.state.Foo++
		fmt.Println("Value foo update: ", e.state.Foo)
	}

	if char == 'S' || char == 's' {
		if len(e.peers) == 0 {
			fmt.Println("No peers!")
		}

		for i, peer := range e.peers {
			fmt.Println()
			fmt.Println("-------------------------------")
			fmt.Printf("Peer %d: %#v\n", i, peer)
			fmt.Println("-------------------------------")
		}
	}

	if char == 'B' || char == 'b' {
		e.state.Busy = !e.state.Busy
		fmt.Println("Busy updated to: ", e.state.Busy)
	}

	// Note: In an unbuffered channel, passing a value is blocking.
	// Therefore it is important to always have something listening to that channel,
	// as that will unblock the value pass
	e.peersLock.Lock()
	if char == 'C' || char == 'c' {
		if len(e.peers) == 0 {
			fmt.Println("No peers!")
		}

		for _, peer := range e.peers {
			peer.Sender.DataChan <- 5
		}
	}
	e.peersLock.Unlock()

	if key == keyboard.KeyCtrlC {
		fmt.Println("Exit")
		return true
	}
	return false
}

func (e *elevator) timeout() {
	for {
		e.peersLock.Lock()

		for i, peer := range e.peers {
			if peer.lastSeen.Add(timeout).Before(time.Now()) {
				fmt.Println("Removing peer:", peer)
				peer.Sender.QuitChan <- 1
				e.peers[i] = e.peers[len(e.peers)-1]
				e.peers = e.peers[:len(e.peers)-1]
			}
		}

		e.peersLock.Unlock()
	}
}

func (e *elevator) sendLifeSignal(signalChan chan (LifeSignal)) {
	for {
		signal := LifeSignal{
			ConnectionMap: make(map[string]bool),
			ListenerAddr:  e.listener.Addr,
			SenderId:      e.id,
			State:         e.state,
		}

		for _, peer := range e.peers {
			signal.ConnectionMap[peer.id] = peer.Sender.Connected
			signal.WorldView = append(signal.WorldView, peer.state)
		}

		signalChan <- signal
		time.Sleep(time.Millisecond * 10)
	}
}

func (e *elevator) readLifeSignals(signalChan chan (LifeSignal)) {
LifeSignals:
	for lifeSignal := range signalChan {
		if e.id == lifeSignal.SenderId {
			continue
		}

		e.peersLock.Lock()
		for _, _peer := range e.peers {
			if _peer.id == lifeSignal.SenderId {
				_peer.lastSeen = time.Now()
				_peer.state = lifeSignal.State

				// I think QUIC might be the best thing to have graced the earth with its existence
				// We want to connect that boy
				// Probably a good idea to check that the guy we're connecting to is also
				// trying to connect to us
				otherSenderConnected, ok := lifeSignal.ConnectionMap[e.id]
				if !ok {
					fmt.Println("fricked up")
					continue LifeSignals
				}
				// fmt.Println(lifeSignal.ConnectionMap)
				// fmt.Println(ok)
				if !_peer.Sender.Connected && !otherSenderConnected {
					fmt.Println("SEnding")
					go _peer.Sender.Send()

					<-e.listener.ReadyChan
					fmt.Println("Listener completed")
					<-_peer.Sender.ReadyChan
					fmt.Println("Sender completed")
					fmt.Println("Connection completed")
					// Need to also wait for the listener to be ready
					_peer.Sender.Connected = true
				}

				e.peersLock.Unlock()

				continue LifeSignals
			}
		}

		sender := transfer.NewSender(lifeSignal.ListenerAddr)

		newPeer := newPeer(sender, lifeSignal.State, lifeSignal.SenderId)

		e.peers = append(e.peers, newPeer)
		fmt.Println("New peer added: ")
		fmt.Println(newPeer)

		e.peersLock.Unlock()
	}
}

func initElevator() elevator {
	var id, name string
	flag.StringVar(&id, "id", "", "id of this peer")
	flag.StringVar(&name, "name", "", "name of this peer")

	flag.Parse()

	ip, err := localip.LocalIP()
	if err != nil {
		log.Fatal("Could not get local IP adress")
	}

	IP := net.ParseIP(ip)

	elevator := newElevator(id, name, IP, newElevatorState(0))

	// Open the TCP socket
	go elevator.listener.Listen()
	<-elevator.listener.ReadyChan

	fmt.Println("Successfully created new elevator: ")
	fmt.Println(elevator)

	return elevator
}

func newElevator(id string, name string, ip net.IP, state ElevatorState) elevator {
	return elevator{
		id:    id,
		name:  name,
		state: state,
		ip:    ip,
		listener: transfer.Listener{
			Addr: net.UDPAddr{
				IP:   ip,
				Port: transfer.GetAvailablePort(),
			},
			ReadyChan: make(chan int),
		},
		peers:     make([]*peer, 0),
		peersLock: &sync.Mutex{},
	}
}

func newElevatorState(Foo int) ElevatorState {
	return ElevatorState{
		Foo:  Foo,
		Busy: false,
	}
}

func newPeer(sender transfer.Sender, state ElevatorState, id string) *peer {
	return &peer{
		Sender:   sender,
		state:    state,
		id:       id,
		lastSeen: time.Now(),
	}
}

// Sub-optimal
func (e elevator) String() string {
	return fmt.Sprintf("------- Elevator %s----\n ~ id: %s\n ~ listening on: %s",
		e.name, e.id, &e.listener.Addr)
}

func (p peer) String() string {
	return fmt.Sprintf("------- Peer %s----\n ~ Sender:\n %s\n", p.id, p.Sender)
}
