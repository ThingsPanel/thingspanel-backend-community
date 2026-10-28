//go:build !mosquitto_plugin

package main

func main() {
	panic("build this plugin with -tags mosquitto_plugin -buildmode=c-shared and Mosquitto 2.1.2 headers")
}
