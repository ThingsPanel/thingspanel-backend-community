package mysql

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/go-sql-driver/mysql"
)

func TestDialector_Translate(t *testing.T) {
	normalErr := errors.New("normal error")

	type fields struct {
		Config *Config
	}
	type args struct {
		err error
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   error
	}{
		{
			name: "it should translate error to ErrDuplicatedKey when the error number is 1062",
			args: args{err: &mysql.MySQLError{Number: uint16(1062)}},
			want: gorm.ErrDuplicatedKey,
		},
		{
			name: "it should translate error to ErrForeignKeyViolated when the error number is 1452",
			args: args{err: &mysql.MySQLError{Number: uint16(1452)}},
			want: gorm.ErrForeignKeyViolated,
		},
		{
			name: "it should not translate the error when the error number is not registered in translated error codes",
			args: args{err: &mysql.MySQLError{Number: uint16(8888)}},
			want: &mysql.MySQLError{Number: uint16(8888)},
		},
		{
			name: "it should not translate the error when the error is not a mysql error",
			args: args{err: normalErr},
			want: normalErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialector := Dialector{
				Config: tt.fields.Config,
			}
			if err := dialector.Translate(tt.args.err); !errors.Is(err, tt.want) {
				t.Errorf("Translate() got error = %v, want error %v", err, tt.want)
			}
		})
	}
}
