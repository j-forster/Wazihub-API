package mqtt

import (
	"bytes"
	"log"
	"testing"
)

type LoggingServer struct {
	Server
}

var connectCounter, disconnectCounter int

func (server *LoggingServer) Publish(client *Client, msg *Message) error {

	log.Printf("PUBLISH %q %q %q\n", client.Id, msg.Topic, msg.Data)
	return server.Server.Publish(client, msg)
}

func (server *LoggingServer) Connect(client *Client, auth *ConnectAuth) byte {
	log.Printf("CONNECT %q %+v\n", client.Id, auth)
	connectCounter++
	if auth != nil {
		if auth.Username+auth.Password == "not allowed" {
			return CodeBatUserOrPassword
		}
	}
	return CodeAccepted
}

func (server *LoggingServer) Disconnect(client *Client, err error) {
	log.Printf("DISCONNECT %q %v\n", client.Id, err)
	disconnectCounter++
}

func (server *LoggingServer) Subscribe(recv Reciever, topic string, qos byte) *Subscription {

	log.Printf("SUBSCRIBE %+v %q QoS:%d\n", recv, topic, qos)
	return server.Server.Subscribe(recv, topic, qos)
}

func (server *LoggingServer) Unsubscribe(subs *Subscription) {

	log.Printf("UNSUBSCRIBE %+v %q QoS:%d\n", subs.Recv, subs.Topic.FullName(), subs.QoS)
	server.Server.Unsubscribe(subs)
}

func TestServer(t *testing.T) {
	server := &LoggingServer{NewServer()}

	go ListenAndServe(":1883", server)

	//////////

	mario, err := Dial(":1883", "It'sMeMario!", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	subs, _ := mario.Subscribe("a/+", 2)
	unused, _ := mario.Subscribe("a/y/#", 2)

	//////////

	luigi, err := Dial(":1883", "It'sMeLuigi!", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("Hello my brother :)")
	luigi.Publish(nil, &Message{
		Topic: "a/b",
		QoS:   2,
		Data:  data,
	})
	luigi.Disconnect()

	//////////

	msg := <-subs
	if msg == nil {
		log.Fatalf("Subscription closed prematurely.")
	}

	if msg.Topic != "a/b" {
		log.Fatalf("Recieved wrong topic: %q\n", msg.Topic)
	}
	if bytes.Compare(msg.Data, data) != 0 {
		log.Fatalf("Recieved wrong data: %q\n", msg.Data)
	}
	log.Printf("Recieved: %q %q QoS:%d\n", msg.Topic, msg.Data, msg.QoS)

	//////////

	mario.Disconnect()

	msg = <-unused
	if msg != nil {
		log.Fatalln("Got a message for an unused subscription.")
	}

	//////////

	auth := &ConnectAuth{
		Username: "not ",
		Password: "allowed",
	}
	if _, err := Dial(":1883", "NotAllowed", auth, nil); err == nil {
		log.Fatalln("Client 'NotAllowed' was not rejected.")
	}

	//////////

	if connectCounter != 3 {
		log.Fatalf("Expected 3 connections, got %d.\n", connectCounter)
	}

	if disconnectCounter != 3 {
		log.Fatalf("Expected 3 disconnections, got %d.\n", disconnectCounter)
	}
}
