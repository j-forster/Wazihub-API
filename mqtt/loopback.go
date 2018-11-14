package mqtt

import "strings"

type channelReciever struct {
	channel chan *Message
}

func (cr *channelReciever) Publish(client *Client, msg *Message) error {
	cr.channel <- msg
	return nil
}

////////////////////////////////////////////////////////////////////////////////

type loopback struct {
	topics *Topic
	//subs   map[string]*Subscription
}

func (loop *loopback) Publish(client *Client, msg *Message) error {

	topic := strings.Split(msg.Topic, "/")
	loop.topics.Publish(topic, client, msg)
	return nil
}

func (loop *loopback) Connect(client *Client, auth *ConnectAuth) byte {
	return CodeAccepted
}

func (loop *loopback) Disconnect(client *Client, err error) {
}

func (loop *loopback) Subscribe(recv Reciever, topic string, qos byte) *Subscription {

	subs := NewSubscription(recv, qos)
	//loop.subs[topic] = subs
	loop.topics.Subscribe(strings.Split(topic, "/"), subs)
	return subs
}

func (loop *loopback) Unsubscribe(subs *Subscription) {
	/*for topic, s := range loop.subs {
		if s == subs {
			delete(loop.subs, topic)
		}
	}*/
	if recv, ok := subs.Recv.(*channelReciever); ok {
		close(recv.channel)
	}
	subs.Unsubscribe()
}
