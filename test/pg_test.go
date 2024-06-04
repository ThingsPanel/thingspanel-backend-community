/*
 * @Author: smith
 * @Date: 2024-3-7 22:15:38
 * @LastEditTime: 2024-3-7 22:15:38
 * @LastEditors: smith
 * @Description: In User Settings Edit
 * @FilePath: /irrigation-iot-platform/test/example_test.go
 * 使用单元测试初始化数据库以及必要的测试数据
 */

package test

import (
	"os"
	"testing"
	"time"

	"project/initialize"

	"project/model"
	"project/query"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var adr = func(s string) *string { return &s }
var config *initialize.DbConfig
var db *gorm.DB

func TestDatebase(t *testing.T) {
	// 要保证测试顺序，下面的函数都不能以Test开头
	testConnect(t)
	testDDLInit(t)
	testNotificationGroup(t)
}

func testConnect(t *testing.T) {
	require := require.New(t)
	if os.Getenv("run_env") == "git-actions" {
		initialize.ViperInit("../configs/conf-push-test.yml")
	} else if os.Getenv("run_env") == "localdev" {
		initialize.ViperInit("../configs/conf-localdev.yml")
	} else {
		t.Log("未知环境")
		return
	}
	var err error
	config, err = initialize.LoadDbConfig()
	require.Nil(err)
	db, err = initialize.PgConnect(config)
	require.Nil(err)
}

func testDDLInit(t *testing.T) {
	require := require.New(t)

	// 清空数据库所有的表
	res := db.Exec("DROP SCHEMA public CASCADE;CREATE SCHEMA public;")
	require.Nil(res.Error)

	// 切换到新创建的数据库
	db, err := initialize.PgConnect(config)
	require.Nil(err)

	// ts := db.Exec("CREATE TABLE sys_version (version_number INT NOT NULL DEFAULT 0, version varchar(255) NOT NULL, PRIMARY KEY (version_number))")
	// err = ts.Error
	// require.Nilf(err,"CREATE TABLE sys_version error %v",err)

	// 执行1.sql文件
	err = initialize.ExecuteSQLFile(db, "../sql/1.sql")
	require.Nilf(err, "执行ddl失败%v", err)

	require.Nilf(err, "ddl提交失败%v", err)
	t.Log("初始化数据库成功")
}

func testNotificationGroup(t *testing.T) {
	require := require.New(t)
	require.NotNil(db, "数据库连接失败")
	query.SetDefault(db)

	// 创建测试数据
	notificationGroup := model.NotificationGroup{
		Name:               "test",
		NotificationType:   "MEMBER",
		Status:             "ON",
		NotificationConfig: adr("{}"),
		Description:        adr("test"),
		TenantID:           "123456",
		Remark:             adr("test"),
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	err := query.NotificationGroup.Create(&notificationGroup)
	require.Nil(err, "创建数据notificationGroup失败")
	db.Commit()
}
