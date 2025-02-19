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

// Fucking everything in one file yeah this is great wooh i love life

const (
	stateBroadcastPort = 36251 // Akkordrekke
)

var (
	timeout = time.Second * 5 // Timeout period before deleting peer
)

type ConnectionState int

const (
	Uninitialized ConnectionState = iota
	Listening
	Connected
)

type LifeSignal struct {
	ListenerAddr net.TCPAddr
	SenderId     string
	State        ElevatorState   // State of this peer
	WorldView    []ElevatorState // State of other peers
}

type elevator struct {
	id    string
	name  string // For debugging purposes
	state ElevatorState
	// Remove
	ip         net.IP
	ListenAddr net.TCPAddr // Broadcasted address used for telling the
	// other peers which port they should send to

	// Main address, used *only* for sending data (no, that's just not what it is)
	// ListenAddr net.TCPAddr
	peers     []*peer
	peersLock *sync.Mutex
}

type P2PConnection struct {
}

// Any other elevator
type peer struct {
	Sender   transfer.P2PSender
	Listener transfer.P2PListener
	state    ElevatorState
	id       string
	lastSeen time.Time
}

type ElevatorState struct {
	ConnectionStates map[string]ConnectionState
	Foo              int
	Busy             bool
}

// I am sane

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
		char, key, err := keyboard.GetSingleKey()
		if err != nil {
			log.Fatal(err)
		}

		if char == 'A' || char == 'a' {
			elevator.state.Foo++
			fmt.Println("Value foo update: ", elevator.state.Foo)
		}

		if char == 'S' || char == 's' {
			if len(elevator.peers) == 0 {
				fmt.Println("No peers!")
			}

			for _, peer := range elevator.peers {
				fmt.Println()
				fmt.Println("-------------------------------")
				fmt.Println(peer)
				fmt.Println("-------------------------------")
			}
		}

		if char == 'B' || char == 'b' {
			elevator.state.Busy = !elevator.state.Busy
			fmt.Println("Busy updated to: ", elevator.state.Busy)
		}

		// Note: In an unbuffered channel, passing a value is blocking.
		// Therefore it is important to always have something listening to that channel,
		// as that will unblock the value pass
		elevator.peersLock.Lock()
		if char == 'C' || char == 'c' {
			if len(elevator.peers) == 0 {
				fmt.Println("No peers!")
			}

			for _, peer := range elevator.peers {
				peer.Sender.DataChan <- 5
			}
		}
		elevator.peersLock.Unlock()

		if key == keyboard.KeyCtrlC {
			fmt.Println("Exit")
			break
		}
	}
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
			ListenerAddr: e.ListenAddr,
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

// Big problem: When we add a new node into a network which already has multiple existing nodes,
// the added node will broadcast that it's listening on the same port to all the nodes, causing
// them to all try to connect to the same port. This is a problem.

// How to resolve?
// - Either we can connect via UDP first and then set up all the communication channels
// - Somehow enforce sequentiality

// what this does
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
				connectionState, ok := _peer.state.ConnectionStates[e.id]
				if connectionState == Listening && ok && e.state.ConnectionStates[_peer.id] == Listening {
					// We can safely set the ports again because we know that both sides of the connection
					// have entered the listening state and thus established all port values
					// (This just kind of doesn't matter with the new / better port usage)

					// newSendPort, err := transfer.GetAvailablePort()
					// // TODO: fix
					// if err != nil {
					// 	log.Fatal(err)
					// }
					// e.SendAddr.Port = newSendPort

					// fmt.Println("New send port:")
					// fmt.Println(newSendPort)

					// newListenPort, err := transfer.GetAvailablePort()
					// // TODO: fix
					// if err != nil {
					// 	log.Fatal(err)
					// }
					e.ListenAddr.Port = transfer.GetAvailablePort()

					// fmt.Println("New listening port")
					// fmt.Println(newListenPort)

					// fmt.Println("Sender host port", _peer.Sender.HostAddr.Port)
					// fmt.Println("Sender peer port", _peer.Sender.PeerAddr.Port)
					// fmt.Println("Listener port", _peer.Listener.Addr.Port)
					go _peer.Sender.Send()

					<-_peer.Sender.ReadyChan
					<-_peer.Listener.ReadyChan
					e.state.ConnectionStates[_peer.id] = Connected
				}

				e.peersLock.Unlock()

				continue LifeSignals
			}
		}

		// Ensure that only one connection is initialized at a time?
		// for k, v := range e.state.ConnectionStates {
		// 	if v == Listening {
		// 		fmt.Printf("connection from elevator %s to %s is in Listening state", e.id, k)
		// 		continue LifeSignals
		// 	}
		// }

		// Initialize ports
		sender := transfer.NewSender(
			net.TCPAddr{
				IP:   e.ip,
				Port: transfer.GetAvailablePort(),
			},
			lifeSignal.ListenerAddr,
		)

		listener := transfer.NewListener(e.ListenAddr)

		newPeer := newPeer(sender, listener, lifeSignal.State, lifeSignal.SenderId)

		go newPeer.Listener.Listen()
		// Wait for "new peer"-listener to return that it is ready
		<-newPeer.Listener.ReadyChan
		// Broadcast this to the rest of the system
		e.state.ConnectionStates[newPeer.id] = Listening

		e.peers = append(e.peers, newPeer)
		fmt.Println("New peer added: ")
		fmt.Println(newPeer)

		e.peersLock.Unlock()

		// TODO
		// After adding a new peer, we need to update the port we are
		// listening to as the one we just used will be taken
	}
}

// Intialize program tagged with 'elevatorprogram' elns
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

	// TODO: Improve the debugging / printing information
	// Use some kind of custom struct print() implementation idk how but it's probably possible
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
		ListenAddr: net.TCPAddr{
			IP:   ip,
			Port: transfer.GetAvailablePort(),
		},
		peers:     make([]*peer, 0),
		peersLock: &sync.Mutex{},
	}
}

func newElevatorState(Foo int) ElevatorState {
	return ElevatorState{
		ConnectionStates: make(map[string]ConnectionState),
		Foo:              Foo,
		Busy:             false,
	}
}

func newPeer(sender transfer.P2PSender, listener transfer.P2PListener, state ElevatorState, id string) *peer {
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
		fmt.Sprintf(" ~ listening on: %s", &e.ListenAddr),
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
