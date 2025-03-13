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

// Question: Is it time to scrap the QUIC connections?
// Instead we could: Broadcast incoming requests over UDP
// Include the assignee in the broadcast
// Then everything just works

// Fuck.

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

// TODO: Split into several files for readability maybe

const (
	stateBroadcastPort = 36251 // Akkordrekke
)

var (
	timeout = time.Millisecond * 500
)

// Note that all members must be public
type Heartbeat struct {
	ListenerAddr net.UDPAddr
	SenderId     string
	State        State
	WorldView    []State
}

type Msg struct {
	SenderId string
	Data     int
}

type node struct {
	id            string
	name          string
	state         State
	ip            net.IP
	listener      *connection.Listener
	peers         map[string]*peer
	heartbeatChan chan Heartbeat
	keyEventChan  <-chan keyboard.KeyEvent
}

type peer struct {
	sender    connection.Sender
	state     State
	id        string
	lastSeen  time.Time
	connected bool
}

type State struct {
	Foo  int
	Busy bool
}

// (Almost) all threads are spawned in main!
// The only thread that doesn't spawn in main is the connection thread
// This obviouslt cannot be done as we need to spawn sender threads every time someone
// connects.
func main() {
	nodeInstance := initNode()

	go broadcast.BroadcastSender(stateBroadcastPort, nodeInstance.heartbeatChan)
	go broadcast.BroadcastReceiver(stateBroadcastPort, nodeInstance.heartbeatChan)

	nodeInstance.listener.Init()
	go nodeInstance.listener.Listen()

	// -- KEYBOARD INPUT
	var err error
	nodeInstance.keyEventChan, err = keyboard.GetKeys(10)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = keyboard.Close()
	}()

	for {
		if nodeInstance.update() {
			break
		}
	}
}

func (n *node) ConnectPeer(_peer *peer, lifeSignal Heartbeat) {
	_peer.sender.Addr = lifeSignal.ListenerAddr
	_peer.sender.Init()
	go _peer.sender.Send()
	_peer.connected = true
}

func (n *node) handleHeartbeat(heartbeat Heartbeat) {
	if n.id == heartbeat.SenderId {
		return
	}

	_peer, ok := n.peers[heartbeat.SenderId]
	if ok {
		_peer.lastSeen = time.Now()
		_peer.state = heartbeat.State

		// I think QUIC might be the best thing to have graced the earth with its
		// existence
		// We want to connect that boy
		if !_peer.connected {
			n.ConnectPeer(_peer, heartbeat)
			return
		}

		if _peer.sender.Addr.Port != heartbeat.ListenerAddr.Port {
			fmt.Printf("Sending to port %d, but peer is listening on port %d\n", _peer.sender.Addr.Port, heartbeat.ListenerAddr.Port)
			_peer.sender.QuitChan <- 1
			n.ConnectPeer(_peer, heartbeat)
		}

		return
	}

	sender := connection.NewSender(heartbeat.ListenerAddr, n.id)

	newPeer := newPeer(sender, heartbeat.State, heartbeat.SenderId)

	n.peers[heartbeat.SenderId] = newPeer
	fmt.Println("New peer added: ")
	fmt.Println(newPeer)
}

func (n *node) handleMessage(_msg connection.Message) {
	var message connection.Message
	mapstructure.Decode(_msg, &message)

	var msg Msg
	err := mapstructure.Decode(message.Data, &msg)

	if err != nil {
		log.Fatal("Could not decode elevator request:", err)
	}

	fmt.Printf("Received data %d from elevator %s\n", msg.Data, msg.SenderId)
}

func (n *node) handleKeyInput(keyEvent keyboard.KeyEvent) bool {
	if keyEvent.Rune == 'A' || keyEvent.Rune == 'a' {
		n.state.Foo++
		fmt.Println("Value foo update: ", n.state.Foo)
	}

	if keyEvent.Rune == 'S' || keyEvent.Rune == 's' {
		for id, peer := range n.peers {
			if !peer.connected {
				return false
			}
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
		for _, peer := range n.peers {
			if !peer.connected {
				continue
			}
			fmt.Println("Sending data to peer")
			msg := connection.NewMessage(n.state.Foo)
			peer.sender.DataChan <- msg
		}
	}

	if keyEvent.Key == keyboard.KeyCtrlC {
		fmt.Println("Exit")
		return true
	}
	return false
}

func (n *node) sendHeartbeat() {
	signal := Heartbeat{
		ListenerAddr: n.listener.Addr,
		SenderId:     n.id,
		State:        n.state,
	}

	for _, peer := range n.peers {
		signal.WorldView = append(signal.WorldView, peer.state)
	}

	n.heartbeatChan <- signal
}

func (n *node) checkLostPeers() {
	for _, peer := range n.peers {
		if peer.lastSeen.Add(timeout).Before(time.Now()) && peer.connected {
			fmt.Println("Lost peer:", peer)
			peer.connected = false
			peer.sender.QuitChan <- 1
			fmt.Println("Deleting peer")
			n.listener.LostPeersChan <- peer.id
			fmt.Println("Continued")
		}
	}
}

func (n *node) update() bool {
	select {
	case heartbeat := <-n.heartbeatChan:
		n.handleHeartbeat(heartbeat)
	case _msg := <-n.listener.DataChan:
		n.handleMessage(_msg)
	case keyEvent := <-n.keyEventChan:
		return n.handleKeyInput(keyEvent)
	default:
		n.sendHeartbeat()
		n.checkLostPeers()
	}
	return false
}

func initNode() node {
	// -- FLAGS
	var id, name string
	flag.StringVar(&id, "id", "", "id of this peer")
	flag.StringVar(&name, "name", "", "name of this peer")

	flag.Parse()

	if id == "" {
		r := rand.Int()
		fmt.Println("No id was given. Using randomly generated number", r)
		id = strconv.Itoa(r)
	}

	// -- LOCAL IP
	ip, err := localip.LocalIP()
	if err != nil {
		log.Fatal("Could not get local IP address. Error:", err)
	}

	IP := net.ParseIP(ip)

	// -- NODE INSTANCE
	nodeInstance := newNode(id, name, IP, newState(0))

	// -- DONE
	fmt.Println("Successfully created new elevator: ")
	fmt.Println(nodeInstance)

	return nodeInstance
}

func newNode(id string, name string, ip net.IP, state State) node {
	return node{
		id:    id,
		name:  name,
		state: state,
		ip:    ip,
		listener: connection.NewListener(net.UDPAddr{
			IP:   ip,
			Port: connection.GetAvailablePort(),
		}),
		peers:         make(map[string]*peer),
		heartbeatChan: make(chan Heartbeat),
	}
}

func newState(Foo int) State {
	return State{
		Foo:  Foo,
		Busy: false,
	}
}

func newPeer(sender connection.Sender, state State, id string) *peer {
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
		e.name, e.id, &e.listener.Addr)
}

func (p peer) String() string {
	return fmt.Sprintf("------- Peer ----\n ~ id: %s\n ~ sends to: %s\n", p.id, &p.sender.Addr)
}
