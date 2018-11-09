package mqtt

import (
	"strconv"
	"strings"
)

///////////////////////////////////////////////////////////////////////////////

type Subscription struct {
	conn *Connection
	// topic string
	topic *Topic

	qos byte

	next, prev *Subscription
}

func NewSubscription(conn *Connection, qos byte) *Subscription {
	sub := new(Subscription)
	sub.conn = conn
	sub.qos = qos
	return sub
}

func (s *Subscription) Publish(msg *Message) {

	if s == nil {
		return
	}

	s.conn.Publish(s, msg)

	s.next.Publish(msg)
}

func (s *Subscription) ChainLength() int {
	if s == nil {
		return 0
	}
	return s.next.ChainLength() + 1
}

func (sub *Subscription) Unsubscribe() {

	topic := sub.topic

	if topic == nil {
		return
	}

	if sub.prev == nil {
		if topic.subs == sub {
			topic.subs = sub.next
		} else {
			topic.mlwcSubs = sub.next
		}

		if sub.next != nil {
			sub.next.prev = nil
		}

		// the topic we unsubscribed can be removed if
		if topic.subs == nil && // no subscribers
			topic.retainMsg == nil && // no retrain message
			topic.mlwcSubs == nil && // no /# subscribers
			topic.wcTopic == nil && // no /+ topic
			len(topic.children) == 0 { // no sub-topics
			topic.Remove()
		}
	} else {
		sub.prev.next = sub.next
		if sub.next != nil {
			sub.next.prev = sub.prev
		}
	}

	sub.topic = nil
}

///////////////////////////////////////////////////////////////////////////////

type Topic struct {
	// topic name like "b" in 'a/b' for b
	name string
	// any sub topic like /b in 'a/b' for a
	children map[string]*Topic
	// wildcard (+) topic
	wcTopic *Topic
	// parent topic like a in 'a/b' for b
	parent *Topic
	// all subscriptions to this topic (double linked list)
	subs *Subscription
	// all subscriptions to /# (multi level wildcard)
	mlwcSubs *Subscription
	// retain message
	retainMsg *Message
}

func NewTopic(parent *Topic, name string) *Topic {
	t := new(Topic)
	t.children = make(map[string]*Topic)
	t.parent = parent
	t.name = name
	return t
}

func (topic *Topic) Find(s []string) *Subscription {

	if len(s) == 0 {

		return topic.subs
	} else {

		t, ok := topic.children[s[0]]
		if ok {
			return t.Find(s[1:])
		}
	}
	return nil
}

func (topic *Topic) Publish(s []string, msg *Message) {

	if len(s) == 0 {

		// len() = 0 means we are at the end of the topics-tree
		// and inform all subscribers here
		topic.subs.Publish(msg)

		// attach retain message to the topic
		if msg.retain {
			topic.retainMsg = msg
		}
	} else {

		// search for the child note
		t, ok := topic.children[s[0]]
		if ok {
			t.Publish(s[1:], msg)
		} else {

			if msg.retain {
				// retain messages are attached to a topic
				// se we need to create the topic as it does not exist
				t.Subscribe(s[1:], nil)
			}
		}

		// notify all ../+ subscribers
		if topic.wcTopic != nil {
			topic.wcTopic.Publish(s[1:], msg)
		}
	}

	// the /# subscribers always match
	topic.mlwcSubs.Publish(msg)
}

func (topic *Topic) FullName() string {
	if topic.parent != nil {
		return topic.parent.FullName() + "/" + topic.name
	} else {
		return topic.name
	}
}

func (topic *Topic) String() string {

	var builder strings.Builder
	if n := topic.subs.ChainLength(); n != 0 {
		builder.WriteString("\n/ (" + strconv.Itoa(n) + " listeners)\n")
	}
	if n := topic.mlwcSubs.ChainLength(); n != 0 {
		builder.WriteString("\n/# (" + strconv.Itoa(n) + " listeners)\n")
	}

	for sub, topic := range topic.children {
		builder.WriteString("\n" + sub + " (" + strconv.Itoa(topic.subs.ChainLength()) + " listeners)")
		topic.PrintIndent(&builder, "  ")
	}
	return builder.String()
}

func (topic *Topic) PrintIndent(builder *strings.Builder, indent string) {

	if n := topic.mlwcSubs.ChainLength(); n != 0 {
		builder.WriteString("\n" + indent + "/# (" + strconv.Itoa(n) + " listeners)")
	}

	if topic.wcTopic != nil {

		builder.WriteString("\n" + indent + "/+ (" + strconv.Itoa(topic.wcTopic.subs.ChainLength()) + " listeners)")
		topic.wcTopic.PrintIndent(builder, indent+"  ")
	}

	for sub, t := range topic.children {
		builder.WriteString("\n" + indent + "/" + sub + " (" + strconv.Itoa(t.subs.ChainLength()) + " listeners)")
		t.PrintIndent(builder, indent+"  ")
	}
}

func (topic *Topic) Enqueue(queue **Subscription, s *Subscription) {

	s.prev = nil
	s.next = *queue

	if s.next != nil {
		s.next.prev = s
	}
	*queue = s

	s.topic = topic
}

func (topic *Topic) Subscribe(t []string, sub *Subscription) {

	if len(t) == 0 {

		topic.Enqueue(&topic.subs, sub)
		if topic.retainMsg != nil {
			sub.conn.Publish(sub, topic.retainMsg)
		}

	} else {

		if t[0] == "#" {
			topic.Enqueue(&topic.mlwcSubs, sub)
			return
		}

		var child *Topic
		var ok bool

		if t[0] == "+" {

			if topic.wcTopic == nil {
				topic.wcTopic = NewTopic(topic, "+")
			}
			child = topic.wcTopic
		} else {
			child, ok = topic.children[t[0]]
			if !ok {
				child = NewTopic(topic, t[0])
				topic.children[t[0]] = child
			}
		}

		child.Subscribe(t[1:], sub)
	}
}

func (topic *Topic) Remove() {

	parent := topic.parent

	if parent != nil {

		// the wildcard topic is attached different to the parent topic
		if topic.name == "+" {

			// also the parent topic if
			if len(parent.children) == 0 && // no sub-topics
				parent.retainMsg == nil && // no retain message
				parent.subs == nil && // no subscriptions
				parent.mlwcSubs == nil && // no /# subscriptions
				parent.parent != nil { // but not the root topic :)

				parent.Remove()
				return
			}

			// remove this wildcard topic
			parent.wcTopic = nil
			return
		}

		// also the parent topic if
		if parent.wcTopic == nil && // no /+ subscribers
			parent.retainMsg == nil && // no retain message
			parent.mlwcSubs == nil && // no /# subscribers
			len(parent.children) == 1 && // no sub-topics (just this one)
			parent.subs == nil && // no subscriptions
			parent.parent != nil { // but not the root topic

			parent.Remove()
			return
		}

		// remove this topic from the parents sub-topics
		delete(parent.children, topic.name)
	}
}
