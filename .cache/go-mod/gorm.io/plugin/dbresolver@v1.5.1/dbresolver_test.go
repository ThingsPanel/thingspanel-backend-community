package dbresolver_test

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"
)

type User struct {
	ID     uint
	Name   string
	Orders []Order
}

type Product struct {
	ID   uint
	Name string
}

type Order struct {
	ID      uint
	OrderNo string
	UserID  uint
}

func GetDB(port int) *gorm.DB {
	DB, err := gorm.Open(mysql.Open(fmt.Sprintf("gorm:gorm@tcp(localhost:%v)/gorm?charset=utf8&parseTime=True&loc=Local", port)), &gorm.Config{})
	if err != nil {
		panic(fmt.Sprintf("failed to connect db, got error: %v, port: %v", err, port))
	}
	return DB
}

func init() {
	for _, port := range []int{9911, 9912, 9913, 9914} {
		DB := GetDB(port)
		DB.Migrator().DropTable(&User{}, &Product{}, &Order{})
		DB.AutoMigrate(&User{}, &Product{}, &Order{})

		user := User{Name: fmt.Sprintf("%v", port)}
		DB.Create(&user)
		DB.Create(&Product{Name: fmt.Sprintf("%v", port)})
		DB.Create(&Order{OrderNo: fmt.Sprintf("%v", port), UserID: user.ID})
	}
}

func TestDBResolver(t *testing.T) {
	for i := 0; i < 2; i++ {
		DB, err := gorm.Open(mysql.Open("gorm:gorm@tcp(localhost:9911)/gorm?charset=utf8&parseTime=True&loc=Local"), &gorm.Config{PrepareStmt: i%2 == 0})
		if err != nil {
			t.Fatalf("failed to connect db, got error: %v", err)
		}
		if debug := os.Getenv("DEBUG"); debug == "true" {
			DB.Logger = DB.Logger.LogMode(logger.Info)
		} else if debug == "false" {
			DB.Logger = DB.Logger.LogMode(logger.Silent)
		}

		if err := DB.Use(dbresolver.Register(dbresolver.Config{
			Sources: []gorm.Dialector{mysql.Open("gorm:gorm@tcp(localhost:9911)/gorm?charset=utf8&parseTime=True&loc=Local")},
			Replicas: []gorm.Dialector{
				mysql.Open("gorm:gorm@tcp(localhost:9912)/gorm?charset=utf8&parseTime=True&loc=Local"),
				mysql.Open("gorm:gorm@tcp(localhost:9913)/gorm?charset=utf8&parseTime=True&loc=Local"),
			},
			TraceResolverMode: true,
		}).Register(dbresolver.Config{
			Sources:           []gorm.Dialector{mysql.Open("gorm:gorm@tcp(localhost:9914)/gorm?charset=utf8&parseTime=True&loc=Local")},
			Replicas:          []gorm.Dialector{mysql.Open("gorm:gorm@tcp(localhost:9913)/gorm?charset=utf8&parseTime=True&loc=Local")},
			TraceResolverMode: true,
		}, "users", &Product{}).SetMaxOpenConns(5)); err != nil {
			t.Fatalf("failed to use plugin, got error: %v", err)
		}

		for j := 0; j < 20; j++ {
			var order Order
			// test transaction
			tx := DB.Begin()
			tx.Find(&order)
			if order.OrderNo != "9911" {
				t.Fatalf("idx: %v: order should comes from default db, but got order %v", j, order.OrderNo)
			}
			tx.Rollback()

			tx = DB.Clauses(dbresolver.Read).Begin()
			tx.Find(&order)
			if order.OrderNo != "9912" && order.OrderNo != "9913" {
				t.Fatalf("idx: %v: order should comes from read db, but got order %v", j, order.OrderNo)
			}
			tx.Rollback()

			tx = DB.Clauses(dbresolver.Write).Begin()
			tx.Find(&order)
			if order.OrderNo != "9911" {
				t.Fatalf("idx: %v: order should comes from write db, but got order %v", j, order.OrderNo)
			}
			tx.Rollback()

			tx = DB.Clauses(dbresolver.Use("users"), dbresolver.Write).Begin()
			tx.Find(&order)
			if order.OrderNo != "9914" {
				t.Fatalf("idx: %v: order should comes from users, write db, but got order %v", j, order.OrderNo)
			}
			tx.Rollback()

			tx = DB.Clauses(dbresolver.Write, dbresolver.Use("users")).Begin()
			tx.Find(&order)
			if order.OrderNo != "9914" {
				t.Fatalf("idx: %v: order should comes from users, write db, but got order %v", j, order.OrderNo)
			}
			tx.Rollback()

			// test query
			DB.First(&order)
			if order.OrderNo != "9912" && order.OrderNo != "9913" {
				t.Fatalf("idx: %v: order should comes from read db, but got order %v", j, order.OrderNo)
			}

			DB.Clauses(dbresolver.Write).First(&order)
			if order.OrderNo != "9911" {
				t.Fatalf("idx: %v: order should comes from write db, but got order %v", j, order.OrderNo)
			}

			DB.Clauses(dbresolver.Use("users")).First(&order)
			if order.OrderNo != "9913" {
				t.Fatalf("idx: %v: order should comes from write db @ users, but got order %v", j, order.OrderNo)
			}

			DB.Clauses(dbresolver.Use("users"), dbresolver.Write).First(&order)
			if order.OrderNo != "9914" {
				t.Fatalf("idx: %v: order should comes from write db @ users, but got order %v", j, order.OrderNo)
			}

			var user User
			DB.First(&user)
			if user.Name != "9913" {
				t.Fatalf("idx: %v: user should comes from read db, but got %v", j, user.Name)
			}

			DB.Clauses(dbresolver.Write).First(&user)
			if user.Name != "9914" {
				t.Fatalf("idx: %v: user should comes from read db, but got %v", j, user.Name)
			}

			var product Product
			DB.First(&product)
			if product.Name != "9913" {
				t.Fatalf("idx: %v: product should comes from read db, but got %v", j, product.Name)
			}

			DB.Clauses(dbresolver.Write).First(&product)
			if product.Name != "9914" {
				t.Fatalf("idx: %v: product should comes from write db, but got %v", j, product.Name)
			}

			// test preload
			if err := DB.Clauses(dbresolver.Write).Preload("Orders").First(&user).Error; err != nil || len(user.Orders) != 1 {
				t.Fatalf("failed to preload orders, count: %v, got error: %v", len(user.Orders), err)
			}

			// order source 9911, user source: 9914
			if user.Orders[0].OrderNo != "9911" || user.Name != "9914" {
				t.Fatalf("incorrect order info: userName: %v, orderNo: %v", user.Name, user.Orders[0].OrderNo)
			}

			if err := DB.Preload("Orders", func(tx *gorm.DB) *gorm.DB {
				return tx.Clauses(dbresolver.Write)
			}).First(&user).Error; err != nil || len(user.Orders) != 1 {
				t.Fatalf("failed to preload orders, count: %v, got error: %v", len(user.Orders), err)
			}

			// order source 9911, user replica: 9913
			if user.Orders[0].OrderNo != "9911" || user.Name != "9913" {
				t.Fatalf("incorrect order info: userName: %v, orderNo: %v", user.Name, user.Orders[0].OrderNo)
			}

			// test create
			DB.Create(&User{Name: "create"})
			if err := DB.First(&User{}, "name = ?", "create").Error; err == nil {
				t.Fatalf("can't read user from read db, got no error happened")
			}

			if err := DB.Clauses(dbresolver.Write).First(&User{}, "name = ?", "create").Error; err != nil {
				t.Fatalf("read user from write db, got error: %v", err)
			}

			DB9914 := GetDB(9914)

			if err := DB9914.First(&User{}, "name = ?", "create").Error; err != nil {
				t.Fatalf("read user from write db, got error: %v", err)
			}

			var name string
			if err := DB.Raw("select name from users").Row().Scan(&name); err != nil || name != "9913" {
				t.Fatalf("read users from read db, name %v", name)
			}

			if err := DB.Debug().Raw("select name from users where name = ? for update", "create").Row().Scan(&name); err != nil || name != "create" {
				t.Fatalf("read users from write db, name %v, err %v", name, err)
			}

			// test update
			if err := DB.Model(&User{}).Where("name = ?", "create").Update("name", "update").Error; err != nil {
				t.Fatalf("failed to update users, got error: %v", err)
			}

			if err := DB9914.First(&User{}, "name = ?", "update").Error; err != nil {
				t.Fatalf("read user from write db, got error: %v", err)
			}

			// test raw sql
			name = ""
			if err := DB.Raw("select name from users where name = ?", "update").Row().Scan(&name); err == nil || name != "" {
				t.Fatalf("can't read users from read db, name %v", name)
			}

			if err := DB.Raw(" select name from users where name = ?", "9913").Row().Scan(&name); err != nil {
				t.Fatalf("(raw sql has leading space) should go to read db, got error: %v", err)
			}

			if err := DB.Raw(`
select name
from users where name = ?`, "9913").Row().Scan(&name); err != nil {
				t.Fatalf("(raw sql has leading newline) should go to read db, got error: %v", err)
			}

			if err := DB.Clauses(dbresolver.Write).Raw("select name from users where name = ?", "update").Row().Scan(&name); err != nil || name != "update" {
				t.Fatalf("read users from write db, error %v, name %v", err, name)
			}

			// test delete
			if err := DB.Where("name = ?", "update").Delete(&User{}).Error; err != nil {
				t.Fatalf("failed to delete users, got error: %v", err)
			}

			if err := DB9914.First(&User{}, "name = ?", "update").Error; err != gorm.ErrRecordNotFound {
				t.Fatalf("read user from write db after delete, got error: %v", err)
			}
		}
	}
}

func TestConnPool(t *testing.T) {
	for i := 0; i < 2; i++ {
		DB, err := gorm.Open(mysql.Open("gorm:gorm@tcp(localhost:9911)/gorm?charset=utf8&parseTime=True&loc=Local"), &gorm.Config{PrepareStmt: i%2 == 0})
		if err != nil {
			t.Fatalf("failed to connect db, got error: %v", err)
		}
		if debug := os.Getenv("DEBUG"); debug == "true" {
			DB.Logger = DB.Logger.LogMode(logger.Info)
		} else if debug == "false" {
			DB.Logger = DB.Logger.LogMode(logger.Silent)
		}

		if err := DB.Use(dbresolver.Register(dbresolver.Config{
			Sources:           []gorm.Dialector{mysql.Open("gorm:gorm@tcp(localhost:9912)/gorm?charset=utf8&parseTime=True&loc=Local")},
			Replicas:          []gorm.Dialector{mysql.Open("gorm:gorm@tcp(localhost:9913)/gorm?charset=utf8&parseTime=True&loc=Local")},
			TraceResolverMode: true,
		}).Register(dbresolver.Config{
			Sources:           []gorm.Dialector{mysql.Open("gorm:gorm@tcp(localhost:9914)/gorm?charset=utf8&parseTime=True&loc=Local")},
			Replicas:          []gorm.Dialector{mysql.Open("gorm:gorm@tcp(localhost:9913)/gorm?charset=utf8&parseTime=True&loc=Local")},
			TraceResolverMode: true,
		}, "users", &Product{}).SetMaxOpenConns(5)); err != nil {
			t.Fatalf("failed to use plugin, got error: %v", err)
		}

		tests := []struct {
			name string
			db   *gorm.DB
			want int
		}{
			{"global", DB, 9911},
			{"source", DB.Clauses(dbresolver.Write), 9912},
			{"replica", DB.Clauses(dbresolver.Read), 9913},
			{"table global", DB.Table("users"), 9911},
			{"table source", DB.Table("users").Clauses(dbresolver.Write), 9914},
			{"table replica", DB.Table("users").Clauses(dbresolver.Read), 9913},
			{"model global", DB.Model(&Product{}), 9911},
			{"model source", DB.Model(&Product{}).Clauses(dbresolver.Write), 9914},
			{"model replica", DB.Model(&Product{}).Clauses(dbresolver.Read), 9913},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db, err := tt.db.DB()
				if err != nil {
					t.Fatalf("failed to get *sql.DB, got error: %v", err)
				}

				var got int
				if err := db.QueryRow("SELECT order_no FROM orders LIMIT 1").Scan(&got); err != nil {
					t.Fatalf("failed to get order_no, got error: %v", err)
				}

				if got != tt.want {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			})
		}
	}
}
