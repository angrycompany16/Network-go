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

// TODO: When adding a third elevator, the peer for some reason takes a very long time
// to be detected. Why could this be?
// Fucking everything in one file yeah this is great wooh i love life

const (
	stateBroadcastPort = 36251 // Akkordrekke
)

var (
	timeout = time.Second * 5 // Timeout period before deleting peer
)

type LifeSignal struct {
	ListenerAddr net.TCPAddr
	SenderId     string
	State        ElevatorState   // State of this peer
	WorldView    []ElevatorState // State of other peers
}

type elevator struct {
	id        string
	name      string // For debugging purposes
	state     ElevatorState
	ip        net.IP
	listener  transfer.ListenSocket
	peers     []*peer
	peersLock *sync.Mutex
}

// Any other elevator
type peer struct {
	Sender   transfer.Sender
	Listener transfer.Receiver
	state    ElevatorState
	id       string
	lastSeen time.Time
}

type ElevatorState struct {
	Connected map[string]bool
	Foo       int
	Busy      bool
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
				fmt.Printf("\nRemove peer: \n%#v\n", peer)
				// TODO: make this work. Should be a small feat.
				// Close the connnection
				// Make sure to only use this when TCP is fully implemented
				// peer.Sender.QuitChan <- 1
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
			ListenerAddr: e.listener.Addr,
			SenderId:     e.id,
			State:        e.state,
		}

		for _, peer := range e.peers {
			signal.WorldView = append(signal.WorldView, peer.state)
		}

		signalChan <- signal
		time.Sleep(time.Millisecond * 10)
	}
}

// TODO: Fix all the todos and also make this a bit more separated so it's actually possible to understand
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

				// We want to connect that boy
				connectionState, ok := _peer.state.Connected[e.id]
				// TODO: handle !ok
				if !connectionState && ok && !e.state.Connected[_peer.id] {
					go _peer.Sender.Send()
					go _peer.Listener.Listen()

					<-_peer.Sender.ReadyChan
					<-_peer.Listener.ReadyChan
					e.state.Connected[_peer.id] = true
				}

				e.peersLock.Unlock()

				continue LifeSignals
			}
		}

		sender := transfer.NewSender(
			net.TCPAddr{
				IP:   e.ip,
				Port: transfer.GetAvailablePort(),
			},
			lifeSignal.ListenerAddr,
		)

		listener := transfer.NewListenConnection(&e.listener)

		newPeer := newPeer(sender, listener, lifeSignal.State, lifeSignal.SenderId)
		// it FUCKING WIRKS!!!!

		e.state.Connected[newPeer.id] = false

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
		listener: transfer.ListenSocket{
			Addr: net.TCPAddr{
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
		Connected: make(map[string]bool),
		Foo:       Foo,
		Busy:      false,
	}
}

func newPeer(sender transfer.Sender, listener transfer.Receiver, state ElevatorState, id string) *peer {
	return &peer{
		Sender:   sender,
		Listener: listener,
		state:    state,
		id:       id,
		lastSeen: time.Now(),
	}
}

// Sub-optimal
func (e elevator) String() string {
	return fmt.Sprint(
		fmt.Sprintf("------- Elevator %s----\n", e.name),
		fmt.Sprintf(" ~ id: %s\n", e.id),
		fmt.Sprintf(" ~ listening on: %s", &e.listener.Addr),
	)
}

// Sub-optimal
// TODO: improve using multiline strings
func (p peer) String() string {
	return fmt.Sprint(
		fmt.Sprintf("------- Peer %s----\n", p.id),
		fmt.Sprintf(" ~ Sender: %s\n", p.Sender),
		fmt.Sprintf(" ~ Listener: %s", p.Listener),
		// not very important
		// fmt.Sprintf(" ~ State: %s\n", p.State),
		// fmt.Sprintf(" ~ Last seen: %s\n", p.LastSeen),
	)
}
