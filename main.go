package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
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
	state ElevatorState
	// Minor change: Instead of setting these via parameters, set them programmatically at the start
	// and then broadcast a new port whenever we have successfully made a new connection
	// How to ensure (if two peers connect at the same time) that they don't try to connect at the same port?
	SendAddr   net.TCPAddr // Main address, used *only* for sending data (no, that's just not what it is)
	ListenAddr net.TCPAddr
	peers      []*peer
	peersLock  *sync.Mutex
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
	Name             string
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

			for i, peer := range elevator.peers {
				fmt.Println()
				fmt.Println("-------------------------------")
				fmt.Printf("Peer %d: %#v\n", i, peer)
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
//
//	what this does
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
					newSendPort, err := transfer.GetAvailablePort()
					// TODO: fix
					if err != nil {
						log.Fatal(err)
					}
					e.SendAddr.Port = newSendPort

					fmt.Println("New send port:")
					fmt.Println(newSendPort)

					newListenPort, err := transfer.GetAvailablePort()
					// TODO: fix
					if err != nil {
						log.Fatal(err)
					}
					e.ListenAddr.Port = newListenPort

					fmt.Println("New listening port")
					fmt.Println(newListenPort)

					fmt.Println("Sender host port", _peer.Sender.HostAddr.Port)
					fmt.Println("Sender peer port", _peer.Sender.PeerAddr.Port)
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

		newPeer := &peer{
			Sender: transfer.P2PSender{
				DataChan:  make(chan int),
				QuitChan:  make(chan int),
				ReadyChan: make(chan int),
				HostAddr:  e.SendAddr,
				PeerAddr:  lifeSignal.ListenerAddr,
			},
			Listener: transfer.P2PListener{
				QuitChan:  make(chan int),
				ReadyChan: make(chan int),
				Addr:      e.ListenAddr,
			},
			state:    lifeSignal.State,
			id:       lifeSignal.SenderId,
			lastSeen: time.Now(),
		}

		// Problem: We need to be able to switch the ports between reading two life signals
		// to ensure that we can connect to two different nodes (if we are the 3rd guy joining the network)
		// but we also need to ensure that it takes a sufficient amount of time so that we don't overwrite the
		// previous

		// The genious: Setting the ports one at a time
		// We can safely set the send port without fucking up anything
		// Fuscking shit
		// We also need the listening port to change between calls
		fmt.Println("----Peer ports: ----")
		fmt.Println("Sender:")
		fmt.Println(e.SendAddr)
		fmt.Println(lifeSignal.ListenerAddr)
		fmt.Println("Listener:")
		fmt.Println(e.ListenAddr)
		fmt.Println("-------------------")
		// fmt.Println(lifeSignal.ListenerAddr)

		go newPeer.Listener.Listen()
		// Wait for "new peer"-listener to return that it is ready
		<-newPeer.Listener.ReadyChan
		// Broadcast this to the rest of the system
		e.state.ConnectionStates[newPeer.id] = Listening

		e.peers = append(e.peers, newPeer)
		fmt.Println()
		// fmt.Printf("New peer added: \n%#v\n", newPeer)

		e.peersLock.Unlock()
	}
}

func initElevator() elevator {
	var id, name, dataFlag, dataPort string
	flag.StringVar(&id, "id", "", "id of this peer")
	flag.StringVar(&name, "name", "", "name of this peer")
	flag.StringVar(&dataFlag, "data", "", "data of this peer")
	flag.StringVar(&dataPort, "port", "", "port for this peer")

	flag.Parse()

	data, err := strconv.Atoi(dataFlag)

	if err != nil {
		fmt.Print("Could not convert data flag value ", data)
		fmt.Println(" to int.")
	}

	sendPort, err := transfer.GetAvailablePort()
	// TODO: fix
	if err != nil {
		log.Fatal(err)
	}

	listenPort, err := transfer.GetAvailablePort()
	// TODO: fix
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(sendPort)
	fmt.Println(listenPort)

	ip, err := localip.LocalIP()
	if err != nil {
		log.Fatal("Could not get local IP adress")
	}

	IP := net.ParseIP(ip)

	elevator := elevator{
		id: id,
		state: ElevatorState{
			ConnectionStates: make(map[string]ConnectionState),
			Foo:              data,
			Name:             name,
			Busy:             false,
		},
		SendAddr: net.TCPAddr{
			IP:   IP,
			Port: sendPort,
		},
		ListenAddr: net.TCPAddr{
			IP:   IP,
			Port: listenPort,
		},
		peers:     make([]*peer, 0),
		peersLock: &sync.Mutex{},
	}

	// TODO: Improve the debugging / printing information
	// Use some kind of custom struct print() implementation idk how but it's probably possible
	// fmt.Printf("Successfully created new elevator:\n%#v\n", elevator)

	return elevator
}
