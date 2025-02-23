package integration

import (
	"testing"
)

func TestToo(t *testing.T) {

	t.Log("Test")
	t.Fatal("Suffer")
	// fmt.Println("test")

	// What do we need to test?
	// 1. Check that UDP broadcasting works
	// 2. Make a QUIC connection
	// IDK???
	// 2. Send som data over the network, make sure connections are all-all
	// 3. Kill and revive all but two elevators
	// 4. Shut down all the elevators
}
