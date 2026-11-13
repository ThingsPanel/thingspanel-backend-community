package authcore

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"net/mail"
	"strings"
	"unicode"
)

type CompatibilityAccounts interface {
	Detail(context.Context, string) (json.RawMessage, error)
	HasAdmin(context.Context) (bool, error)
	Initialize(context.Context, string, string) (User, error)
}

func (a *SQLAccounts) Detail(ctx context.Context, id string) (json.RawMessage, error) {
	var raw []byte
	e := a.DB.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'id',u.id,'name',u.name,'phone_number',u.phone_number,'email',u.email,'status',u.status,
 'authority',u.authority,'tenant_id',u.tenant_id,'remark',u.remark,'additional_info',u.additional_info,
 'organization',u.organization,'timezone',u.timezone,'default_language',u.default_language,
 'created_at',u.created_at,'updated_at',u.updated_at,'password_last_updated',u.password_last_updated,
 'last_visit_time',u.last_visit_time,'last_visit_ip',u.last_visit_ip,'last_visit_device',u.last_visit_device,
 'password_fail_count',u.password_fail_count,'avatar_url',u.avatar_url,
 'user_roles',COALESCE((SELECT jsonb_agg(v1) FROM casbin_rule WHERE ptype='g' AND v0=u.id),'[]'::jsonb),
 'address',(SELECT to_jsonb(a)-'user_id' FROM user_address a WHERE a.user_id=u.id LIMIT 1))
 FROM users u WHERE u.id=$1`, id).Scan(&raw)
	if e != nil {
		return nil, ErrUnavailable
	}
	return json.RawMessage(raw), nil
}
func (a *SQLAccounts) HasAdmin(ctx context.Context) (bool, error) {
	var yes bool
	e := a.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE authority='SYS_ADMIN')`).Scan(&yes)
	if e != nil {
		return false, ErrUnavailable
	}
	return yes, nil
}
func validPassword(p string) bool {
	if len(p) < 6 || len(p) > 72 {
		return false
	}
	lower, digit := false, false
	for _, c := range p {
		switch {
		case unicode.IsLower(c):
			lower = true
		case unicode.IsDigit(c):
			digit = true
		case unicode.IsUpper(c):
		case strings.ContainsRune("!@#$%^&*()_+-=[]{};\\':\"|,./<>?", c):
		default:
			return false
		}
	}
	return lower && digit
}
func (a *SQLAccounts) Initialize(ctx context.Context, email, password string) (User, error) {
	parsed, e := mail.ParseAddress(email)
	if e != nil || parsed.Address != email || !validPassword(password) {
		return User{}, ErrUnauthorized
	}
	tx, e := a.DB.BeginTx(ctx, nil)
	if e != nil {
		return User{}, ErrUnavailable
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('thingspanel_super_admin_init'))`); e != nil {
		return User{}, ErrUnavailable
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE authority='SYS_ADMIN')`).Scan(&exists); e != nil {
		return User{}, ErrUnavailable
	}
	if exists {
		return User{}, ErrConflict
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return User{}, ErrUnavailable
	}
	u := User{ID: uuid.NewString(), Email: email, Name: email, Authority: "SYS_ADMIN", Status: "N"}
	_, e = tx.ExecContext(ctx, `INSERT INTO users(id,name,email,phone_number,password,status,authority,tenant_id,created_at,updated_at,password_last_updated) VALUES($1,$2,$2,'',$3,'N','SYS_ADMIN','',now(),now(),now())`, u.ID, email, string(hash))
	if e != nil {
		return User{}, ErrConflict
	}
	if tx.Commit() != nil {
		return User{}, ErrUnavailable
	}
	return u, nil
}
