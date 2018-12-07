package mqtt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
)

type Closer interface {
	Close(client *Client, err error)
}

const (
	StateConnecting = iota
	StateConnected
	StateDisconnecting
	StateDisconnected
	StateSession
)

var States = [...]string{
	"connecting",
	"connected",
	"disconnecting",
	"disconnected",
	"session",
}

type Client struct {
	// Client-Id as given by the client.
	Id string
	// The server used for Subscribe & Unsubscribe, as well as for Authentification
	Server Server
	// A closer that can be set to the underlying connection.
	// It will be closed when the client disconnects.
	io.Closer
	//
	Context context.Context

	CleanSession bool

	Will *Message

	State int

	queuePacket chan Packet
	queueWriter chan io.Writer

	sigServed chan struct{}

	pending map[int]Packet
	subs    map[string]*Subscription

	sysall *Subscription
}

var (
	connectionRefused = errors.New("The server declined the connection.")
	unexpectedPacket  = errors.New("Recieved an unexpected packet.")
)

func Dial(addr string, clientId string, cleanSession bool, auth *ConnectAuth, will *Message) (*Client, error) {

	client := &Client{
		Id:           clientId,
		pending:      make(map[int]Packet),
		queuePacket:  make(chan Packet),
		queueWriter:  make(chan io.Writer),
		CleanSession: cleanSession,
		// Server: &loopback{
		// 	topics: NewTopic(nil, ""),
		// },
		Server: make(loopback),
		subs:   make(map[string]*Subscription),
		State:  StateConnecting,
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	client.Context = context.Background()
	client.Closer = conn

	connect := Connect("MQIsdp", byte(0x03), cleanSession, 5000, clientId, will, auth)
	connect.WriteTo(conn)

	pkt, err := Read(conn)
	if err != nil {
		return client, err
	}

	if connAck, ok := pkt.(*ConnAckPacket); ok {
		switch connAck.Code {
		case CodeAccepted:
			client.State = StateConnected
			go client.serveReader(conn)
			go client.serve()
			client.serveWriter(conn)
			return client, nil

		default:
			if int(connAck.Code) > 0 && int(connAck.Code) < len(Codes) {
				return client, fmt.Errorf("Connect Error: %q", Codes[int(connAck.Code)])
			}
			return client, connectionRefused
		}
	} else {

		return client, unexpectedPacket
	}
}

func (client *Client) Message() chan *Message {
	if loop, ok := client.Server.(loopback); ok {
		return loop
	}
	return nil
}

func (client *Client) Send(pkt Packet) error {

	if client.State == StateDisconnected {
		return nil
	}

	header := pkt.Header()
	if header.QoS != 0x00 {

		var id int
		switch packet := pkt.(type) {
		case *PublishPacket:
			id = packet.Id
		case *PubRelPacket:
			id = packet.Id
		case *SubscribePacket:
			id = packet.Id
		case *UnsubscribePacket:
			id = packet.Id
		}
		// id must not be 0, but we ignore that here
		if id != 0 {
			client.pending[id] = pkt
		}
	}

	client.queuePacket <- pkt
	return nil
}

func (client *Client) Publish(sender *Client, msg *Message) error {
	// if client != sender {
	// We don't notify ourselves.
	// ( is that correct? )
	return client.Send(Publish(msg))
	// }
	// return nil
}

func (client *Client) Subscribe(topic string, qos byte) error {

	return client.Send(Subscribe(0, []TopicSubscription{TopicSubscription{topic, qos}}))
}

func (client *Client) Unsubscribe(topic string) {

	client.Send(Unsubscribe(0, []string{topic}))
}

func (client *Client) serve() {

	var writer io.Writer
	buffer := &bytes.Buffer{}
	client.sigServed = make(chan struct{})

	for {
		// log.Println("waiting client..")
		select {
		case packet := <-client.queuePacket:
			if packet == nil {
				close(client.sigServed)
				return
			}
			if writer != nil && buffer.Len() == 0 {
				packet.WriteTo(writer)
			} else {
				packet.WriteTo(buffer)
				log.Printf("Bufferd %q by %v", client, packet)
			}
			// log.Println("Buffer now:", buffer.Len())
		case writer = <-client.queueWriter:
			// log.Println("got writer")
		default:
			// log.Println("waiting ! client..")
			if writer != nil && buffer.Len() != 0 {
				packet, _ := Read(buffer)
				// log.Println("from buf", packet.Header())
				packet.WriteTo(writer)
			} else {
				select {
				case packet := <-client.queuePacket:
					if packet == nil {
						close(client.sigServed)
						return
					}
					// log.Println("make ! packet", writer, packet.Header())
					if writer != nil {
						packet.WriteTo(writer)
					} else {
						packet.WriteTo(buffer)
						log.Printf("Bufferd %q by %v", client, packet)
					}
					// log.Println("Buffer now:", buffer.Len())
				case writer = <-client.queueWriter:
					// log.Println("got ! writer")
				}
			}
		}
	}

	/*
		if buffer.Len() == 0 {
			if packet, ok := <-client.queuePacket; ok {
				packet.WriteTo(writer)
			}
			} else {
				packet, _ := Read(buffer)
				packet.WriteTo(writer)
			}
		}
	*/
}

func (client *Client) serveWriter(writer io.Writer) {

	client.queueWriter <- writer

	/*
		for pkt := range client.queue {
			pkt.WriteTo(writer)
		}

		log.Println("Closing", client.Id, " to ", client.State == StateSession)

		if client.State == StateSession {

			session, err := os.OpenFile("session/"+client.Id+".dump", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Fatal(err)
			}

			for pkt := range client.queue {
				pkt.WriteTo(session)
			}
		}
	*/
}

func (client *Client) Disconnect() {

	if client.State != StateConnected {
		return
	}
	client.State = StateDisconnecting

	client.Send(Disconnect())

	client.Close(nil)
}

func (client *Client) String() string {
	return client.Id
}

func (client *Client) cleanup() {

	// unsubscribe all
	for _, sub := range client.subs {
		client.Server.Unsubscribe(sub)
	}
	if client.sysall != nil {
		client.Server.Unsubscribe(client.sysall)
	}
	client.subs = nil

	if client.Closer != nil {
		// this should close the network connection
		client.Closer.Close()
		client.Closer = nil
		// and will make .Read & .Write calls fail
		// so that .serveReader terminates
	}
}

func (client *Client) Close(err error) {

	if client.State == StateDisconnected || client.State == StateSession {
		return
	}

	client.Server.Disconnect(client, err)

	if client.Will != nil && err != nil {
		will := client.Will
		client.Will = nil
		client.Publish(nil, will)
	}

	if client.CleanSession || err == nil {

		client.State = StateDisconnected

		client.cleanup()

		// will end the .serve() and trigger sigServed
		close(client.queuePacket)
		<-client.sigServed
		// must be closed after sigServed
		close(client.queueWriter)

	} else {

		client.State = StateSession
		client.queueWriter <- nil
	}
}

////////////////////////////////////////////////////////////////////////////////

var (
	UnacceptableProtoV = errors.New("Unacceptable protocol verion. Expected '0x03'.")
	ClientIdRejected   = errors.New("Client-Id too long or too short.")
	UnknownPacketType  = errors.New("Unknown packet type.")
	Unaccepted         = errors.New("Connection not accepted.")
	connRefused        = errors.New("The remote station rejected the connection.")
	recoveredRead      = errors.New("Reading error.")
)

func unknownPacketErr(mtype byte, state int) error {
	return fmt.Errorf("Recieved a %s-packet while %s.", MessageTypes[mtype], States[state])
}

func (client *Client) serveReader(reader io.Reader) {

	for {
		packet, err := Read(reader)

		if err != nil {
			client.Close(err)
			return
		}

		client.consume(packet)
		if client.State != StateConnected {
			return
		}
	}
}

func (client *Client) connect(pkt *ConnectPacket, w io.Writer) error {

	if client.State != StateConnecting && client.State != StateSession {
		return unknownPacketErr(pkt.header.MType, client.State)
	}

	if pkt.Protocol != "MQIsdp" {
		return fmt.Errorf("unsupported protocol '%.12s'", pkt.Protocol)
	}
	if pkt.Version != 0x03 {
		return UnacceptableProtoV
	}
	if pkt.Will != nil {
		// log.Printf("[MQTT ] Will: topic:%q qos:%d %q\n", pkt.Will.Topic, pkt.Will.QoS, pkt.Will.Data)
		client.Will = pkt.Will
	}

	client.CleanSession = pkt.CleanSession

	if len(pkt.ClientId) < 3 || len(pkt.ClientId) > 128 {
		return ClientIdRejected
	}

	client.Id = pkt.ClientId

	code := client.Server.Connect(client, pkt.Auth)

	ConnAck(code).WriteTo(w)
	if code != CodeAccepted {
		return Unaccepted
	}

	client.State = StateConnected
	return nil
}

func (client *Client) consume(packet Packet) {

	switch pkt := packet.(type) {
	case *ConnectPacket:

		client.Close(unknownPacketErr(pkt.header.MType, client.State))

	case *ConnAckPacket:

		if client.State != StateConnecting {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		if pkt.Code != 0 {

			if int(pkt.Code) > 0 && int(pkt.Code) < len(Codes) {
				err := errors.New("Connection refused: " + Codes[int(pkt.Code)])
				client.Close(err)
				return
			}
			client.Close(connectionRefused)
			return
		}

		client.State = StateConnected

	case *SubscribePacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		granted := make([]TopicSubscription, len(pkt.Topics))

		for i, topic := range pkt.Topics {
			granted[i].Name = topic.Name

			subs, ok := client.subs[topic.Name]
			if !ok {
				subs = client.Server.Subscribe(client, topic.Name, topic.QoS)
				client.subs[topic.Name] = subs
			}
			granted[i].QoS = subs.QoS
		}

		client.Send(SubAck(pkt.Id, granted))

	case *SubAckPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		// Delete from pending to stop resending Publish
		delete(client.pending, pkt.Id)

	case *UnsubscribePacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		for _, topic := range pkt.Topics {
			if subs, ok := client.subs[topic]; ok {
				client.Server.Unsubscribe(subs)
				delete(client.subs, topic)
			}
		}

		client.Send(UnsubAck(pkt.Id))

	case *UnsubAckPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		// might already be deleted from previous duplicate PubAck packets
		delete(client.pending, pkt.Id)

	case *PublishPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		switch pkt.Header().QoS {
		case 0x00: // At most once

			client.Server.Publish(client, pkt.Message())

		case 0x01: // At least once

			client.Server.Publish(client, pkt.Message())

			// Acknowledge the Publishing
			client.Send(PubAck(pkt.Id))

		case 0x02: // Exactly once

			client.Send(PubRec(pkt.Id))

			// we stop here if we already recieved this Publish (with the same Id)
			if _, ok := client.pending[pkt.Id]; ok {
				break
			}

			client.Server.Publish(client, pkt.Message())

			// to indicate that this Message Id is taken
			client.pending[pkt.Id] = nil
		}

	case *PubAckPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		// might already be deleted from previous duplicate PubAck packets
		delete(client.pending, pkt.Id)

	case *PubRelPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		client.Send(PubComp(pkt.Id))
		// might already be deleted from previous duplicate PubRel packets
		delete(client.pending, pkt.Id)

	case *PubRecPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		// delete from pending to stop resending Publish
		delete(client.pending, pkt.Id)
		client.Send(PubRel(pkt.Id))

	case *PubCompPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		// delete from pending to stop resending PubRel
		delete(client.pending, pkt.Id)

	case *PingReqPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}
		// Ping Request -> Response
		client.Send(PingResp())

	case *DisconnectPacket:

		if client.State != StateConnected {
			client.Close(unknownPacketErr(pkt.header.MType, client.State))
			return
		}

		client.Close(nil)
		return

	default:
		client.Close(UnknownPacketType)
		return
	}
}
