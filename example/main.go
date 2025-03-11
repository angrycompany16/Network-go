package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"strconv"
	"time"

	"github.com/angrycompany16/Network-go/network/broadcast"
	"github.com/angrycompany16/Network-go/network/connection"
	"github.com/angrycompany16/Network-go/network/localip"
	"github.com/eiannone/keyboard"
	"github.com/mitchellh/mapstructure"
)

// Problem: at 90% packet loss peers time out even after five seconds

// NOTE:
// Read this if the buffer size warning appears
// https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes
// TL;DR
// Run
// sudo sysctl -w net.core.rmem_max=7500000
// and
// sudo sysctl -w net.core.wmem_max=7500000

// Problem: We want to read and write to / from the peers map all the time. How can we do
// this? One way is to use a mutex which locks the resource, however we can also
// set up a server pattern that allows

// TODO: Rewrite without using mutexes all the time?

// We are gonna rewrite with select

const (
	stateBroadcastPort = 36251 // Akkordrekke
)

var (
	timeout = time.Millisecond * 500
)

// Note that all members must be public
type LifeSignal struct {
	ListenerAddr net.UDPAddr
	SenderId     string
	State        ElevatorState
	WorldView    []ElevatorState
}

type ElevatorMsg struct {
	SenderId string
	Data     int
}

type node struct {
	id              string
	name            string
	state           ElevatorState
	ip              net.IP
	requestListener *connection.Listener
	peers           map[string]*peer
	lifesignalChan  chan LifeSignal // Thread communication channel
	keyEventChan    <-chan keyboard.KeyEvent
}

type peer struct {
	sender    connection.Sender
	state     ElevatorState
	id        string
	lastSeen  time.Time
	connected bool
}

type ElevatorState struct {
	Foo  int
	Busy bool
}

func main() {
	elevator := initElevator()

	elevator.lifesignalChan = make(chan LifeSignal)

	var err error
	elevator.keyEventChan, err = keyboard.GetKeys(10)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = keyboard.Close()
		fmt.Println("Exiting function")
	}()

	go broadcast.BroadcastSender(stateBroadcastPort, elevator.lifesignalChan)
	go broadcast.BroadcastReceiver(stateBroadcastPort, elevator.lifesignalChan)

	// Literally the only thread we need...
	// When you're a total fucking moron :^)
	for {
		if elevator.readLifeSignals() {
			fmt.Println("Exit2")
			break
		}
	}
}

// Problem: If i am reading the variable (sendLifeSignal) concurrently while writing
// to it (readLifeSignals, updates the peers variable and such), I get a race condition.

// Even worse, in the main function of the actual elevator program I will need to receive
// changes to the actual elevator state (backed up cab calls) in the readLifeSignals
// function.

func (n *node) makeLifeSignal() LifeSignal {
	signal := LifeSignal{
		ListenerAddr: n.requestListener.Addr,
		SenderId:     n.id,
		State:        n.state,
	}

	for _, peer := range n.peers {
		signal.WorldView = append(signal.WorldView, peer.state)
	}

	return signal
}

func (n *node) readLifeSignals() bool {
	select {
	case lifeSignal := <-n.lifesignalChan:
		if n.id == lifeSignal.SenderId {
			return false
		}

		_peer, ok := n.peers[lifeSignal.SenderId]
		if ok {
			_peer.lastSeen = time.Now()
			_peer.state = lifeSignal.State

			// I think QUIC might be the best thing to have graced the earth with its
			// existence
			// We want to connect that boy
			if !_peer.connected {
				n.ConnectPeer(_peer, lifeSignal)
				return false
			}

			if _peer.sender.Addr.Port != lifeSignal.ListenerAddr.Port {
				fmt.Printf("Sending to port %d, but peer is listening on port %d\n", _peer.sender.Addr.Port, lifeSignal.ListenerAddr.Port)
				_peer.sender.QuitChan <- 1
				n.ConnectPeer(_peer, lifeSignal)
			}

			return false
		}

		sender := connection.NewSender(lifeSignal.ListenerAddr, n.id)

		newPeer := newPeer(sender, lifeSignal.State, lifeSignal.SenderId)

		n.peers[lifeSignal.SenderId] = newPeer
		fmt.Println("New peer added: ")
		fmt.Println(newPeer)
	case _msg := <-n.requestListener.DataChan:
		var message connection.Message
		mapstructure.Decode(_msg, &message)

		var msg ElevatorMsg
		err := mapstructure.Decode(message.Data, &msg)

		if err != nil {
			log.Fatal("Could not decode elevator request:", err)
		}

		fmt.Printf("Received data %d from elevator %s\n", msg.Data, msg.SenderId)
	case keyEvent := <-n.keyEventChan:
		if keyEvent.Rune == 'A' || keyEvent.Rune == 'a' {
			n.state.Foo++
			fmt.Println("Value foo update: ", n.state.Foo)
		}

		if keyEvent.Rune == 'S' || keyEvent.Rune == 's' {
			if len(n.peers) == 0 {
				fmt.Println("No peers!")
			}

			for id, peer := range n.peers {
				fmt.Println()
				fmt.Println("-------------------------------")
				fmt.Printf("Peer %s: %#v\n", id, peer)
				fmt.Println("-------------------------------")
			}
		}

		if keyEvent.Rune == 'B' || keyEvent.Rune == 'b' {
			n.state.Busy = !n.state.Busy
			fmt.Println("Busy updated to: ", n.state.Busy)
		}

		if keyEvent.Rune == 'C' || keyEvent.Rune == 'c' {
			if len(n.peers) == 0 {
				fmt.Println("No peers!")
			}

			for _, peer := range n.peers {
				if !peer.connected {
					continue
				}
				msg := n.newMsg(n.state.Foo)
				peer.sender.DataChan <- msg
			}
		}

		if keyEvent.Key == keyboard.KeyCtrlC {
			fmt.Println("Exit")
			return true
		}
	default:
		// Send a heartbeat
		n.lifesignalChan <- n.makeLifeSignal()

		// Check for lost peers
		for _, peer := range n.peers {
			if peer.lastSeen.Add(timeout).Before(time.Now()) && peer.connected {
				fmt.Println("Lost peer:", peer)
				peer.connected = false
				peer.sender.QuitChan <- 1

				// n.requestListener.LostPeersLock.Lock()
				n.requestListener.LostPeersChan <- peer.id
				// n.requestListener.LostPeers[peer.id] = true
				// n.requestListener.LostPeersLock.Unlock()
			}
		}
	}
	return false
}

func (n *node) ConnectPeer(_peer *peer, lifeSignal LifeSignal) {
	_peer.sender.Addr = lifeSignal.ListenerAddr
	_peer.sender.Init()
	go _peer.sender.Send()
	_peer.connected = true
}

func (n *node) newMsg(data int) ElevatorMsg {
	return ElevatorMsg{
		Data:     data,
		SenderId: n.id,
	}
}

func initElevator() node {
	var id, name string
	flag.StringVar(&id, "id", "", "id of this peer")
	flag.StringVar(&name, "name", "", "name of this peer")

	flag.Parse()

	if id == "" {
		r := rand.Int()
		fmt.Println("No id was given. Using randomly generated number", r)
		id = strconv.Itoa(r)
	}

	ip, err := localip.LocalIP()
	if err != nil {
		log.Fatal("Could not get local IP address. Error:", err)
	}

	IP := net.ParseIP(ip)

	elevator := newElevator(id, name, IP, newElevatorState(0))

	elevator.requestListener.Init()
	go elevator.requestListener.Listen()

	fmt.Println("Successfully created new elevator: ")
	fmt.Println(elevator)

	return elevator
}

func newElevator(id string, name string, ip net.IP, state ElevatorState) node {
	return node{
		id:    id,
		name:  name,
		state: state,
		ip:    ip,
		requestListener: connection.NewListener(net.UDPAddr{
			IP:   ip,
			Port: connection.GetAvailablePort(),
		}),
		peers:          make(map[string]*peer),
		lifesignalChan: make(chan LifeSignal),
	}
}

func newElevatorState(Foo int) ElevatorState {
	return ElevatorState{
		Foo:  Foo,
		Busy: false,
	}
}

func newPeer(sender connection.Sender, state ElevatorState, id string) *peer {
	return &peer{
		sender:    sender,
		state:     state,
		id:        id,
		lastSeen:  time.Now(),
		connected: false,
	}
}

func (e node) String() string {
	return fmt.Sprintf("------- Elevator %s----\n ~ id: %s\n ~ listening on: %s",
		e.name, e.id, &e.requestListener.Addr)
}

func (p peer) String() string {
	return fmt.Sprintf("------- Peer ----\n ~ id: %s\n ~ sends to: %s\n", p.id, &p.sender.Addr)
}
