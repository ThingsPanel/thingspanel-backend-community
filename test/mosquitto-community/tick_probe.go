package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

type tickProbeReport struct {
	StartedAt             time.Time     `json:"started_at"`
	IdleConnectDuration   time.Duration `json:"idle_connect_duration_ns"`
	TrafficSourceDuration time.Duration `json:"traffic_source_connect_duration_ns"`
	TrafficTargetDuration time.Duration `json:"traffic_target_connect_duration_ns"`
	TrafficPingIntervalMS int           `json:"traffic_ping_interval_ms"`
	TrafficPings          int64         `json:"traffic_pings"`
	Status                string        `json:"status"`
	Failure               string        `json:"failure,omitempty"`
}

func (r *runner) runTickProbe(reportPath string) error {
	result := tickProbeReport{StartedAt: time.Now().UTC(), TrafficPingIntervalMS: 100, Status: "running"}
	finish := func(err error) error {
		if err != nil {
			result.Status, result.Failure = "INCONCLUSIVE", err.Error()
		} else {
			result.Status = "PASS"
		}
		if reportPath != "" {
			b, marshalErr := json.MarshalIndent(result, "", "  ")
			if marshalErr == nil {
				_ = os.WriteFile(reportPath, append(b, '\n'), 0o600)
			}
			_ = r.writeFixtureStats(reportPath + ".owner.json")
		}
		fmt.Printf("tick probe: idle_connect=%s traffic_source_connect=%s traffic_target_connect=%s pings=%d status=%s\n", result.IdleConnectDuration, result.TrafficSourceDuration, result.TrafficTargetDuration, result.TrafficPings, result.Status)
		return err
	}

	if err := waitHTTPDeadline(r.addr, 10*time.Second); err != nil {
		return finish(err)
	}

	started := time.Now()
	idle, err := r.connect("tick-probe-idle", "qa-device-device-a", 4)
	result.IdleConnectDuration = time.Since(started)
	if err != nil {
		return finish(fmt.Errorf("idle CONNECT: %w", err))
	}
	idle.disconnect()

	started = time.Now()
	source, err := r.connect("tick-probe-traffic-source", "qa-device-device-a", 4)
	result.TrafficSourceDuration = time.Since(started)
	if err != nil {
		return finish(fmt.Errorf("traffic source CONNECT: %w", err))
	}
	defer source.disconnect()

	stop := make(chan struct{})
	pingErrors := make(chan error, 1)
	var pings atomic.Int64
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = source.conn.SetDeadline(time.Now().Add(time.Second))
				if err := writePacket(source.conn, 0xc0, nil); err != nil {
					select {
					case pingErrors <- err:
					default:
					}
					return
				}
				pkt, err := readPacket(source.conn)
				if err != nil {
					select {
					case pingErrors <- err:
					default:
					}
					return
				}
				if pkt.header != 0xd0 {
					select {
					case pingErrors <- fmt.Errorf("expected PINGRESP, got %#x", pkt.header):
					default:
					}
					return
				}
				_ = source.conn.SetDeadline(time.Time{})
				pings.Add(1)
			}
		}
	}()
	time.Sleep(250 * time.Millisecond)
	started = time.Now()
	target, err := r.connect("tick-probe-traffic-target", "qa-device-device-a", 4)
	result.TrafficTargetDuration = time.Since(started)
	close(stop)
	result.TrafficPings = pings.Load()
	if err != nil {
		return finish(fmt.Errorf("target CONNECT during PINGREQ traffic: %w", err))
	}
	target.disconnect()
	select {
	case err := <-pingErrors:
		return finish(fmt.Errorf("traffic source ping failed: %w", err))
	default:
	}
	return finish(nil)
}
