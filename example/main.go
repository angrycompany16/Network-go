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
	timeout = time.Second * 5
)

type LifeSignal struct {
	// ConnectionMap map[string]bool
	ListenerAddr net.UDPAddr
	SenderId     string
	State        ElevatorState
	WorldView    []ElevatorState
}

type ElevatorMsg struct {
	SenderId string
	Data     int
}

type elevator struct {
	id        string
	name      string
	state     ElevatorState
	ip        net.IP
	listener  transfer.Listener
	peers     []*peer
	peersLock *sync.Mutex
}

type peer struct {
	Sender   transfer.Sender
	state    ElevatorState
	id       string
	lastSeen time.Time
}

type ElevatorState struct {
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

	go elevator.readPeerMsgs()

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

	e.peersLock.Lock()
	if char == 'C' || char == 'c' {
		if len(e.peers) == 0 {
			fmt.Println("No peers!")
		}

		for _, peer := range e.peers {
			msg := e.newMsg(e.state.Foo)
			peer.Sender.DataChan <- msg
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

func (e *elevator) readPeerMsgs() {
	for {
		msg := <-e.listener.DataChan
		var result ElevatorMsg
		e.listener.DecodeMsg(&msg, &result)
		fmt.Printf("Received data %d from elevator %s\n", result.Data, result.SenderId)
	}
}

func (e *elevator) sendLifeSignal(signalChan chan (LifeSignal)) {
	for {
		signal := LifeSignal{
			// ConnectionMap: make(map[string]bool),
			ListenerAddr: e.listener.Addr,
			SenderId:     e.id,
			State:        e.state,
		}

		for _, peer := range e.peers {
			// signal.ConnectionMap[peer.id] = peer.Sender.Connected
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
				// TODO: improve synchronization of connections so they work even under
				// huge packet loss / just figure out why they don't work
				if !_peer.Sender.Connected {
					go _peer.Sender.Send()
					<-_peer.Sender.ReadyChan
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

func (e *elevator) newMsg(data int) ElevatorMsg {
	return ElevatorMsg{
		Data:     data,
		SenderId: e.id,
	}
}

func initElevator() elevator {
	for {
		var id, name string
		flag.StringVar(&id, "id", "", "id of this peer")
		flag.StringVar(&name, "name", "", "name of this peer")

		flag.Parse()

		ip, err := localip.LocalIP()
		if err != nil {
			fmt.Println("Could not get local IP address. Error:", err)
			fmt.Println("Retrying...")
			time.Sleep(time.Second)
			continue
		}

		IP := net.ParseIP(ip)

		elevator := newElevator(id, name, IP, newElevatorState(0))

		go elevator.listener.Listen()
		<-elevator.listener.ReadyChan

		fmt.Println("Successfully created new elevator: ")
		fmt.Println(elevator)

		return elevator
	}
}

func newElevator(id string, name string, ip net.IP, state ElevatorState) elevator {
	return elevator{
		id:    id,
		name:  name,
		state: state,
		ip:    ip,
		listener: transfer.NewListener(net.UDPAddr{
			IP:   ip,
			Port: transfer.GetAvailablePort(),
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

func newPeer(sender transfer.Sender, state ElevatorState, id string) *peer {
	return &peer{
		Sender:   sender,
		state:    state,
		id:       id,
		lastSeen: time.Now(),
	}
}

func (e elevator) String() string {
	return fmt.Sprintf("------- Elevator %s----\n ~ id: %s\n ~ listening on: %s",
		e.name, e.id, &e.listener.Addr)
}

func (p peer) String() string {
	return fmt.Sprintf("------- Peer %s----\n ~ Sender:\n %s\n", p.id, p.Sender)
}
