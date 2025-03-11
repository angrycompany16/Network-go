package connection

import "reflect"

// Wrapper for network messages
type Message struct {
	TypeName string      `json:"TypeName"`
	Data     interface{} `json:"Data"`
}

func NewMessage(data any) Message {
	return Message{
		TypeName: reflect.TypeOf(data).Name(),
		Data:     data,
	}
}
