package dal

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/google/uuid"

	"project/internal/model"
	global "project/pkg/global"
	componentsql "project/sql"
)

func TestMariaDBTopicMappingUniqueKeyRepository(t *testing.T) {
	if os.Getenv("TP_MYSQL_TEST_FLAVOR") != "mariadb" {
		t.Skip("requires TP_MYSQL_TEST_FLAVOR=mariadb and a disposable MariaDB database")
	}
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if host == "" || port == "" || os.Getenv("TP_MYSQL_TEST_DB") == "" {
		t.Skip("requires TP_MYSQL_TEST_* MariaDB connection settings")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	db, err := relational.Open(context.Background(), relational.Target{
		Name: "topic-mapping-test", Dialect: relational.MySQL, Flavor: relational.FlavorMariaDB,
		Host: host, Port: portNumber, Database: os.Getenv("TP_MYSQL_TEST_DB"),
		Username: os.Getenv("TP_MYSQL_TEST_USER"), Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"), TimeZone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	statements, err := componentsql.MySQLFamilyMigrationStatements("mariadb", "012_platform_base.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS `device_topic_mappings`") {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	t.Cleanup(func() { _ = relational.CloseDB(db) })
	previous := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = previous })

	suffix := uuid.NewString()
	base := model.DeviceTopicMapping{
		DeviceConfigID: suffix, Name: "mapping-" + suffix, Direction: "down",
		SourceTopic: "src/" + suffix, TargetTopic: "target/" + suffix, Priority: 10,
		Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := CreateDeviceTopicMapping(&base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Delete(&model.DeviceTopicMapping{}, "id = ?", base.ID).Error })
	duplicate := base
	duplicate.ID = 0
	duplicate.Name += "-duplicate"
	if err := CreateDeviceTopicMapping(&duplicate); err == nil {
		t.Fatal("duplicate composite key was not rejected by the MariaDB unique index")
	}

	newSource := "updated/" + suffix
	if err := UpdateDeviceTopicMappingByID(context.Background(), base.ID, map[string]interface{}{
		"source_topic": newSource, "updated_at": time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	duplicate = base
	duplicate.ID = 0
	duplicate.Name += "-updated-duplicate"
	duplicate.SourceTopic = newSource
	if err := CreateDeviceTopicMapping(&duplicate); err == nil {
		t.Fatal("updated composite key was not reflected in the MariaDB digest index")
	}
	var stored model.DeviceTopicMapping
	if err := db.Model(&model.DeviceTopicMapping{}).Take(&stored, "id = ?", base.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SourceTopic != newSource || stored.UniqueKeyDigest == "" {
		t.Fatalf("updated row=%+v; expected topic and digest to stay in sync", stored)
	}
}
