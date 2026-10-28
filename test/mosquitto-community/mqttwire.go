package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

type packet struct {
	header byte
	body   []byte
}

type mqttClient struct {
	conn    net.Conn
	version byte // MQTT 3.1.1=4, MQTT 5=5
	nextID  uint16
}

type mqttWill struct {
	topic       string
	payload     []byte
	qos         byte
	retain      bool
	delaySecond uint32 // MQTT 5 Will Delay Interval; zero omits the property
}

func dialMQTT(addr string, version byte) (*mqttClient, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	return &mqttClient{conn: conn, version: version, nextID: 1}, nil
}

func (c *mqttClient) connectPacket(clientID, username string) error {
	return c.connectPacketWithWill(clientID, username, nil)
}

func (c *mqttClient) connectPacketWithWill(clientID, username string, will *mqttWill) error {
	return c.connectPacketWithSessionAndWill(clientID, username, true, 0, will)
}

func (c *mqttClient) connectPacketWithSessionAndWill(clientID, username string, clean bool, sessionExpiry uint32, will *mqttWill) error {
	body := make([]byte, 0, 128)
	body = appendUTF8(body, "MQTT")
	body = append(body, c.version)
	flags := byte(0)
	if clean {
		flags = 0x02
	}
	if will != nil {
		flags |= 0x04 | (will.qos << 3)
		if will.retain {
			flags |= 0x20
		}
	}
	if username != "" {
		flags |= 0x80
	}
	body = append(body, flags, 0, 30) // keep alive
	if c.version == 5 {
		if !clean && sessionExpiry > 0 {
			body = append(body, 5, 0x11, byte(sessionExpiry>>24), byte(sessionExpiry>>16), byte(sessionExpiry>>8), byte(sessionExpiry))
		} else {
			body = append(body, 0) // CONNECT properties length
		}
	}
	body = appendUTF8(body, clientID)
	if will != nil {
		if c.version == 5 && will.delaySecond > 0 {
			props := []byte{0x18}
			var delay [4]byte
			binary.BigEndian.PutUint32(delay[:], will.delaySecond)
			props = append(props, delay[:]...)
			body = appendVarInt(body, len(props))
			body = append(body, props...)
		} else if c.version == 5 {
			body = append(body, 0)
		}
		body = appendUTF8(body, will.topic)
		var n [2]byte
		binary.BigEndian.PutUint16(n[:], uint16(len(will.payload)))
		body = append(body, n[:]...)
		body = append(body, will.payload...)
	}
	if username != "" {
		body = appendUTF8(body, username)
	}
	return writePacket(c.conn, 0x10, body)
}

func (c *mqttClient) connect(clientID, username string) (byte, error) {
	if err := c.connectPacket(clientID, username); err != nil {
		return 0xff, err
	}
	p, err := readPacket(c.conn)
	if err != nil {
		return 0xff, err
	}
	if p.header>>4 != 2 || len(p.body) < 2 {
		return 0xff, fmt.Errorf("expected CONNACK, got header %#x body %x", p.header, p.body)
	}
	_ = c.conn.SetDeadline(time.Time{})
	return p.body[1], nil
}

func (c *mqttClient) connectSession(clientID, username string, clean bool, sessionExpiry uint32) (byte, byte, error) {
	if err := c.connectPacketWithSessionAndWill(clientID, username, clean, sessionExpiry, nil); err != nil {
		return 0xff, 0xff, err
	}
	p, err := readPacket(c.conn)
	if err != nil {
		return 0xff, 0xff, err
	}
	if p.header>>4 != 2 || len(p.body) < 2 {
		return 0xff, 0xff, fmt.Errorf("expected CONNACK, got header %#x body %x", p.header, p.body)
	}
	_ = c.conn.SetDeadline(time.Time{})
	return p.body[0] & 1, p.body[1], nil
}

func (c *mqttClient) connectWithWill(clientID, username string, will *mqttWill) (byte, error) {
	if err := c.connectPacketWithWill(clientID, username, will); err != nil {
		return 0xff, err
	}
	p, err := readPacket(c.conn)
	if err != nil {
		return 0xff, err
	}
	if p.header>>4 != 2 || len(p.body) < 2 {
		return 0xff, fmt.Errorf("expected CONNACK, got header %#x body %x", p.header, p.body)
	}
	_ = c.conn.SetDeadline(time.Time{})
	return p.body[1], nil
}

func (c *mqttClient) subscribe(filters ...string) ([]byte, error) {
	return c.subscribeWithOptions(0x01, filters...)
}

// Subscription options request QoS 1 with Retain As Published disabled (RAP=0).
func (c *mqttClient) subscribeWithOptions(options byte, filters ...string) ([]byte, error) {
	id := c.allocateID()
	body := []byte{byte(id >> 8), byte(id)}
	if c.version == 5 {
		body = append(body, 0) // SUBSCRIBE properties length
	}
	for _, filter := range filters {
		body = appendUTF8(body, filter)
		body = append(body, options)
	}
	if err := writePacket(c.conn, 0x82, body); err != nil {
		return nil, err
	}
	p, err := readPacket(c.conn)
	if err != nil {
		return nil, err
	}
	if p.header>>4 != 9 || len(p.body) < 3 || binary.BigEndian.Uint16(p.body[:2]) != id {
		return nil, fmt.Errorf("expected SUBACK for %d, got header %#x body %x", id, p.header, p.body)
	}
	pos := 2
	if c.version == 5 {
		n, size, err := readVarInt(p.body[pos:])
		if err != nil || pos+size+n > len(p.body) {
			return nil, errors.New("malformed MQTT 5 SUBACK properties")
		}
		pos += size + n
	}
	return append([]byte(nil), p.body[pos:]...), nil
}

func (c *mqttClient) publish(topic string, payload []byte, qos byte, retain bool) (byte, error) {
	flags := byte(0x30) | (qos << 1)
	if retain {
		flags |= 1
	}
	body := appendUTF8(nil, topic)
	if qos > 0 {
		id := c.allocateID()
		body = append(body, byte(id>>8), byte(id))
	}
	if c.version == 5 {
		body = append(body, 0) // PUBLISH properties length
	}
	body = append(body, payload...)
	if err := writePacket(c.conn, flags, body); err != nil {
		return 0xff, err
	}
	if qos == 0 {
		return 0, nil
	}
	for {
		p, err := readPacket(c.conn)
		if err != nil {
			return 0xff, err
		}
		if p.header>>4 != 4 {
			if p.header>>4 == 3 {
				if err := c.ackIncomingPublish(p); err != nil {
					return 0xff, err
				}
				continue
			}
			return 0xff, fmt.Errorf("expected PUBACK, got header %#x body %x", p.header, p.body)
		}
		if c.version == 4 {
			if len(p.body) != 2 {
				return 0xff, fmt.Errorf("malformed MQTT 3 PUBACK: %x", p.body)
			}
			return 0, nil
		}
		// MQTT 5 allows a successful PUBACK to omit both the reason code and
		// properties, leaving only the packet identifier.
		if len(p.body) == 2 {
			return 0, nil
		}
		if len(p.body) < 3 {
			return 0xff, fmt.Errorf("malformed MQTT 5 PUBACK: %x", p.body)
		}
		return p.body[2], nil
	}
}

func (c *mqttClient) readPublish(timeout time.Duration) (string, []byte, error) {
	topic, payload, _, err := c.readPublishDetail(timeout)
	return topic, payload, err
}

func (c *mqttClient) readPublishDetail(timeout time.Duration) (string, []byte, bool, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	p, err := readPacket(c.conn)
	if err != nil {
		return "", nil, false, err
	}
	if p.header>>4 != 3 || len(p.body) < 2 {
		return "", nil, false, fmt.Errorf("expected PUBLISH, got header %#x body %x", p.header, p.body)
	}
	topic, pos, err := readUTF8(p.body, 0)
	if err != nil {
		return "", nil, false, err
	}
	qos := (p.header >> 1) & 3
	if qos > 0 {
		if pos+2 > len(p.body) {
			return "", nil, false, errors.New("malformed PUBLISH packet id")
		}
		id := binary.BigEndian.Uint16(p.body[pos : pos+2])
		pos += 2
		if c.version == 5 {
			properties, size, err := readVarInt(p.body[pos:])
			if err != nil || pos+size+properties > len(p.body) {
				return "", nil, false, errors.New("malformed MQTT 5 PUBLISH properties")
			}
			pos += size + properties
		}
		if qos == 1 {
			ack := []byte{byte(id >> 8), byte(id)}
			if c.version == 5 {
				ack = append(ack, 0, 0) // reason success, property length
			}
			if err := writePacket(c.conn, 0x40, ack); err != nil {
				return "", nil, false, err
			}
		}
	}
	return topic, append([]byte(nil), p.body[pos:]...), p.header&1 != 0, nil
}

func (c *mqttClient) ackIncomingPublish(p packet) error {
	if len(p.body) < 2 {
		return errors.New("malformed incoming PUBLISH")
	}
	_, pos, err := readUTF8(p.body, 0)
	if err != nil {
		return err
	}
	if pos+2 > len(p.body) {
		return errors.New("missing incoming packet id")
	}
	id := binary.BigEndian.Uint16(p.body[pos : pos+2])
	ack := []byte{byte(id >> 8), byte(id)}
	if c.version == 5 {
		ack = append(ack, 0, 0)
	}
	return writePacket(c.conn, 0x40, ack)
}

func (c *mqttClient) nextPacket(timeout time.Duration) (packet, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	return readPacket(c.conn)
}

func (c *mqttClient) disconnect() {
	if c.conn == nil {
		return
	}
	if c.version == 5 {
		_ = writePacket(c.conn, 0xe0, []byte{0, 0})
	} else {
		_ = writePacket(c.conn, 0xe0, nil)
	}
	_ = c.conn.Close()
	c.conn = nil
}

func (c *mqttClient) close() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *mqttClient) allocateID() uint16 {
	id := c.nextID
	c.nextID++
	if c.nextID == 0 {
		c.nextID = 1
	}
	return id
}

func writePacket(w io.Writer, header byte, body []byte) error {
	packet := []byte{header}
	packet = appendVarInt(packet, len(body))
	packet = append(packet, body...)
	_, err := w.Write(packet)
	return err
}

func readPacket(r io.Reader) (packet, error) {
	var p packet
	var first [1]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return p, err
	}
	p.header = first[0]
	remaining := 0
	multiplier := 1
	for i := 0; i < 4; i++ {
		var b [1]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return p, err
		}
		remaining += int(b[0]&127) * multiplier
		if b[0]&128 == 0 {
			p.body = make([]byte, remaining)
			_, err := io.ReadFull(r, p.body)
			return p, err
		}
		multiplier *= 128
	}
	return p, errors.New("malformed MQTT remaining length")
}

func appendVarInt(dst []byte, n int) []byte {
	for {
		digit := byte(n % 128)
		n /= 128
		if n > 0 {
			digit |= 0x80
		}
		dst = append(dst, digit)
		if n == 0 {
			return dst
		}
	}
}

func readVarInt(src []byte) (int, int, error) {
	value, multiplier := 0, 1
	for i, b := range src {
		if i >= 4 {
			break
		}
		value += int(b&127) * multiplier
		if b&128 == 0 {
			return value, i + 1, nil
		}
		multiplier *= 128
	}
	return 0, 0, errors.New("malformed MQTT variable integer")
}

func appendUTF8(dst []byte, s string) []byte {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(s)))
	dst = append(dst, size[:]...)
	return append(dst, s...)
}

func readUTF8(src []byte, offset int) (string, int, error) {
	if offset+2 > len(src) {
		return "", offset, errors.New("truncated MQTT UTF-8 field")
	}
	n := int(binary.BigEndian.Uint16(src[offset : offset+2]))
	start := offset + 2
	if start+n > len(src) {
		return "", offset, errors.New("truncated MQTT UTF-8 value")
	}
	return string(src[start : start+n]), start + n, nil
}
