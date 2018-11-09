package mqtt

import (
	"io"
	"log"
	"net"
	"strings"

	"github.com/j-forster/mqtt/tools"
)

type SubscriptionRequest struct {
	subs  *Subscription
	topic string
}

const (
	CREATE = 1
	REMOVE = 2
)

type SubscriptionChange struct {
	action int
	subs   *Subscription
	topic  string
}

type Server struct {

	//subsReq chan SubscriptionRequest
	//unsubs chan *Subscription
	state    int
	closer   io.Closer
	sigclose chan (struct{})
	subs     chan SubscriptionChange
	pub      chan *Message
	topics   *Topic
	handler  Handler
	debug    bool
}

func NewServer(closer io.Closer, handler Handler) *Server {

	svr := new(Server)
	//svr.subsReq = make(chan SubscriptionRequest)
	//svr.unsubs = make(chan *Subscription)
	svr.closer = closer
	svr.handler = handler
	svr.sigclose = make(chan struct{})
	svr.subs = make(chan SubscriptionChange)
	svr.pub = make(chan *Message)
	svr.topics = NewTopic(nil, "")
	return svr
}

func (svr *Server) SetDebug(debug bool) {

	svr.debug = debug
}

func (svr *Server) Alive() bool {

	return svr.state != CLOSING && svr.state != CLOSED
}

func (svr *Server) Publish(conn *Connection, msg *Message) {

	if !svr.Alive() {
		return
	}

	var err error = nil
	if svr.handler != nil {
		err = svr.handler.Publish(conn, msg)
	}
	if err == nil {

		svr.pub <- msg
	}
}

func (svr *Server) Subscribe(conn *Connection, topic string, qos byte) *Subscription {

	if !svr.Alive() {
		return nil
	}

	var err error = nil
	if svr.handler != nil {
		err = svr.handler.Subscribe(conn, topic, qos)
	}
	if err == nil {

		subs := NewSubscription(conn, qos)
		svr.subs <- SubscriptionChange{CREATE, subs, topic}
		return subs
	}
	return nil
}

func (svr *Server) Unsubscribe(subs *Subscription) {

	if !svr.Alive() {
		return
	}

	svr.subs <- SubscriptionChange{REMOVE, subs, ""}
}

func (svr *Server) Run() {

RUN:
	for {
		select {
		case <-svr.sigclose:

			close(svr.subs)
			close(svr.pub)

			SYSALL := []string{"$SYS", "all"}
			subs := svr.topics.Find(SYSALL)
			for ; subs != nil; subs = subs.next {

				subs.conn.Close()
			}

			svr.state = CLOSED
			break RUN

		case evt := <-svr.subs:

			switch evt.action {
			case CREATE:
				svr.topics.Subscribe(strings.Split(evt.topic, "/"), evt.subs)

			case REMOVE:
				evt.subs.Unsubscribe()
			}

			log.Println("[DEBUG] Topics:", svr.topics)

		case msg := <-svr.pub:

			if msg.Topic == "$SYS/close" {
				// svr.Close()

			} else {

				n := len(msg.Buf)
				if n > 30 {
					n = 30
				}

				if svr.debug {
					log.Printf("[DEBUG] Publish: %q: %q", msg.Topic, string(msg.Buf[:n]))
				}
				svr.topics.Publish(strings.Split(msg.Topic, "/"), msg)
			}
		}
	}
}

func (svr *Server) Close() {

	if svr.Alive() {

		close(svr.sigclose)
		svr.state = CLOSING
		if svr.closer != nil {
			svr.closer.Close()
		}
	}
}

func (svr *Server) Serve(rwc io.ReadWriteCloser) {

	// uconn := tools.Unblock(rwc)

	conn := NewConnection(rwc, rwc, svr)
	defer conn.Close()

	// conn.Subscribe("$SYS/all", 0)

	for conn.Alive() {
		conn.Read(rwc)
	}
}

func Join(conn net.Conn, server *Server) {

	uconn := tools.Unblock(conn)

	wsconn := NewConnection(uconn, uconn, server)
	defer wsconn.Close()

	wsconn.Subscribe("$SYS/all", 0)

	for wsconn.Alive() {
		wsconn.Read(conn)
	}
}

func ListenAndServe(addr string, handler Handler) error {

	tcp, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	server := NewServer(tcp, handler)
	go server.Run()

	for {

		conn, err := tcp.Accept()
		if err == nil {

			go server.Serve(conn)
		} else {

			return err
		}
	}

	return nil
}
