package mqtt

import (
	"errors"
	"io"
	"log"
)

// errors
var (
	InclompleteHeader       = errors.New("incomplete header")
	MaxMessageLength        = errors.New("message length exceeds server maximum")
	MessageLengthInvalid    = errors.New("message length exceeds maximum")
	IncompleteMessage       = errors.New("incomplete message")
	UnknownMessageType      = errors.New("unknown mqtt message type")
	ReservedMessageType     = errors.New("reserved message type")
	ConnectMsgLacksProtocol = errors.New("connect message has no protocol field")
	ConnectProtocolUnexp    = errors.New("connect message protocol is not 'MQIsdp'")
	TooLongClientID         = errors.New("connect client id is too long")
	UnknownMessageID        = errors.New("unknown message id")
)

const maxMessageLength = 15360

// CONNACK return codes
const (
	ACCEPTED            = 0
	UNACCEPTABLE_PROTOV = 1
	IDENTIFIER_REJ      = 2
	SERVER_UNAVAIL      = 3
	BAD_USER_OR_PASS    = 4
	NOT_AUTHORIZED      = 5
)

// message types
const (
	CONNECT     = 1
	CONNACK     = 2
	PUBLISH     = 3
	PUBACK      = 4
	PUBREC      = 5
	PUBREL      = 6
	PUBCOMP     = 7
	SUBSCRIBE   = 8
	SUBACK      = 9
	UNSUBSCRIBE = 10
	UNSUBACK    = 11
	PINGREQ     = 12
	PINGRESP    = 13
	DISCONNECT  = 14
)

// string representation of message types
var messageType = [...]string{"reserved", "CONNECT", "CONNACK", "PUBLISH",
	"PUBACK", "PUBREC", "PUBREL", "PUBCOMP", "SUBSCRIBE", "SUBACK", "UNSUBSCRIBE",
	"UNSUBACK", "PINGREQ", "PINGRESP", "DISCONNECT"}

///////////////////////////////////////////////////////////////////////////////

type Message struct {
	Topic  string
	Buf    []byte
	QoS    byte
	retain bool
}

///////////////////////////////////////////////////////////////////////////////

func readString(buf []byte) (int, string) {
	length, b := readBytes(buf)
	return length, string(b)
}

func readBytes(buf []byte) (int, []byte) {

	if len(buf) < 2 {
		return 0, nil
	}
	length := (int(buf[0])<<8 + int(buf[1])) + 2
	if len(buf) < length {
		return 0, nil
	}
	return length, buf[2:length]
}

///////////////////////////////////////////////////////////////////////////////

type FixedHeader struct {
	MType  byte
	Dup    bool
	QoS    byte
	Retain bool
	Length int
}

func (fh *FixedHeader) Read(reader io.Reader) error {

	var headBuf [1]byte
	n, err := reader.Read(headBuf[:])
	if err != nil {
		return err // read error
	}
	if n == 0 {
		return io.EOF // connection closed
	}

	fh.MType = byte(headBuf[0] >> 4)
	fh.Dup = bool(headBuf[0]&0x8 != 0)
	fh.QoS = byte((headBuf[0] & 0x6) >> 1)
	fh.Retain = bool(headBuf[0]&0x1 != 0)

	if fh.MType == 0 || fh.MType == 15 {
		return ReservedMessageType // reserved type
	}

	var multiplier int = 1
	var length int

	for {
		n, err = reader.Read(headBuf[:])
		if err != nil {
			return err // read error
		}
		if n == 0 {
			return InclompleteHeader // connection closed in header
		}

		length += int(headBuf[0]&127) * multiplier

		if length > maxMessageLength {
			return MaxMessageLength // server maximum message size exceeded
		}

		if headBuf[0]&128 == 0 {
			break
		}

		if multiplier > 0x4000 {
			return MessageLengthInvalid // mqtt maximum message size exceeded
		}

		multiplier *= 128
	}

	fh.Length = length
	return nil
}

func (fh *FixedHeader) ReadMessage(msg []byte) (int, error) {

	if len(msg) == 0 {
		return 0, io.EOF // conn closed
	}

	fh.MType = msg[0] >> 4
	fh.Dup = bool(msg[0]&0x8 != 0)
	fh.QoS = (msg[0] & 0x6) >> 1
	fh.Retain = bool(msg[0]&0x1 != 0)

	if fh.MType == 0 || fh.MType == 15 {
		return 0, ReservedMessageType // reserved type
	}

	read := 1

	var multiplier int = 1
	var length int

	for {
		msg = msg[1:]
		read++
		if len(msg) == 0 {
			return read, InclompleteHeader // not enough bytes
		}

		length += int(msg[0]&127) * multiplier

		if length > maxMessageLength {
			return read, MaxMessageLength // server maximum message size exceeded
		}

		if msg[0]&128 == 0 {
			break
		}

		if multiplier > 0x4000 {
			return read, MessageLengthInvalid // mqtt maximum message size exceeded
		}

		multiplier *= 128
	}

	fh.Length = length
	return read, nil
}

///////////////////////////////////////////////////////////////////////////////

func (conn *Connection) ReadMessage(msg []byte) {

	for len(msg) != 0 {

		var fh FixedHeader
		l, err := fh.ReadMessage(msg)
		if err != nil {
			conn.Fail(err)
			return
		}
		msg = msg[l:]
		if len(msg) > fh.Length {
			conn.Fail(IncompleteMessage)
			return
		}
		buf := msg[:fh.Length]
		msg = msg[fh.Length:]

		switch fh.MType {
		case CONNECT:
			conn.ReadConnectMessage(&fh, buf)
		case SUBSCRIBE:
			conn.ReadSubscribeMessage(&fh, buf)
		case PUBLISH:
			conn.ReadPublishMessage(&fh, buf)
		case PUBREL:
			conn.ReadPubrelMessage(&fh, buf)
		case PUBREC:
			conn.ReadPubrecMessage(&fh, buf)
		case PUBCOMP:
			conn.ReadPubcompMessage(&fh, buf)
		case PINGREQ:
			conn.PingResp()
		case DISCONNECT:
			conn.Close()
		}
	}
}

// read from a reader (input stream) a new mqtt message
func (conn *Connection) Read(reader io.Reader) {

	var fh FixedHeader
	if err := fh.Read(reader); err != nil {
		conn.Fail(err)
		return
	}

	buf := make([]byte, fh.Length)

	_, err := io.ReadFull(reader, buf)
	if err != nil {
		conn.Fail(IncompleteMessage)
		return
	}

	// log.Printf("Message: %s (length:%d qos:%d dup:%t retain:%t)",
	//   messageType[fh.mtype],
	//   fh.length,
	//   fh.qos,
	//   fh.dup,
	//   fh.retain)

	switch fh.MType {
	case CONNECT:
		conn.ReadConnectMessage(&fh, buf)
	case SUBSCRIBE:
		conn.ReadSubscribeMessage(&fh, buf)
	case PUBLISH:
		conn.ReadPublishMessage(&fh, buf)
	case PUBREL:
		conn.ReadPubrelMessage(&fh, buf)
	case PUBREC:
		conn.ReadPubrecMessage(&fh, buf)
	case PUBCOMP:
		conn.ReadPubcompMessage(&fh, buf)
	case PINGREQ:
		conn.PingResp()
	case DISCONNECT:
		conn.Close()
	}
}

///////////////////////////////////////////////////////////////////////////////

// parse CONNECT messages
func (conn *Connection) ReadConnectMessage(fh *FixedHeader, buf []byte) {
	l, protocol := readString(buf)
	if l == 0 {
		conn.Fail(ConnectMsgLacksProtocol)
		return
	}
	if protocol != "MQIsdp" {
		conn.Failf("unsupported protocol '%.12s'", protocol)
		return
	}
	buf = buf[l:]

	//

	if len(buf) < 1 {
		conn.Fail(IncompleteMessage)
		return
	}
	version := buf[0]
	if version != 0x03 {
		conn.ConnAck(UNACCEPTABLE_PROTOV)
		return
	}
	buf = buf[1:]

	//

	if len(buf) < 1 {
		conn.Fail(IncompleteMessage)
		return
	}
	connFlags := buf[0]
	// log.Printf("Connection Flags: %d", connFlags)

	// cleanSession := connFlags&0x02 != 0
	// if cleanSession {
	// 	log.Println("Clean Session: true")
	// }
	willFlag := connFlags&0x04 != 0
	willQoS := connFlags & 0x18 >> 3
	willRetain := connFlags&0x20 != 0
	passwordFlag := connFlags&0x40 != 0
	usernameFlag := connFlags&0x80 != 0

	buf = buf[1:]

	//

	if len(buf) < 2 {
		conn.Fail(IncompleteMessage)
		return
	}
	// keepAliveTimer := int(buf[0])<<8 + int(buf[1])
	// log.Printf("KeepAlive Timer: %d", keepAliveTimer)
	// TODO set SetDeadline() to conn
	buf = buf[2:]

	//

	l, conn.ClientID = readString(buf)
	if l == 0 {
		conn.Fail(IncompleteMessage)
		return
	}
	if l > 128 {
		// should be max 23, but some client implementations ignore this
		// so we increase the size to 128
		conn.ConnAck(IDENTIFIER_REJ)
		return
	}
	buf = buf[l:]

	//

	if willFlag {

		var will Message

		will.retain = willRetain
		will.QoS = willQoS

		l, will.Topic = readString(buf)
		if l == 0 {
			conn.Fail(IncompleteMessage)
			return
		}
		buf = buf[l:]

		l, will.Buf = readBytes(buf)
		if l == 0 {
			conn.Fail(IncompleteMessage)
			return
		}

		log.Printf("[MQTT ] Will: topic:%q qos:%d %q\n", will.Topic, will.QoS, will.Buf)

		conn.Will = &will
		buf = buf[l:]
	}

	//

	var username, password string

	if usernameFlag {

		l, username = readString(buf)
		if l == 0 {
			conn.Fail(IncompleteMessage)
			return
		}
		buf = buf[l:]

		if passwordFlag {

			l, password = readString(buf)
			if l != 0 {
				buf = buf[l:]
			}
		}
	}

	if conn.server.handler != nil && conn.server.handler.Connect(conn, username, password) == nil {

		conn.ConnAck(ACCEPTED)
	} else {

		if !usernameFlag {
			conn.ConnAck(NOT_AUTHORIZED)
		} else {
			conn.ConnAck(BAD_USER_OR_PASS)
		}
	}
}

///////////////////////////////////////////////////////////////////////////////

// parse a SUBSCRIBE message and send SUBACK
func (conn *Connection) ReadSubscribeMessage(fh *FixedHeader, buf []byte) {

	if len(buf) < 2 {
		conn.Fail(IncompleteMessage)
		return
	}
	mid := int(buf[0])<<8 + int(buf[1])
	buf = buf[2:]
	var s int
	for i, l := 0, len(buf); i != l; s++ {

		i += (int(buf[i]) << 8) + int(buf[i+1]) + 2 + 1
		if i > l {
			conn.Fail(IncompleteMessage)
			return
		}
	}

	l := 2 + s
	head, body := Head(0x90, l, l) // SUBACK
	body[0] = byte(mid >> 8)       // mid MSB
	body[1] = byte(mid & 0xff)     // mid LSB
	s = 2

	for len(buf) != 0 {
		l, topic := readString(buf)
		qos := buf[l] & 0x03
		buf = buf[l+1:]

		// grantedQos
		body[s] = conn.Subscribe(topic, qos)
		s++
	}

	conn.Write(head)
}

///////////////////////////////////////////////////////////////////////////////

// parse a PUBLISH message and tell the server about it
func (conn *Connection) ReadPublishMessage(fh *FixedHeader, buf []byte) {

	if len(buf) < 2 {
		conn.Fail(IncompleteMessage)
		return
	}
	l, topic := readString(buf)
	if l == 0 {
		conn.Fail(IncompleteMessage)
		return
	}
	buf = buf[l:]

	if fh.QoS == 0 { // QoS 0

		conn.server.Publish(conn, &Message{topic, buf, 0, fh.Retain})

	} else { // QoS 1 or 2

		if len(buf) < 2 {
			conn.Fail(IncompleteMessage)
			return
		}
		mid := int(buf[0])<<8 + int(buf[1])
		buf = buf[2:]

		msg := &Message{topic, buf, fh.QoS, fh.Retain}

		if fh.QoS == 1 {

			conn.server.Publish(conn, msg)

			// send PUBACK message
			buf := make([]byte, 4)
			buf[0] = 0x40 // PUBACK
			buf[1] = 0x02 // remaining length: 2
			buf[2] = byte(mid >> 8)
			buf[3] = byte(mid & 0xff)
			conn.Write(buf)
		} else {

			conn.messages[mid] = msg // store

			// send PUBREC message
			buf := make([]byte, 4)
			buf[0] = 0x50 // PUBREC
			buf[1] = 0x02 // remaining length: 2
			buf[2] = byte(mid >> 8)
			buf[3] = byte(mid & 0xff)
			conn.Write(buf)
		}
	}
}

///////////////////////////////////////////////////////////////////////////////

// parse a PUBREL message (a response to a PUBREC at QoS 2)
// the message has alredy been stored at the previous PUBREC message
func (conn *Connection) ReadPubrelMessage(fh *FixedHeader, buf []byte) {

	if len(buf) < 2 {
		conn.Fail(IncompleteMessage)
		return
	}
	mid := int(buf[0])<<8 + int(buf[1])

	msg, ok := conn.messages[mid]
	if !ok {
		conn.Fail(UnknownMessageID)
		return
	}

	conn.server.Publish(conn, msg)
	delete(conn.messages, mid)

	// send PUBREC message
	buf = make([]byte, 4)
	buf[0] = 0x70 // PUBCOMP
	buf[1] = 0x02 // remaining length: 2
	buf[2] = byte(mid >> 8)
	buf[3] = byte(mid & 0xff)
	conn.Write(buf)
}

///////////////////////////////////////////////////////////////////////////////

// parse a PUBREC message
// (a response to a publish from this server to a client on qos 2)
func (conn *Connection) ReadPubrecMessage(fh *FixedHeader, buf []byte) {

	if len(buf) < 2 {
		conn.Fail(IncompleteMessage)
		return
	}
	mid := int(buf[0])<<8 + int(buf[1])

	// send PUBREL message
	buf = make([]byte, 4)
	buf[0] = 0x62 // PUBREL at qos 1
	buf[1] = 0x02 // remaining length: 2
	buf[2] = byte(mid >> 8)
	buf[3] = byte(mid & 0xff)
	conn.Write(buf)
}

///////////////////////////////////////////////////////////////////////////////

// parse a PUBCOMP message
// (a response to a PUBREL from a client to this server)
func (conn *Connection) ReadPubcompMessage(fh *FixedHeader, buf []byte) {

	if len(buf) < 2 {
		conn.Fail(IncompleteMessage)
		return
	}
	// mid := int(buf[0])<<8 + int(buf[1])
}

//////////////////////////////////////////////////////////////////////////////
