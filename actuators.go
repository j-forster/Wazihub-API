package wazihub

type Actuator struct {
	Id              string `json:"id"`
	Name            string `json:"name"`
	ActuationDevice string `json:"actuating_device"`
	ControlType     string `json:"control_type"`
	Value           string `json:"value"`
}

func Actuation(deviceId, actuatorId string) (chan string, error) {

	if client == nil {
		panic("Call wazihub.Login() before using this function.")
	}

	subs, err := client.Subscribe("devices/"+deviceId+"/actuators/"+actuatorId+"/value", 0x00)
	if err != nil {
		return nil, err
	}

	channel := make(chan string)

	go (func() {
		for msg := range subs {
			channel <- string(msg.Data)
		}
		close(channel)
	})()

	return channel, nil
}
