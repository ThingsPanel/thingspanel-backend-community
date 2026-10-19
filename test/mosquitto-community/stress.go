package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	bc "project/internal/brokercontract"
)

type stressReport struct {
	Status               string        `json:"status"`
	StartedAt            time.Time     `json:"started_at"`
	FinishedAt           time.Time     `json:"finished_at"`
	MessageTarget        int           `json:"message_target"`
	MessagesAcked        int           `json:"messages_broker_acknowledged"`
	MessagesSeen         int           `json:"messages_ingress_seen"`
	ReconnectTarget      int           `json:"reconnect_target"`
	Reconnects           int           `json:"authenticated_reconnects"`
	ReconnectConcurrency int           `json:"reconnect_concurrency"`
	ReconnectDurationsMS []int64       `json:"reconnect_durations_ms"`
	OwnerAuthMaxOverlap  int           `json:"owner_auth_max_overlap"`
	MessageDuration      time.Duration `json:"message_duration_ns"`
	ReconnectTime        time.Duration `json:"reconnect_duration_ns"`
	MQTTVersions         []string      `json:"mqtt_versions"`
	BusinessAccept       string        `json:"business_acceptance"`
	Failure              string        `json:"failure,omitempty"`
}

func (r *runner) runStress(messages, reconnects, concurrency int, reportPath string) error {
	if messages < 0 || reconnects < 0 || (messages == 0 && reconnects == 0) {
		return fmt.Errorf("stress counts must be positive")
	}
	if concurrency < 1 || concurrency > 16 {
		return fmt.Errorf("stress reconnect concurrency must be in range 1..16")
	}
	ctxDeadline := time.Now().Add(14 * time.Minute)
	started := time.Now().UTC()
	result := stressReport{Status: "running", StartedAt: started, MessageTarget: messages, ReconnectTarget: reconnects, ReconnectConcurrency: concurrency, MQTTVersions: []string{"3.1.1", "5"}, BusinessAccept: "unobserved; broker PUBACK and ingress observation are separate stages"}
	if err := waitHTTPDeadline(r.addr, 10*time.Second); err != nil {
		return err
	}

	messageStart := time.Now()
	if messages > 0 {
		service, err := r.connect("stress-service", "qa-service", 4)
		if err != nil {
			return fmt.Errorf("stress subscriber connect: %w", err)
		}
		defer service.disconnect()
		codes, err := service.subscribe(bc.IngressPrefix + "/#")
		if err != nil || len(codes) != 1 || codes[0] != 1 {
			return fmt.Errorf("stress subscriber SUBACK=%v err=%v", codes, err)
		}
		for _, version := range []byte{4, 5} {
			c, err := r.connect(fmt.Sprintf("stress-device-%d", version), "qa-device-device-a", version)
			if err != nil {
				return fmt.Errorf("stress MQTT %s device connect: %w", versionLabel(version), err)
			}
			start := messages / 2
			end := messages / 2 * 2
			if version == 4 {
				start, end = 0, messages/2
			} else if messages%2 != 0 {
				end++
			}
			for i := start; i < end; i++ {
				if time.Now().After(ctxDeadline) {
					c.disconnect()
					return fmt.Errorf("stress time limit reached after %d/%d messages", result.MessagesSeen, messages)
				}
				topic := fmt.Sprintf("telemetry/device-a/stress/%d", i)
				payload := []byte(fmt.Sprintf("stress-payload-%d", i))
				reason, err := c.publish(topic, payload, 1, false)
				if err != nil || reason != 0 {
					c.disconnect()
					return fmt.Errorf("message %d broker PUBACK reason=%#x err=%v", i, reason, err)
				}
				result.MessagesAcked++
				if err := r.checkIngress(service, topic, payload); err != nil {
					c.disconnect()
					return fmt.Errorf("message %d ingress validation: %w", i, err)
				}
				result.MessagesSeen++
			}
			c.disconnect()
		}
	}
	result.MessageDuration = time.Since(messageStart)

	reconnectStart := time.Now()
	type reconnectResult struct {
		index int
		ms    int64
		err   error
	}
	jobs := make(chan int)
	results := make(chan reconnectResult, reconnects)
	var workers sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				version := byte(4)
				if i%2 == 1 {
					version = 5
				}
				connectStart := time.Now()
				client, err := r.connect(fmt.Sprintf("stress-reconnect-%d", i), "qa-device-device-a", version)
				elapsed := time.Since(connectStart).Milliseconds()
				if err == nil {
					client.disconnect()
				}
				results <- reconnectResult{index: i, ms: elapsed, err: err}
			}
		}()
	}
	result.ReconnectDurationsMS = make([]int64, reconnects)
	sent := 0
	for i := 0; i < reconnects; i++ {
		if time.Now().After(ctxDeadline) {
			break
		}
		jobs <- i
		sent++
	}
	close(jobs)
	workers.Wait()
	close(results)
	var firstErr error
	for attempt := range results {
		result.ReconnectDurationsMS[attempt.index] = attempt.ms
		if attempt.err == nil {
			result.Reconnects++
		} else if firstErr == nil {
			firstErr = fmt.Errorf("reconnect %d: %w", attempt.index, attempt.err)
		}
	}
	result.ReconnectDurationsMS = result.ReconnectDurationsMS[:sent]
	result.ReconnectTime = time.Since(reconnectStart)
	if firstErr != nil || sent != reconnects {
		result.Status = "INCONCLUSIVE"
		if firstErr != nil {
			result.Failure = firstErr.Error()
		} else {
			result.Failure = fmt.Sprintf("stress time limit reached after %d/%d reconnects", result.Reconnects, reconnects)
		}
	} else {
		result.Status = "PASS"
	}
	result.FinishedAt = time.Now().UTC()
	if stats, err := r.readFixtureStats(); err == nil {
		result.OwnerAuthMaxOverlap = maxAuthOverlap(stats.Auths)
	}
	if reportPath != "" {
		if err := r.writeStressReport(reportPath, result); err != nil {
			return err
		}
		_ = r.writeFixtureStats(reportPath + ".owner.json")
	}
	fmt.Printf("PASS: sanitizer stress counts messages=%d acked=%d ingress_seen=%d reconnects=%d MQTT3/5; elapsed=%s; report=%s\n", result.MessagesSeen, result.MessagesAcked, result.MessagesSeen, result.Reconnects, result.FinishedAt.Sub(result.StartedAt), reportPath)
	if result.Status != "PASS" {
		return fmt.Errorf("%s: %s", result.Status, result.Failure)
	}
	return nil
}

func (r *runner) writeStressReport(path string, result stressReport) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func (r *runner) writeFixtureStats(path string) error {
	if path == "" {
		return nil
	}
	stats, err := r.readFixtureStats()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func (r *runner) readFixtureStats() (fixtureStats, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(r.fixtureURL + "/__test/stats")
	if err != nil {
		return fixtureStats{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fixtureStats{}, fmt.Errorf("fixture stats returned HTTP %d", resp.StatusCode)
	}
	var stats fixtureStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fixtureStats{}, err
	}
	return stats, nil
}

func maxAuthOverlap(auths []fixtureAuth) int {
	type event struct {
		at    time.Time
		delta int
	}
	events := make([]event, 0, len(auths)*2)
	for _, auth := range auths {
		start, err1 := time.Parse(time.RFC3339Nano, auth.ReceivedAt)
		end, err2 := time.Parse(time.RFC3339Nano, auth.ReturnedAt)
		if err1 != nil || err2 != nil || !end.After(start) {
			continue
		}
		events = append(events, event{at: start, delta: 1}, event{at: end, delta: -1})
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].at.Equal(events[j].at) {
			return events[i].delta < events[j].delta
		}
		return events[i].at.Before(events[j].at)
	})
	active, peak := 0, 0
	for _, e := range events {
		active += e.delta
		if active > peak {
			peak = active
		}
	}
	return peak
}

func waitHTTPDeadline(addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return waitHTTP(ctx, addr)
}
