package mqtt

import (
	"errors"
	"fmt"
	"io"
	"net"
)

type Closer interface {
	Close(client *Client, err error)
}

const (
	StateConnecting = iota
	StateConnected
	StateDisconnected
)

var States = [...]string{
	"connecting",
	"connected",
	"disconnected",
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
	State int

	queue   chan Packet
	pending map[int]Packet
	subs    map[string]*Subscription
}

var (
	connectionRefused = errors.New("The server declined the connection.")
	unexpectedPacket  = errors.New("Recieved an unexpected packet.")
)

func Dial(addr string, clientId string, auth *ConnectAuth, will *Message) (*Client, error) {

	client := &Client{
		Id:      clientId,
		pending: make(map[int]Packet),
		queue:   make(chan Packet),
		Server: &loopback{
			topics: NewTopic(nil, ""),
		},
		subs:  make(map[string]*Subscription),
		State: StateConnecting,
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	client.Closer = conn

	connect := Connect("MQIsdp", byte(0x03), true, 5000, clientId, will, auth)
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
			go client.serveWriter(conn)
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

func (client *Client) Send(pkt Packet) error {

	if pkt.Header().QoS != 0x00 {

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

	client.queue <- pkt
	return nil

	/*
		var buf bytes.Buffer
		pkt.WriteTo(&buf)
		fmt.Println(buf.Bytes())
		return nil
	*/
}

func (client *Client) Publish(sender *Client, msg *Message) error {
	// if client != sender {
	// We don't notify ourselves.
	// ( is that correct? )
	return client.Send(Publish(msg))
	// }
	// return nil
}

func (client *Client) Subscribe(topic string, qos byte) (chan *Message, error) {

	channel := make(chan *Message)
	subscription := client.Server.Subscribe(&channelReciever{channel}, topic, qos)
	client.subs[topic] = subscription
	return channel, client.Send(Subscribe(0, []TopicSubscription{TopicSubscription{topic, qos}}))
}

func (client *Client) Unsubscribe(topic string) {
	if subs, ok := client.subs[topic]; ok {
		client.Server.Unsubscribe(subs)
		delete(client.subs, topic)
	}
}

func (client *Client) serveWriter(writer io.Writer) {

	for pkt := range client.queue {
		pkt.WriteTo(writer)
	}
}

func (client *Client) Disconnect() {
	client.Close(nil)
}

func (client *Client) Close(err error) {

	if client.State == StateDisconnected {
		return
	}

	for _, sub := range client.subs {
		client.Server.Unsubscribe(sub)
	}

	client.subs = nil

	if client.State == StateConnected {
		client.Send(Disconnect())
	}

	client.Server.Disconnect(client, err)

	client.State = StateDisconnected

	if client.Closer != nil {
		client.Closer.Close()
	}
}

////////////////////////////////////////////////////////////////////////////////

var (
	UnacceptableProtoV = errors.New("Unacceptable protocol verion. Expected '0x03'.")
	ClientIdRejected   = errors.New("Client-Id too long or too short.")
	UnknownPacketType  = errors.New("Unknown packet type.")
	connRefused        = errors.New("The remote station rejected the connection.")
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

		switch pkt := packet.(type) {
		case *ConnectPacket:

			if client.State != StateConnecting {
				client.Close(unknownPacketErr(pkt.header.MType, client.State))
				return
			}

			if pkt.Protocol != "MQIsdp" {
				err := fmt.Errorf("unsupported protocol '%.12s'", pkt.Protocol)
				client.Close(err)
				return
			}
			if pkt.Version != 0x03 {
				client.Close(UnacceptableProtoV)
				return
			}
			if pkt.Will != nil {
				// log.Printf("[MQTT ] Will: topic:%q qos:%d %q\n", pkt.Will.Topic, pkt.Will.QoS, pkt.Will.Data)
			}
			if pkt.CleanSession {
				// log.Printf("[MQTT ] Clean Session.")
			}
			if len(pkt.ClientId) < 3 || len(pkt.ClientId) > 128 {
				client.Close(ClientIdRejected)
				return
			}

			client.Id = pkt.ClientId

			code := client.Server.Connect(client, pkt.Auth)

			client.Send(ConnAck(code))
			if code != CodeAccepted {
				client.Close(nil)
				return
			}

			client.State = StateConnected

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
}
