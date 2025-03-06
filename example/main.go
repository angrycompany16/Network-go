package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"strconv"
	"sync"
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

// TODO: Figure out why the third elevator always takes a lot longer to connect
// Potentially a UDP issue?

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
	id        string
	name      string
	state     ElevatorState
	ip        net.IP
	listener  *connection.Listener
	peers     []*peer
	peersLock *sync.Mutex
}

type peer struct {
	sender          connection.Sender
	state           ElevatorState
	id              string
	lastSeen        time.Time
	senderConnected bool
}

type ElevatorState struct {
	Foo  int
	Busy bool
}

func main() {
	elevator := initElevator()

	lifeSignalChannel := make(chan LifeSignal)

	go broadcast.BroadcastSender(stateBroadcastPort, lifeSignalChannel)
	go broadcast.BroadcastReceiver(stateBroadcastPort, lifeSignalChannel)

	go elevator.timeout()
	go elevator.sendLifeSignal(lifeSignalChannel)
	go elevator.readLifeSignals(lifeSignalChannel)

	go elevator.readPeerMsgs()

	for {
		if elevator.HandleDebugInput() {
			break
		}
	}
}

func (n *node) HandleDebugInput() bool {
	char, key, err := keyboard.GetSingleKey()
	if err != nil {
		log.Fatal(err)
	}

	if char == 'A' || char == 'a' {
		n.state.Foo++
		fmt.Println("Value foo update: ", n.state.Foo)
	}

	if char == 'S' || char == 's' {
		if len(n.peers) == 0 {
			fmt.Println("No peers!")
		}

		for i, peer := range n.peers {
			fmt.Println()
			fmt.Println("-------------------------------")
			fmt.Printf("Peer %d: %#v\n", i, peer)
			fmt.Println("-------------------------------")
		}
	}

	if char == 'B' || char == 'b' {
		n.state.Busy = !n.state.Busy
		fmt.Println("Busy updated to: ", n.state.Busy)
	}

	n.peersLock.Lock()
	if char == 'C' || char == 'c' {
		if len(n.peers) == 0 {
			fmt.Println("No peers!")
		}

		for _, peer := range n.peers {
			msg := n.newMsg(n.state.Foo)
			peer.sender.DataChan <- msg
		}
	}
	n.peersLock.Unlock()

	if key == keyboard.KeyCtrlC {
		fmt.Println("Exit")
		return true
	}
	return false
}

func (n *node) timeout() {
	for {
		n.peersLock.Lock()
		for i, peer := range n.peers {
			if peer.lastSeen.Add(timeout).Before(time.Now()) {
				fmt.Println("Removing peer\n", peer)
				peer.sender.QuitChan <- 1
				n.listener.LostPeers[peer.id] = true
				n.peers[i] = n.peers[len(n.peers)-1]
				n.peers = n.peers[:len(n.peers)-1]
			}
		}
		n.peersLock.Unlock()
	}
}

func (n *node) readPeerMsgs() {
	for msg := range n.listener.DataChan {
		var message connection.Message
		mapstructure.Decode(msg, &message)

		var msg ElevatorMsg
		err := mapstructure.Decode(message.Data, &msg)

		if err != nil {
			log.Fatal("Could not decode elevator request:", err)
		}

		fmt.Printf("Received data %d from elevator %s\n", msg.Data, msg.SenderId)
	}
}

func (n *node) sendLifeSignal(signalChan chan (LifeSignal)) {
	for {
		signal := LifeSignal{
			ListenerAddr: n.listener.Addr,
			SenderId:     n.id,
			State:        n.state,
		}

		for _, peer := range n.peers {
			signal.WorldView = append(signal.WorldView, peer.state)
		}

		signalChan <- signal
		time.Sleep(time.Millisecond * 10)
	}
}

func (n *node) readLifeSignals(signalChan chan (LifeSignal)) {
LifeSignals:
	for lifeSignal := range signalChan {
		if n.id == lifeSignal.SenderId {
			continue
		}

		n.peersLock.Lock()
		for _, _peer := range n.peers {
			if _peer.id == lifeSignal.SenderId {
				_peer.lastSeen = time.Now()
				_peer.state = lifeSignal.State
				// I think QUIC might be the best thing to have graced the earth with its
				// existence
				// We want to connect that boy

				if !_peer.senderConnected {
					_peer.sender.Init()
					go _peer.sender.Send()
					_peer.senderConnected = true
				}

				if _peer.sender.Addr.Port != lifeSignal.ListenerAddr.Port {
					fmt.Printf("Sending to port %d, but peer is listening on port %d\n", _peer.sender.Addr.Port, lifeSignal.ListenerAddr.Port)
					_peer.sender.QuitChan <- 1
					_peer.sender.Addr.Port = lifeSignal.ListenerAddr.Port
					_peer.sender.Init()
					go _peer.sender.Send()
					fmt.Println("Done")
				}

				n.peersLock.Unlock()

				continue LifeSignals
			}
		}

		sender := connection.NewSender(lifeSignal.ListenerAddr, n.id)

		newPeer := newPeer(sender, lifeSignal.State, lifeSignal.SenderId)

		n.peers = append(n.peers, newPeer)
		fmt.Println("New peer added: ")
		fmt.Println(newPeer)

		n.peersLock.Unlock()
	}
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

	elevator.listener.Init()
	go elevator.listener.Listen()

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
		listener: connection.NewListener(net.UDPAddr{
			IP:   ip,
			Port: connection.GetAvailablePort(),
		}),
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

func newPeer(sender connection.Sender, state ElevatorState, id string) *peer {
	return &peer{
		sender:          sender,
		state:           state,
		id:              id,
		lastSeen:        time.Now(),
		senderConnected: false,
	}
}

func (e node) String() string {
	return fmt.Sprintf("------- Elevator %s----\n ~ id: %s\n ~ listening on: %s",
		e.name, e.id, &e.listener.Addr)
}

func (p peer) String() string {
	return fmt.Sprintf("------- Peer ----\n ~ id: %s\n ~ sends to: %s\n", p.id, &p.sender.Addr)
}
