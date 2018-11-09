package mqtt

import (
	"fmt"
	"io"
	"log"
)

const (
	CONNECTING = 0
	CONNECTED  = 1
	CLOSING    = 3
	CLOSED     = 4
)

type Publisher interface {
	Publish(msg *Message)
}

type SubscriptionHandler interface {
	Subscribe(conn *Connection, topic string, qos byte) *Subscription
	Unsubscribe(subs *Subscription)
}

type Connection struct {
	writer io.Writer
	closer io.Closer
	server *Server
	// publisher Publisher
	// subsHandler SubscriptionHandler

	//	server   *Server
	ClientID string

	state int

	mid int

	Will *Message

	messages map[int]*Message
	subs     map[string]*Subscription
	values   map[string]interface{}
}

func NewConnection(w io.Writer, c io.Closer, server *Server) *Connection {

	conn := &Connection{
		writer:   w,
		closer:   c,
		server:   server,
		messages: make(map[int]*Message),
		values:   make(map[string]interface{}),
		subs:     make(map[string]*Subscription)}

	return conn
}

func (conn *Connection) Get(key string) interface{} {

	v, ok := conn.values[key]
	if ok {
		return v
	}
	return nil
}

func (conn *Connection) Set(key string, value interface{}) {

	conn.values[key] = value
}

func (conn *Connection) Alive() bool {

	return conn.state != CLOSED
}

func (conn *Connection) Write(data []byte) (n int, err error) {
	n, err = conn.writer.Write(data)
	return
}

func (conn *Connection) Close() error {

	if conn.state != CLOSED {

		conn.state = CLOSED

		for _, sub := range conn.subs {
			//conn.server
			conn.server.Unsubscribe(sub)
		}

		conn.subs = nil

		if conn.closer != nil {
			conn.closer.Close()
		}

		if conn.server.handler != nil {
			conn.server.handler.Disconnect(conn)
		}
	}
	return nil
}

func (conn *Connection) Fail(err error) error {

	if conn.Alive() {

		fmt.Println(err)
		conn.Close()

		if conn.Will != nil {
			conn.server.Publish(conn, conn.Will)
		}
	}

	return err
}

func (conn *Connection) Failf(format string, a ...interface{}) error {
	return conn.Fail(fmt.Errorf(format, a...))
}

// sowas wie Body() oder New() weil mal mit body und mal nur head benötigt wird..
func Head(b0 byte, length int, total int) ([]byte, []byte) {

	if length < 0x80 {
		buf := make([]byte, 2+total)
		buf[0] = b0
		buf[1] = byte(length)
		return buf, buf[2:]
	}

	if length < 0x8000 {
		buf := make([]byte, 3+total)
		buf[0] = b0
		buf[1] = byte(length & 127)
		buf[2] = byte(length >> 7)
		return buf, buf[3:]
	}

	if length < 0x800000 {
		buf := make([]byte, 4+total)
		buf[0] = b0
		buf[1] = byte(length & 127)
		buf[2] = byte((length >> 7) & 127)
		buf[3] = byte(length >> 14)
		return buf, buf[4:]
	}

	if length < 0x80000000 {
		buf := make([]byte, 5+total)
		buf[0] = b0
		buf[1] = byte(length & 127)
		buf[2] = byte((length >> 7) & 127)
		buf[3] = byte((length >> 14) & 127)
		buf[4] = byte(length >> 21)
		return buf, buf[5:]
	}

	return nil, nil
}

func (conn *Connection) ConnAck(code byte) {

	// buf, _ := WriteBegin(1)
	buf := make([]byte, 4)
	buf[0] = 0x20 // CONNACK
	buf[1] = 0x02 // remaining length: 2
	buf[3] = code

	conn.Write(buf)
	if code != 0 {
		conn.Close()
	}
}

func (conn *Connection) Subscribe(topic string, qos byte) byte {

	sub, ok := conn.subs[topic]
	if !ok {
		//sub = new(Subscription)
		//sub.conn = conn
		//sub.qos = qos
		//conn.server.Subscribe(topic, sub)
		sub = conn.server.Subscribe(conn, topic, qos)

		if sub != nil {

			conn.subs[topic] = sub
		} else {

			// could not subscribe (the server is closing)
			conn.Close()
			return 0
		}
	}

	//TODO it's not qos, but sub.qos
	// need to update the qos at the stored subscription
	// (the client may subscribe to an already subscribed topic)
	return qos // granted qos
}

func (conn *Connection) Publish(sub *Subscription, msg *Message) {

	log.Printf("[DEBUG] Publish to (%s) at %q: [%d]", sub.conn.ClientID, msg.Topic, len(msg.Buf))

	// qos = Min(sub.qos, msg.qos)
	qos := sub.qos
	if msg.QoS < qos {
		qos = msg.QoS
	}

	switch qos {
	case 0:
		l := len(msg.Topic)
		head, vhead := Head(0x30|bool2byte(msg.retain), 2+l+len(msg.Buf), 2+l)
		vhead[0] = byte(l >> 8)
		vhead[1] = byte(l & 0xff)
		copy(vhead[2:], msg.Topic)
		conn.Write(head)
		conn.Write(msg.Buf)
	case 1, 2:
		l := len(msg.Topic)
		head, vhead := Head(0x30|(qos<<1)|bool2byte(msg.retain), 2+l+2+len(msg.Buf), 2+l+2)
		vhead[0] = byte(l >> 8)
		vhead[1] = byte(l & 0xff)
		copy(vhead[2:], msg.Topic)
		conn.mid++
		vhead[2+l] = byte(conn.mid >> 8)
		vhead[2+l+1] = byte(conn.mid & 0xff)
		conn.Write(head)
		conn.Write(msg.Buf)

		//TODO store message and retry if timeout
	}
}

func (conn *Connection) Unsubscribe(topic string) {

	sub, ok := conn.subs[topic]
	if ok {
		conn.server.Unsubscribe(sub)
	}
}

func (conn *Connection) PingResp() {
	// buf, _ := WriteBegin(0)
	buf := make([]byte, 2)
	buf[0] = 0xD0 // PINGRESP
	buf[1] = 0x00 // remaining length: 0
	conn.Write(buf)
}

///////////////////////////////////////////////////////////////////////////////

// what is wrong with golang to not support b := byte(a bool) ?!
func bool2byte(a bool) byte {
	if a {
		return 1
	}
	return 0
}
