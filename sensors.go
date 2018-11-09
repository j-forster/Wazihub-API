package wazihub

type Sensor struct {
	Id            string      `json:"id"`
	Name          string      `json:"name"`
	SensingDevice string      `json:"sensing_device"`
	QuantityKind  string      `json:"quantity_kind"`
	Unit          string      `json:"unit"`
	LastValue     interface{} `json:"last_value"`
	Calibration   interface{} `json:"calibration"`
}
