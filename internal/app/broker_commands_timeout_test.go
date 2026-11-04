package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"project/internal/brokercommands"
)

func TestBrokerCommandWorkerUsesConfiguredDeviceAcknowledgementTimeout(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&brokercommands.Row{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE command_set_logs (id text PRIMARY KEY, message_id text, status text, error_message text)`).Error; err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	row := brokercommands.Row{ID: id, TenantID: "tenant", CallerID: "caller", Intent: "control", IdempotencyKey: "key", DeviceID: "device", TargetDeviceID: "device", RouteCell: "cell", RouteEpoch: 1, ExpiresAt: now.Add(time.Hour), State: brokercommands.StateBrokerAccepted, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-20 * time.Second)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO command_set_logs (id,message_id,status) VALUES (?,?,?)`, id, id, "1").Error; err != nil {
		t.Fatal(err)
	}

	old := viper.Get("mqtt.command_ack_timeout")
	viper.Set("mqtt.command_ack_timeout", 0)
	if got := deviceAcknowledgementTimeout(); got != defaultDeviceAcknowledgementTimeout {
		t.Fatalf("default command acknowledgement timeout=%s, want %s", got, defaultDeviceAcknowledgementTimeout)
	}
	viper.Set("mqtt.command_ack_timeout", "10s")
	t.Cleanup(func() { viper.Set("mqtt.command_ack_timeout", old) })
	worker := &brokerCommandWorker{store: brokercommands.New(db)}
	if err := worker.sweepStaleDispatches(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got brokercommands.Row
	if err := db.Take(&got, "id=?", id).Error; err != nil || got.State != brokercommands.StateFailed {
		t.Fatalf("worker sweep state=%q err=%v", got.State, err)
	}
	var log struct{ Status, ErrorMessage string }
	if err := db.Table("command_set_logs").Select("status,error_message").Where("message_id=?", id).Scan(&log).Error; err != nil || log.Status != "2" {
		t.Fatalf("worker sweep log status=%q error=%q err=%v", log.Status, log.ErrorMessage, err)
	}
}
