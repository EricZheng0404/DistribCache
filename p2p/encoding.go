package p2p
import (
	"encoding/gob"
	"io"
)


type Decoder interface {
	Decode(io.Reader, *RPC) error
}


type GOBDecoder struct {}

func (dec GOBDecoder) Decode(r io.Reader, msg *RPC) error {
	return gob.NewDecoder(r).Decode(msg)
}

type DefaultDecoder struct {}

func (dec DefaultDecoder) Decode(r io.Reader, msg *RPC) error {
	peekBuf := make([]byte, 1)
	if _, err := r.Read(peekBuf); err != nil {
		return err
	}

	
	// we are just reading the first byte to determine if it is a stream or not
	// if it is a stream we are not decoding anything and we can handle that
	stream := peekBuf[0] == IncomingStream
	if stream{
		msg.Stream = true
		return nil
	} 

	buf := make([]byte, 1028)
	n, err := r.Read(buf)
	if err != nil {
		return err
	}
	msg.Payload = buf[:n]
	return nil
}