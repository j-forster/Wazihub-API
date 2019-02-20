package wazihub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type Sensor struct {
	Id            string      `json:"id"`
	Name          string      `json:"name"`
	SensingDevice string      `json:"sensing_device"`
	QuantityKind  string      `json:"quantity_kind"`
	Unit          string      `json:"unit"`
	LastValue     interface{} `json:"last_value"`
	Calibration   interface{} `json:"calibration"`
}

func PostValue(deviceId string, sensorId string, value interface{}) error {

	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(data)

	resp, err := http.Post(Cloud+"devices/"+deviceId+"/sensors/"+sensorId+"/value", "application/json", buf)
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
func PostValues(deviceId string, sensorId string, value interface{}) error {

	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(data)

	resp, err := http.Post(Cloud+"devices/"+deviceId+"/sensors/"+sensorId+"/values", "application/json", buf)
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
