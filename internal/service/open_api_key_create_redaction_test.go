package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/utils"
)

func TestCreateAPIKeyFailureRedactsSQLAndError(t *testing.T) {
	var sqlLog, appLog bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.New(log.New(&sqlLog, "", 0), logger.Config{LogLevel: logger.Info})})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.OpenAPIKey{}); err != nil {
		t.Fatal(err)
	}
	oldQ, oldKey := *query.Q, query.OpenAPIKey
	*query.Q = *query.Use(db)
	query.OpenAPIKey = &query.Q.OpenAPIKey
	t.Cleanup(func() { *query.Q = oldQ; query.OpenAPIKey = oldKey })
	oldOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&appLog)
	t.Cleanup(func() { logrus.SetOutput(oldOutput) })
	claims := &utils.UserClaims{ID: "fixture-user", TenantID: "fixture-tenant", Authority: "TENANT_ADMIN"}
	if err := (&OpenAPIKey{}).CreateOpenAPIKey(&model.CreateOpenAPIKeyReq{TenantID: "fixture-tenant", Name: "successful-fixture"}, claims); err != nil {
		t.Fatal("valid create failed")
	}
	if err := db.Exec(`CREATE TRIGGER fixture_key_failure BEFORE INSERT ON open_api_keys BEGIN SELECT RAISE(FAIL, 'synthetic-secret-from-trigger'); END`).Error; err != nil {
		t.Fatal("create isolated trigger")
	}
	var generatedKey string
	if err := db.Callback().Create().Before("gorm:create").Register("fixture:capture-key", func(tx *gorm.DB) {
		if keys, ok := tx.Statement.Dest.([]*model.OpenAPIKey); ok && len(keys) == 1 {
			generatedKey = keys[0].APIKey
		}
	}); err != nil {
		t.Fatal("register fixture callback")
	}
	sqlLog.Reset()
	appLog.Reset()
	err = (&OpenAPIKey{}).CreateOpenAPIKey(&model.CreateOpenAPIKeyReq{TenantID: "fixture-tenant", Name: "synthetic-secret-from-parameter"}, claims)
	var code *errcode.Error
	if !errors.As(err, &code) || code.Code != errcode.CodeDBError || code.Data != nil {
		t.Fatal("create failure did not return safe DB error")
	}
	body, marshalErr := json.Marshal(code)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	combined := sqlLog.String() + appLog.String() + string(body) + code.Error()
	if generatedKey == "" {
		t.Fatal("fixture failed to capture the generated key")
	}
	if strings.Contains(combined, generatedKey) || strings.Contains(combined, "synthetic-secret-from-trigger") || strings.Contains(combined, "synthetic-secret-from-parameter") || strings.Contains(strings.ToUpper(sqlLog.String()), "INSERT INTO") {
		t.Fatal("create failure exposed SQL or synthetic sensitive value")
	}
}
