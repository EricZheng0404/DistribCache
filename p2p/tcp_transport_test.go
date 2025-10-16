package p2p

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"

)

func TestTCPTransport(t *testing.T) {
	opts := TCPTransportOpts{
		ListenAddress:    ":3000",
		HandShakeFunc: NOPHandShakeFunc,
		Decoder:     DefaultDecoder{},
	}
	tr := NewTCPTransport(opts)



	go func(){
		for {
			msg := <- tr.Consume()
			fmt.Printf("Received message: %v\n", msg)
		}
	}()
	assert.Equal(t, tr.ListenAddress, ":3000")

	assert.Nil(t, tr.ListenAndAccept())
	select{}
}