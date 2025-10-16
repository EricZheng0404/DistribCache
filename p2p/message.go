package p2p
const
(IncomingMessage = 0x2
IncomingStream = 0x1)

// Message represents any arbitrary data that is sent between two nodes
type RPC struct {
	Payload []byte
	From string
	Stream bool

}