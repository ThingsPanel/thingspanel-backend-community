//go:build mosquitto_plugin

// Build: CGO_CFLAGS=-I/path/to/mosquitto/include go build -tags mosquitto_plugin -buildmode=c-shared -o tp-mosquitto.so ./cmd/tp-mosquitto-plugin
package main

/*
#cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup
#include <stdlib.h>
#include <stdint.h>
*/
import "C"
import (
	"fmt"
	"os"
	bc "project/internal/brokercontract"
	driver "project/internal/mosquittodriver"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

var runtimeDriver *driver.Driver

//export TPStart
func TPStart() C.int {
	token, err := os.ReadFile(os.Getenv("TP_BROKER_TOKEN_FILE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ThingsPanel plugin: owner token file is not readable by broker runtime user")
		return 1
	}
	maxPending, _ := strconv.Atoi(os.Getenv("TP_BROKER_MAX_PENDING"))
	maxSessions, _ := strconv.Atoi(os.Getenv("TP_BROKER_MAX_SESSIONS"))
	runtimeDriver, err = driver.New(driver.Config{OwnerURL: os.Getenv("TP_BROKER_OWNER_URL"), Token: strings.TrimSpace(string(token)), Cell: os.Getenv("TP_BROKER_CELL"), Instance: os.Getenv("TP_BROKER_INSTANCE"), MaxPending: maxPending, MaxSessions: maxSessions, Timeout: 2 * time.Second, RuntimeFencing: os.Getenv("TP_BROKER_ROUTE_FENCING") == "1"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ThingsPanel plugin: invalid control-plane configuration:", err)
		return 1
	}
	return 0
}

//export TPBegin
func TPBegin(ptr C.uintptr_t, id, user, password, peer *C.char, protocol C.int) C.int {
	if runtimeDriver == nil {
		return 0
	}
	if runtimeDriver.Begin(uintptr(ptr), C.GoString(id), C.GoString(user), C.GoString(password), C.GoString(peer), int(protocol)) {
		return 1
	}
	return 0
}

//export TPPoll
func TPPoll(allow *C.int) *C.char {
	if runtimeDriver == nil {
		return nil
	}
	r := runtimeDriver.Poll()
	if r == nil {
		return nil
	}
	*allow = 0
	if r.Allow {
		*allow = 1
	}
	return C.CString(r.ID)
}

//export TPTick
func TPTick() {
	if runtimeDriver != nil {
		runtimeDriver.Tick()
	}
}

//export TPConnected
func TPConnected(ptr C.uintptr_t) {
	if runtimeDriver != nil {
		runtimeDriver.Connected(uintptr(ptr))
	}
}

//export TPClosed
func TPClosed(ptr C.uintptr_t) {
	if runtimeDriver != nil {
		runtimeDriver.Closed(uintptr(ptr))
	}
}

//export TPCheck
func TPCheck(ptr C.uintptr_t, access C.int, topic *C.char, size C.int, qos C.int, retain C.int) C.int {
	if runtimeDriver == nil {
		return 0
	}
	if runtimeDriver.Authorize(uintptr(ptr), int(access), C.GoString(topic), int(size), byte(qos), retain != 0) {
		return 1
	}
	return 0
}

//export TPRewrite
func TPRewrite(ptr C.uintptr_t, topic *C.char, payload unsafe.Pointer, size C.int, qos C.int, retain C.int, outTopic **C.char, outPayload *unsafe.Pointer, outLen *C.int, outRetain *C.int) C.int {
	if runtimeDriver == nil || size < 0 || size > C.int(bc.MaxPayloadBytes) {
		return 1
	}
	t, b, r, err := runtimeDriver.Rewrite(uintptr(ptr), C.GoString(topic), C.GoBytes(payload, size), byte(qos), retain != 0)
	if err != nil {
		return 1
	}
	*outTopic = C.CString(t)
	*outLen = C.int(len(b))
	*outPayload = nil
	if len(b) > 0 {
		*outPayload = C.CBytes(b)
	}
	*outRetain = 0
	if r {
		*outRetain = 1
	}
	return 0
}

//export TPCheckPayload
func TPCheckPayload(ptr C.uintptr_t, access C.int, topic *C.char, payload unsafe.Pointer, size C.int, qos C.int, retain C.int) C.int {
	if runtimeDriver == nil || size < 0 || size > C.int(bc.MaxEnvelopeBytes) {
		return 0
	}
	if runtimeDriver.AuthorizePayload(uintptr(ptr), int(access), C.GoString(topic), C.GoBytes(payload, size), byte(qos), retain != 0) {
		return 1
	}
	return 0
}

//export TPOutgoing
func TPOutgoing(ptr C.uintptr_t, topic *C.char, payload unsafe.Pointer, size C.int, qos C.int, retain C.int, outPayload *unsafe.Pointer, outLen *C.int) C.int {
	if runtimeDriver == nil || size < 0 || size > C.int(bc.MaxEnvelopeBytes) {
		return -1
	}
	b, changed, err := runtimeDriver.Outgoing(uintptr(ptr), C.GoString(topic), C.GoBytes(payload, size), byte(qos), retain != 0)
	if err != nil {
		return -1
	}
	if !changed {
		return 0
	}
	*outPayload = C.CBytes(b)
	*outLen = C.int(len(b))
	return 1
}

//export TPObservation
func TPObservation(ptr C.uintptr_t, online C.int, outTopic **C.char, outPayload *unsafe.Pointer, outLen *C.int) C.int {
	if runtimeDriver == nil {
		return 1
	}
	t, b, e := runtimeDriver.Observation(uintptr(ptr), online != 0)
	if e != nil {
		return 1
	}
	*outTopic = C.CString(t)
	*outPayload = C.CBytes(b)
	*outLen = C.int(len(b))
	return 0
}

//export TPNextObservation
func TPNextObservation(outTopic **C.char, outPayload *unsafe.Pointer, outLen *C.int) C.int {
	if runtimeDriver == nil {
		return 1
	}
	t, b, e := runtimeDriver.NextObservation()
	if e != nil || t == "" {
		return 1
	}
	*outTopic = C.CString(t)
	*outPayload = C.CBytes(b)
	*outLen = C.int(len(b))
	return 0
}

//export TPIsClosed
func TPIsClosed(ptr C.uintptr_t) C.int {
	if runtimeDriver != nil && runtimeDriver.IsClosed(uintptr(ptr)) {
		return 1
	}
	return 0
}

//export TPRewriteWill
func TPRewriteWill(ptr C.uintptr_t, topic *C.char, payload unsafe.Pointer, size C.int, qos C.int, retain C.int, outTopic **C.char, outPayload *unsafe.Pointer, outLen *C.int, outRetain *C.int) C.int {
	if runtimeDriver == nil || size < 0 || size > C.int(bc.MaxPayloadBytes) {
		return 1
	}
	t, b, r, e := runtimeDriver.RewriteWill(uintptr(ptr), C.GoString(topic), C.GoBytes(payload, size), byte(qos), retain != 0)
	if e != nil {
		return 1
	}
	*outTopic = nil
	if t != "" {
		*outTopic = C.CString(t)
	}
	*outPayload = nil
	if len(b) > 0 {
		*outPayload = C.CBytes(b)
	}
	*outLen = C.int(len(b))
	*outRetain = 0
	if r {
		*outRetain = 1
	}
	return 0
}

//export TPStop
func TPStop() {
	if runtimeDriver != nil {
		runtimeDriver.Close()
		runtimeDriver = nil
	}
}
func main() {}
