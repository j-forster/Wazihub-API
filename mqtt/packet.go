package mqtt

import (
	"errors"
	"fmt"
	"io"
)

type Packet interface {
	WriteTo(w io.Writer) error // (int64, error)
	Header() *FixedHeader
}

func Read(reader io.Reader) (Packet, error) {

	var fh FixedHeader
	if err := fh.Read(reader); err != nil {
		return nil, err
	}

	buf := make([]byte, fh.Length)

	_, err := io.ReadFull(reader, buf)
	if err != nil {
		return nil, err
	}

	switch fh.MType {
	case CONNECT:
		return readConnect(&fh, buf)
	case CONNACK:
		return readConnAck(&fh, buf)
	case SUBSCRIBE:
		return readSubscribe(&fh, buf)
	case SUBACK:
		return readSubAck(&fh, buf)
	case UNSUBSCRIBE:
		return readUnsubscribe(&fh, buf)
	case UNSUBACK:
		return readUnsubAck(&fh, buf)
	case PUBLISH:
		return readPublish(&fh, buf)
	case PUBACK:
		return readPubAck(&fh, buf)
	case PUBREL:
		return readPubRel(&fh, buf)
	case PUBREC:
		return readPubRec(&fh, buf)
	case PUBCOMP:
		return readPubComp(&fh, buf)
	case PINGREQ:
		return readPingReq(&fh, buf)
	case PINGRESP:
		return readPingResp(&fh, buf)
	case DISCONNECT:
		return readDisconnect(&fh, buf)
	default:
		return nil, fmt.Errorf("Unknown MQTT message type: %d", fh.MType)
	}
}

////////////////////////////////////////////////////////////////////////////////

type ConnectAuth struct {
	Username, Password string
}

type ConnectPacket struct {
	// Header
	header *FixedHeader
	// Variable Header
	Protocol       string
	Version        byte
	CleanSession   bool
	KeepAliveTimer int
	// Payload
	ClientId string
	Will     *Message
	Auth     *ConnectAuth
}

func (pkt *ConnectPacket) WriteTo(w io.Writer) error {

	pkt.Header().WriteTo(w)

	writeString(w, pkt.Protocol)

	w.Write([]byte{pkt.Version})

	var flag byte
	if pkt.CleanSession {
		flag |= 0x02
	}
	if pkt.Will != nil {
		flag |= 0x04
		flag |= pkt.Will.QoS << 3
		if pkt.Will.Retain {
			flag |= 0x20
		}
	}
	if pkt.Auth != nil {
		flag |= 0x40 // Username
		flag |= 0x80 // PAssword
	}

	w.Write([]byte{flag})

	writeInt(w, pkt.KeepAliveTimer)

	writeString(w, pkt.ClientId)

	if pkt.Will != nil {
		writeString(w, pkt.Will.Topic)
		writeBytes(w, pkt.Will.Data)
	}

	if pkt.Auth != nil {
		writeString(w, pkt.Auth.Username)
		writeString(w, pkt.Auth.Password)
	}

	return nil
}

func (pkt *ConnectPacket) Header() *FixedHeader {
	// Protocol + Version + Flags + KeepAlive + ClientId
	length := 2 + len(pkt.Protocol) + 1 + 1 + 2 + 2 + len(pkt.ClientId)
	if pkt.Will != nil {
		length += 2 + len(pkt.Will.Topic) + 2 + len(pkt.Will.Data)
	}
	if pkt.Auth != nil {
		length += 2 + len(pkt.Auth.Username) + 2 + len(pkt.Auth.Password)
	}
	pkt.header.Length = length
	return pkt.header
}

func Connect(protocol string, version byte, cleanSession bool, keepAliveTimer int, clientId string, will *Message, auth *ConnectAuth) *ConnectPacket {
	return &ConnectPacket{
		header: &FixedHeader{
			MType: CONNECT,
		},
		Protocol:       protocol,
		Version:        version,
		CleanSession:   cleanSession,
		KeepAliveTimer: keepAliveTimer,
		ClientId:       clientId,
		Will:           will,
		Auth:           auth,
	}
}

func readConnect(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &ConnectPacket{header: fh}

	l, protocol := readString(buf)
	if l == 0 {
		return pkt, IncompleteHeader
	}
	pkt.Protocol = protocol
	buf = buf[l:]

	if len(buf) < 1 {
		return pkt, IncompleteMessage
	}
	pkt.Version = buf[0]
	buf = buf[1:]

	if len(buf) < 1 {
		return pkt, IncompleteMessage
	}
	connFlags := buf[0]

	pkt.CleanSession = connFlags&0x02 != 0
	willFlag := connFlags&0x04 != 0
	willQoS := connFlags & 0x18 >> 3
	willRetain := connFlags&0x20 != 0
	passwordFlag := connFlags&0x40 != 0
	usernameFlag := connFlags&0x80 != 0

	buf = buf[1:]

	//

	if len(buf) < 2 {
		return pkt, IncompleteMessage
	}
	pkt.KeepAliveTimer = int(buf[0])<<8 + int(buf[1])
	// TODO set SetDeadline() to conn

	buf = buf[2:]

	//

	l, pkt.ClientId = readString(buf)
	if l == 0 {
		return pkt, IncompleteMessage
	}
	buf = buf[l:]

	//

	if willFlag {

		pkt.Will = &Message{}

		pkt.Will.Retain = willRetain
		pkt.Will.QoS = willQoS

		l, pkt.Will.Topic = readString(buf)
		if l == 0 {
			return pkt, IncompleteMessage
		}
		buf = buf[l:]

		l, pkt.Will.Data = readBytes(buf)
		if l == 0 {
			return pkt, IncompleteMessage
		}

		buf = buf[l:]
	}

	//

	if usernameFlag {

		pkt.Auth = &ConnectAuth{}

		l, pkt.Auth.Username = readString(buf)
		if l == 0 {
			return pkt, IncompleteMessage
		}
		buf = buf[l:]

		if passwordFlag {

			l, pkt.Auth.Password = readString(buf)
			if l != 0 {
				buf = buf[l:]
			}
		}
	}

	return pkt, nil

	/*
		  if conn.server.handler != nil && conn.server.handler.Connect(conn, username, password) == nil {

				conn.ConnAck(ACCEPTED)
			} else {

				if !usernameFlag {
					conn.ConnAck(NOT_AUTHORIZED)
				} else {
					conn.ConnAck(BAD_USER_OR_PASS)
				}
			}
	*/
}

////////////////////////////////////////////////////////////////////////////////

const (
	CodeAccepted byte = iota
	CodeUnacceptableProtoV
	CodeIdentifierRejected
	CodeServerUnavaliable
	CodeBatUserOrPassword
	CodeNotAuthorized
)

var Codes = [...]string{
	"Connection accepted.",
	"The Server does not support the level of the MQTT protocol requested by the Client.",
	"The Client identifier is correct UTF-8 but not allowed by the Server.",
	"The Network Connection has been made but the MQTT service is unavailable.",
	"The data in the user name or password is malformed.",
	"The Client is not authorized to connect.",
}

type ConnAckPacket struct {
	header *FixedHeader
	Code   byte
}

func (pkt *ConnAckPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *ConnAckPacket) WriteTo(w io.Writer) error {
	pkt.header.WriteTo(w)
	w.Write([]byte{pkt.Code, 0x00})
	return nil
}

func ConnAck(code byte) *ConnAckPacket {
	return &ConnAckPacket{
		header: &FixedHeader{
			MType:  CONNACK,
			Length: 2,
		},
		Code: code,
	}
}

func readConnAck(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &ConnAckPacket{header: fh}
	if len(buf) < 2 {
		return pkt, IncompleteMessage
	}
	pkt.Code = buf[0]
	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type QoS byte

const (
	AtMostOnce  QoS = 0
	AtLeasOnce  QoS = 1
	ExactlyOnce QoS = 2
	Failure     QoS = 128
)

type TopicSubscription struct {
	Name string // Topic Name
	QoS  byte   // Subscriptions QoS
}

type SubscribePacket struct {
	// Header
	header *FixedHeader
	// Variable Header
	Id int // Message Id
	// Payload
	Topics []TopicSubscription // List of Subscriptions
}

func Subscribe(id int, topics []TopicSubscription) *SubscribePacket {
	return &SubscribePacket{
		header: &FixedHeader{
			MType: SUBSCRIBE,
			QoS:   0x01,
		},
		Id:     id,
		Topics: topics,
	}
}

func (pkt *SubscribePacket) Header() *FixedHeader {
	// MessageId + Topics*(Topic Length + QoS)
	length := 2 + len(pkt.Topics)*(2+1)
	for _, topic := range pkt.Topics {
		length += len(topic.Name)
	}
	pkt.header.Length = length
	return pkt.header
}

func (pkt *SubscribePacket) WriteTo(w io.Writer) error {
	pkt.Header().WriteTo(w)
	writeInt(w, pkt.Id)
	for _, topic := range pkt.Topics {
		writeString(w, topic.Name)
		w.Write([]byte{topic.QoS})
	}
	return nil
}

func readSubscribe(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &SubscribePacket{header: fh}

	if len(buf) < 2 {
		return pkt, IncompleteMessage
	}

	pkt.Id = int(buf[0])<<8 + int(buf[1])
	buf = buf[2:]

	var n int
	// count how many topics
	for i, l := 0, len(buf); i != l; n++ {
		// Lenght MSB + Lenght LSB + 2 byte Length + 1 byte QoS
		i += (int(buf[i]) << 8) + int(buf[i+1]) + 2 + 1
		if i > l {
			return pkt, IncompleteMessage
		}
	}

	pkt.Topics = make([]TopicSubscription, n)
	n = 0
	for len(buf) != 0 {
		l, topic := readString(buf)
		qos := buf[l] & 0x03
		pkt.Topics[n] = TopicSubscription{topic, qos}
		buf = buf[l+1:]

		// grantedQos
		//body[s] = conn.Subscribe(topic, qos)
		n++
	}

	return pkt, nil

	/*
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
	*/
}

////////////////////////////////////////////////////////////////////////////////

type UnsubscribePacket struct {
	// Header
	header *FixedHeader
	// Variable Header
	Id int // Message Id
	// Payload
	Topics []string // List of Topics to unsubscribe
}

func Unsubscribe(id int, topics []string) *UnsubscribePacket {
	return &UnsubscribePacket{
		header: &FixedHeader{
			MType: UNSUBSCRIBE,
			QoS:   0x01,
		},
		Id:     id,
		Topics: topics,
	}
}

func (pkt *UnsubscribePacket) Header() *FixedHeader {
	// MessageId + Topics*(Topic Length)
	length := 2 + len(pkt.Topics)*(2)
	for _, topic := range pkt.Topics {
		length += len(topic)
	}
	pkt.header.Length = length
	return pkt.header
}

func (pkt *UnsubscribePacket) WriteTo(w io.Writer) error {
	pkt.Header().WriteTo(w)
	writeInt(w, pkt.Id)
	for _, topic := range pkt.Topics {
		writeString(w, topic)
	}
	return nil
}

func readUnsubscribe(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &UnsubscribePacket{header: fh}

	if len(buf) < 2 {
		return pkt, IncompleteMessage
	}

	pkt.Id = int(buf[0])<<8 + int(buf[1])
	buf = buf[2:]

	var n int
	// count how many topics
	for i, l := 0, len(buf); i != l; n++ {
		// Lenght MSB + Lenght LSB + 2 byte Length
		i += (int(buf[i]) << 8) + int(buf[i+1]) + 2
		if i > l {
			return pkt, IncompleteMessage
		}
	}

	pkt.Topics = make([]string, n)
	n = 0
	for len(buf) != 0 {
		l, topic := readString(buf)
		pkt.Topics[n] = topic
		buf = buf[l:]
		n++
	}

	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type SubAckPacket struct {
	// Header
	header *FixedHeader
	// Variable Header
	Id int // Message Id
	// Payload
	Topics []TopicSubscription // List of Subscriptions
}

func SubAck(id int, topics []TopicSubscription) *SubAckPacket {
	return &SubAckPacket{
		header: &FixedHeader{
			MType: SUBACK,
		},
		Id:     id,
		Topics: topics,
	}
}

func (pkt *SubAckPacket) Header() *FixedHeader {
	// MessageId + Topics*(granted QoS)
	length := 2 + len(pkt.Topics)
	pkt.header.Length = length
	return pkt.header
}

func (pkt *SubAckPacket) WriteTo(w io.Writer) error {
	pkt.Header().WriteTo(w)
	writeInt(w, pkt.Id)
	for _, topic := range pkt.Topics {
		w.Write([]byte{topic.QoS})
	}
	return nil
}

var InvalidQoS = errors.New("Invalid QoS.")

func readSubAck(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &SubAckPacket{header: fh}

	if len(buf) < 2 {
		return pkt, IncompleteMessage
	}

	pkt.Id = int(buf[0])<<8 + int(buf[1])
	buf = buf[2:]

	n := len(buf)
	pkt.Topics = make([]TopicSubscription, n)
	for i := 0; i < n; i++ {
		pkt.Topics[i].QoS = buf[i]
		if (buf[i] != 0x00) && (buf[i] != 0x01) && (buf[i] != 0x02) && (buf[i] != 0x80) {
			return pkt, InvalidQoS
		}
	}

	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type UnsubAckPacket struct {
	// Header
	header *FixedHeader
	// Variable Header
	Id int // Message Id
}

func UnsubAck(id int) *UnsubAckPacket {
	return &UnsubAckPacket{
		header: &FixedHeader{
			MType:  UNSUBACK,
			Length: 2,
		},
		Id: id,
	}
}

func (pkt *UnsubAckPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *UnsubAckPacket) WriteTo(w io.Writer) error {
	pkt.Header().WriteTo(w)
	writeInt(w, pkt.Id)
	return nil
}

func readUnsubAck(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &UnsubAckPacket{header: fh}

	if len(buf) < 2 {
		return pkt, IncompleteMessage
	}

	pkt.Id = int(buf[0])<<8 + int(buf[1])
	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type PublishPacket struct {
	// Header
	header *FixedHeader
	// Variable Header
	Topic string // Publish Topic
	Id    int    // Message Id
	// Payload
	Data []byte
}

func Publish(msg *Message) *PublishPacket {
	return &PublishPacket{
		header: &FixedHeader{
			MType:  PUBLISH,
			QoS:    msg.QoS,
			Retain: msg.Retain,
		},
		Id:    0, //TODO autogen on QoS > 0
		Topic: msg.Topic,
		Data:  msg.Data,
	}
}

func (pkt *PublishPacket) Message() *Message {
	return &Message{
		Topic:  pkt.Topic,
		QoS:    pkt.header.QoS,
		Data:   pkt.Data,
		Retain: pkt.header.Retain,
	}
}

func (pkt *PublishPacket) Header() *FixedHeader {
	//       Topic                    + Data
	length := 2 + len(pkt.Topic) + len(pkt.Data)
	if pkt.header.QoS > 0 {
		length += 2 // Message Id field
	}
	pkt.header.Length = length
	return pkt.header
}

func (pkt *PublishPacket) WriteTo(w io.Writer) error {

	pkt.Header().WriteTo(w)

	writeString(w, pkt.Topic)
	if pkt.header.QoS > 0 {
		writeInt(w, pkt.Id)
	}
	w.Write(pkt.Data)
	return nil
}

func readPublish(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &PublishPacket{header: fh}

	if len(buf) < 2 {
		return pkt, IncompleteMessage
	}
	var l int
	l, pkt.Topic = readString(buf)
	if l == 0 {
		return pkt, IncompleteMessage
	}
	buf = buf[l:]

	if fh.QoS == 0 { // QoS 0

		pkt.Data = buf

	} else { // QoS 1 or 2

		if len(buf) < 2 {
			// missinge message id
			return pkt, IncompleteMessage
		}
		pkt.Id = int(buf[0])<<8 + int(buf[1])
		buf = buf[2:]
		pkt.Data = buf
	}
	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type PubAckPacket struct {
	header *FixedHeader
	Id     int
}

func PubAck(id int) *PubAckPacket {
	return &PubAckPacket{
		header: &FixedHeader{
			MType:  PUBACK,
			Length: 2, // for Message Id field
		},
		Id: id,
	}
}

func (pkt *PubAckPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *PubAckPacket) WriteTo(w io.Writer) error {
	pkt.header.WriteTo(w)
	writeInt(w, pkt.Id)
	return nil
}

func readPubAck(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &PubAckPacket{header: fh}

	if len(buf) < 2 {
		// Missing message id field
		return pkt, IncompleteMessage
	}
	pkt.Id = int(buf[0])<<8 + int(buf[1])
	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type PubRelPacket struct {
	header *FixedHeader
	Id     int
}

func PubRel(id int) *PubRelPacket {
	return &PubRelPacket{
		header: &FixedHeader{
			MType:  PUBREL,
			QoS:    0x01,
			Length: 2, // for Message Id field
		},
		Id: id,
	}
}

func (pkt *PubRelPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *PubRelPacket) WriteTo(w io.Writer) error {
	pkt.header.WriteTo(w)
	writeInt(w, pkt.Id)
	return nil
}

func readPubRel(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &PubRelPacket{header: fh}

	if len(buf) < 2 {
		// Missing message id field
		return pkt, IncompleteMessage
	}
	pkt.Id = int(buf[0])<<8 + int(buf[1])

	/*
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
	*/
	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type PubRecPacket struct {
	header *FixedHeader
	Id     int
}

func PubRec(id int) *PubRecPacket {
	return &PubRecPacket{
		header: &FixedHeader{
			MType:  PUBREC,
			Length: 2, // for Message Id field
		},
		Id: id,
	}
}

func (pkt *PubRecPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *PubRecPacket) WriteTo(w io.Writer) error {
	pkt.header.WriteTo(w)
	writeInt(w, pkt.Id)
	return nil
}

func readPubRec(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &PubRecPacket{header: fh}

	if len(buf) < 2 {
		// Missing message id field
		return pkt, IncompleteMessage
	}
	pkt.Id = int(buf[0])<<8 + int(buf[1])

	/*
		  // send PUBREL message
			buf = make([]byte, 4)
			buf[0] = 0x62 // PUBREL at qos 1
			buf[1] = 0x02 // remaining length: 2
			buf[2] = byte(mid >> 8)
			buf[3] = byte(mid & 0xff)
			conn.Write(buf)
	*/
	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type PubCompPacket struct {
	header *FixedHeader
	Id     int
}

func PubComp(id int) *PubCompPacket {
	return &PubCompPacket{
		header: &FixedHeader{
			MType:  PUBCOMP,
			Length: 2, // for Message Id field
		},
		Id: id,
	}
}

func (pkt *PubCompPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *PubCompPacket) WriteTo(w io.Writer) error {
	pkt.header.WriteTo(w)
	writeInt(w, pkt.Id)
	return nil
}

func readPubComp(fh *FixedHeader, buf []byte) (Packet, error) {

	pkt := &PubCompPacket{header: fh}

	if len(buf) < 2 {
		// Missing message id field
		return pkt, IncompleteMessage
	}
	pkt.Id = int(buf[0])<<8 + int(buf[1])

	return pkt, nil
}

////////////////////////////////////////////////////////////////////////////////

type PingReqPacket struct {
	header *FixedHeader
}

func PingReq() *PingReqPacket {
	return &PingReqPacket{
		header: &FixedHeader{
			MType:  PINGREQ,
			Length: 0,
		},
	}
}

func (pkt *PingReqPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *PingReqPacket) WriteTo(w io.Writer) error {
	return pkt.header.WriteTo(w)
}

func readPingReq(fh *FixedHeader, buf []byte) (Packet, error) {
	return &PingReqPacket{header: fh}, nil
}

////////////////////////////////////////////////////////////////////////////////

type PingRespPacket struct {
	header *FixedHeader
}

func PingResp() *PingRespPacket {
	return &PingRespPacket{
		header: &FixedHeader{
			MType:  PINGRESP,
			Length: 0,
		},
	}
}

func (pkt *PingRespPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *PingRespPacket) WriteTo(w io.Writer) error {
	return pkt.header.WriteTo(w)
}

func readPingResp(fh *FixedHeader, buf []byte) (Packet, error) {
	return &PingRespPacket{header: fh}, nil
}

////////////////////////////////////////////////////////////////////////////////

type DisconnectPacket struct {
	header *FixedHeader
}

func Disconnect() *DisconnectPacket {
	return &DisconnectPacket{
		header: &FixedHeader{
			MType:  DISCONNECT,
			Length: 0,
		},
	}
}

func (pkt *DisconnectPacket) Header() *FixedHeader {
	return pkt.header
}

func (pkt *DisconnectPacket) WriteTo(w io.Writer) error {
	return pkt.header.WriteTo(w)
}

func readDisconnect(fh *FixedHeader, buf []byte) (Packet, error) {
	return &DisconnectPacket{header: fh}, nil
}

////////////////////////////////////////////////////////////////////////////////

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

func writeString(w io.Writer, str string) (int, error) {
	return writeBytes(w, []byte(str))
}

func writeBytes(w io.Writer, b []byte) (int, error) {
	m, err := w.Write([]byte{byte(len(b) >> 8), byte(len(b) & 0xff)})
	if err != nil {
		return m, err
	}
	n, err := w.Write(b)
	return m + n, err
}

func writeInt(w io.Writer, i int) (int, error) {
	return w.Write([]byte{byte(i >> 8), byte(i & 0xff)})
}
