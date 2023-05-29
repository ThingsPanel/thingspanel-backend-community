package connect

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var MQTTSClient mqtt.Client

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("这是mqtts Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

func Connect(broker, user, pass, caPath, crtPath, keyPath string) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("ssl://%s", broker))
	tlsConfig := NewTlsConfig(caPath, crtPath, keyPath)
	opts.SetTLSConfig(tlsConfig)
	opts.SetUsername(user)
	opts.SetPassword(pass)
	opts.SetAutoReconnect(true)
	opts.SetDefaultPublishHandler(messagePubHandler)
	MQTTSClient = mqtt.NewClient(opts)
	if token := MQTTSClient.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	// fmt.Println("mqtts", client)
	// sub(client)
	// MQTTSClient.Subscribe("device/attributes", 0, messagePubHandler)
}

func NewTlsConfig(caPath, crtPath, keyPath string) *tls.Config {
	certpool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(caPath)
	if err != nil {
		log.Fatalln(err.Error())
	}
	certpool.AppendCertsFromPEM(ca)
	clientKeyPair, err := tls.LoadX509KeyPair(crtPath, keyPath)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		RootCAs:            certpool,
		ClientAuth:         tls.NoClientCert,
		ClientCAs:          nil,
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{clientKeyPair},
	}
}
