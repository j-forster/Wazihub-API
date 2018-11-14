package mqtt

import (
	"errors"
	"io"
)

var (
	ReservedMessageType   = errors.New("Reserved message type.")
	IncompleteHeader      = errors.New("Incomplete header.")
	IncompleteMessage     = errors.New("Incomplete message.")
	MessageLengthExceeded = errors.New("Message length exceeds server maximum.")
	MessageLengthInvalid  = errors.New("Message length exceeds maximum.")
	// UnknownMessageType    = errors.New("Unknown mqtt message type.")
)

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

var MessageTypes = [...]string{
	"",
	"CONNECT",
	"CONNACK",
	"PUBLISH",
	"PUBACK",
	"PUBREC",
	"PUBREL",
	"PUBCOMP",
	"SUBSCRIBE",
	"SUBACK",
	"UNSUBSCRIBE",
	"UNSUBACK",
	"PINGREQ",
	"PINGRESP",
	"DISCONNECT",
}

var MaxMessageLength = 15360

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
			return IncompleteHeader // connection closed in header
		}

		length += int(headBuf[0]&127) * multiplier

		if length > MaxMessageLength {
			return MessageLengthExceeded // server maximum message size exceeded
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

var (
	MessageTooLong = errors.New("Message too long.")
)

func (fh *FixedHeader) WriteTo(w io.Writer) error {

	var b byte
	b = fh.MType << 4
	if fh.Dup {
		b |= 0x80
	}
	b |= fh.QoS << 1
	if fh.Retain {
		b |= 0x01
	}

	if fh.Length < 0x80 {
		_, err := w.Write([]byte{b, byte(fh.Length)})
		return err
	} else if fh.Length < 0x8000 {
		_, err := w.Write([]byte{b, byte(fh.Length & 127), byte(fh.Length >> 7)})
		return err
	} else if fh.Length < 0x8000 {
		_, err := w.Write([]byte{b, byte(fh.Length & 127), byte((fh.Length >> 7) & 127), byte(fh.Length >> 14)})
		return err
	}
	return MessageTooLong
}
