package mqtt

import "fmt"

type OTAInformFunc func(deviceNumber string, payload []byte) error

var otaInformPublisher OTAInformFunc

func RegisterOTAInformPublisher(fn OTAInformFunc) {
	otaInformPublisher = fn
}

func PublishOTAInform(deviceNumber string, payload []byte) error {
	if otaInformPublisher == nil {
		return fmt.Errorf("ota mqtt publisher is not registered")
	}
	return otaInformPublisher(deviceNumber, payload)
}

func OTAInformTopics(deviceNumber string) []string {
	return []string{
		"ota/devices/infrom/" + deviceNumber,
		"ota/devices/inform/" + deviceNumber,
	}
}
