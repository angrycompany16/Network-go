package localip

import (
	"net"
	"strings"
	"time"
)

const (
	timeout = time.Second * 10
)

var localIP string

func LocalIP() (string, error) {
	if localIP == "" {
		dialer := net.Dialer{Timeout: timeout}
		conn, err := dialer.Dial("udp", "8.8.8.8:80")
		if err != nil {
			return "", err
		}
		defer conn.Close()
		localIP = strings.Split(conn.LocalAddr().String(), ":")[0]
	}
	return localIP, nil
}
