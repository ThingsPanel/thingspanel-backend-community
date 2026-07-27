package dbresolver

import "testing"

func TestGetTableFromRawSQL(t *testing.T) {
	datas := [][2]string{
		{"select * from users as u", "users"},
		{"select name from users", "users"},
		{"select * from (select * from users) as u", "users"},
		{"select * from (select * from users)", "users"},
		{"select * from (select * from users), (select * from products)", "users"},
		{"select * from users, products", "users"},
		{"select * from users as u, products as p", "users"},
		{"UPDATE users SET column1 = value1, column2 = value2", "users"},
		{"DELETE FROM users WHERE condition;", "users"},
		{"INSERT INTO users (column1, column2) VALUES (v1, v2)", "users"},
		{"insert ignore into users (name,age) VALUES ('jinzhu',18);", "users"},
		{"MERGE INTO users USING ", "users"},
	}

	for _, data := range datas {
		if getTableFromRawSQL(data[0]) != data[1] {
			t.Errorf("Failed to get table name from %v, expect: %v, got: %v", data[0], data[1], getTableFromRawSQL(data[0]))
		}
	}
}
