package wazihub

type Actuator struct {
	Id              string `json:"id"`
	Name            string `json:"name"`
	ActuationDevice string `json:"actuating_device"`
	ControlType     string `json:"control_type"`
	Value           string `json:"value"`
}

func Actuation(deviceId, actuatorId string) chan string {

	return Subscribe("devices/" + deviceId + "/actuators/" + actuatorId + "/value")
}
