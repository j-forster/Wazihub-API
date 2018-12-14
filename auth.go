package wazihub

import (
	"strings"
	"time"

	"github.com/j-forster/Wazihub-API/mqtt"
)

var client *mqtt.Client
var topics *mqtt.Topic

func Login(username, password string) error {

	auth := &mqtt.ConnectAuth{
		Username: username,
		Password: password,
	}

	id := CurrentDeviceId()

	for i := 0; ; i++ {

		dial, err := mqtt.Dial(":1883", id, true, auth, nil)
		if err == nil {

			client = dial
			topics = mqtt.NewTopic(nil, "")

			go func() {
				for msg := range client.Message() {
					topics.Publish(strings.Split(msg.Topic, "/"), client, msg)
				}
			}()
			return nil
		}

		if i == 5 {
			return err
		}

		time.Sleep(time.Second * 1)
	}
}
