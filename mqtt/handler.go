package mqtt

type Handler interface {
	Connect(conn *Connection, username, password string) error
	Disconnect(conn *Connection)
	Publish(conn *Connection, msg *Message) error
	Subscribe(conn *Connection, topic string, qos byte) error
}
