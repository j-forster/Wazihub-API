package wazihub

import (
	"strings"

	"github.com/j-forster/Wazihub-API/mqtt"
)

type Actuator struct {
	Id              string      `json:"id"`
	Name            string      `json:"name"`
	ActuationDevice string      `json:"actuating_device"`
	ControlType     string      `json:"control_type"`
	Value           interface{} `json:"value"`
}

type Reciever chan string

func (r Reciever) Publish(client *mqtt.Client, msg *mqtt.Message) error {
	r <- string(msg.Data)
	return nil
}

func Actuation(deviceId, actuatorId string) (Reciever, error) {

	if client == nil {
		panic("Call wazihub.Login() before using this function.")
	}

	topic := "devices/" + deviceId + "/actuators/" + actuatorId + "/value"

	err := client.Subscribe(topic, 0x00)
	if err != nil {
		return nil, err
	}

	recv := make(Reciever)
	subs := mqtt.NewSubscription(recv, 0)
	topics.Subscribe(strings.Split(topic, "/"), subs)
	return recv, nil
}
