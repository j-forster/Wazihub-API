package wazihub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/j-forster/Wazihub-API/tools"
)

type Device struct {
	Id         string      `json:"id"`
	Name       string      `json:"name"`
	GatewayId  string      `json:"gateway_id"`
	Sensors    []*Sensor   `json:"sensors"`
	Actuators  []*Actuator `json:"actuators"`
	Domain     string      `json:"domain"`
	Visibility string      `json:"visibility"`
}

func (device *Device) GetSensor(sensorId string) *Sensor {
	for _, sensor := range device.Sensors {
		if sensor.Id == sensorId {
			return sensor
		}
	}
	return nil
}

func CurrentDeviceId() string {
	return tools.GetMACAddr()
}

func CreateSensor(deviceId string, sensor *Sensor) error {

	data, err := json.Marshal(sensor)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(data)

	resp, err := http.Post(Cloud+"devices/"+deviceId+"/sensors", "application/json", buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Recieved status %q:%q", resp.Status, body)
	}
	return nil
}

func CreateDevice(device *Device) error {

	data, err := json.Marshal(device)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(data)

	resp, err := http.Post(Cloud+"devices", "application/json", buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Recieved status %q:%q", resp.Status, body)
	}
	return nil
}

func GetDevice(deviceId string) (*Device, error) {

	resp, err := http.Get(Cloud + "devices/" + deviceId)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("Recieved status %q:%q", resp.Status, body)
	}

	body, _ := ioutil.ReadAll(resp.Body)
	device := &Device{}
	err = json.Unmarshal(body, device)
	return device, err
}
