package broadcast

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"

	"github.com/angrycompany16/Network-go/network/conn"
)

const bufSize = 1024

// Encodes received values from `chans` into type-tagged JSON, then broadcasts
// it on `port`
func BroadcastSender(port int, chans ...interface{}) {
	checkArgs(chans...)
	typeNames := make([]string, len(chans))
	selectCases := make([]reflect.SelectCase, len(typeNames))
	for i, ch := range chans {
		selectCases[i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch),
		}
		typeNames[i] = reflect.TypeOf(ch).Elem().String()
	}

	conn := conn.DialBroadcastUDP(port)
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("255.255.255.255:%d", port))
	for {
		chosen, value, _ := reflect.Select(selectCases)
		jsonstr, _ := json.Marshal(value.Interface())
		ttj, _ := json.Marshal(typeTaggedJSON{
			TypeId: typeNames[chosen],
			JSON:   jsonstr,
		})
		if len(ttj) > bufSize {
			fmt.Printf(
				"Tried to send a message longer than the buffer size (length: %d, buffer size: %d)\n\t'%s'\n"+
					"Either send smaller packets, or go to network/transfer/transferQUIC.go and increase the buffer size",
				len(ttj), bufSize, string(ttj))
			continue
		}
		conn.WriteTo(ttj, addr)
	}
}

// Matches type-tagged JSON received on `port` to element types of `chans`, then
// sends the decoded value on the corresponding channel
func BroadcastReceiver(port int, chans ...interface{}) {
	checkArgs(chans...)
	chansMap := make(map[string]interface{})
	for _, ch := range chans {
		chansMap[reflect.TypeOf(ch).Elem().String()] = ch
	}

	var buf [bufSize]byte
	conn := conn.DialBroadcastUDP(port)
	for {
		n, _, e := conn.ReadFrom(buf[0:])
		if e != nil {
			fmt.Printf("bcast.Receiver(%d, ...):ReadFrom() failed: \"%+v\"\n", port, e)
		}

		var ttj typeTaggedJSON
		json.Unmarshal(buf[0:n], &ttj)
		ch, ok := chansMap[ttj.TypeId]
		if !ok {
			continue
		}
		v := reflect.New(reflect.TypeOf(ch).Elem())
		json.Unmarshal(ttj.JSON, v.Interface())
		reflect.Select([]reflect.SelectCase{{
			Dir:  reflect.SelectSend,
			Chan: reflect.ValueOf(ch),
			Send: reflect.Indirect(v),
		}})
	}
}
