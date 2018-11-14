package wazihub

import (
	"github.com/j-forster/Wazihub-API/mqtt"
)

var client *mqtt.Client

func Login(username, password string) error {

	auth := &mqtt.ConnectAuth{
		Username: username,
		Password: password,
	}

	id := CurrentDeviceId()

	c, err := mqtt.Dial(":1883", id, auth, nil)
	if err == nil {
		client = c
	}
	return err
}
