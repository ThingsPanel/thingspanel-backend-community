package authcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	if a.Dialect == DialectMySQL {
		return a.detailMySQL(ctx, id)
	}
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
	e := a.dx().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE authority='SYS_ADMIN')`).Scan(&yes)
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
	tx, release, e := a.dx().beginLocked(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "thingspanel_super_admin_init")
	if e != nil {
		return User{}, ErrUnavailable
	}
	defer release()
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

// detailMySQL builds the profile document in Go: MySQL's JSON_OBJECT would render timestamps as
// "2026-11-03 01:02:03.123456", PostgreSQL's jsonb as ISO 8601 with a zone offset, and the API
// contract is the latter.
func (a *SQLAccounts) detailMySQL(ctx context.Context, id string) (json.RawMessage, error) {
	stamp := func(t sql.NullTime) any {
		if !t.Valid {
			return nil
		}
		return t.Time.UTC().Format("2006-01-02T15:04:05.999999-07:00")
	}
	text := func(v sql.NullString) any {
		if !v.Valid {
			return nil
		}
		return v.String
	}
	var name, phone, email, status, authority, tenant, remark, organization, timezone, language, visitIP, visitDevice, avatar sql.NullString
	var info []byte
	var created, updated, passwordUpdated, visited sql.NullTime
	var failures sql.NullInt64
	var uid string
	e := a.dx().QueryRowContext(ctx, `SELECT id,name,phone_number,email,status,authority,tenant_id,remark,additional_info,organization,timezone,default_language,created_at,updated_at,password_last_updated,last_visit_time,last_visit_ip,last_visit_device,password_fail_count,avatar_url FROM users WHERE id=$1`, id).Scan(
		&uid, &name, &phone, &email, &status, &authority, &tenant, &remark, &info, &organization, &timezone, &language, &created, &updated, &passwordUpdated, &visited, &visitIP, &visitDevice, &failures, &avatar)
	if e != nil {
		return nil, ErrUnavailable
	}
	doc := map[string]any{
		"id": uid, "name": text(name), "phone_number": text(phone), "email": text(email), "status": text(status),
		"authority": text(authority), "tenant_id": text(tenant), "remark": text(remark),
		"organization": text(organization), "timezone": text(timezone), "default_language": text(language),
		"created_at": stamp(created), "updated_at": stamp(updated), "password_last_updated": stamp(passwordUpdated),
		"last_visit_time": stamp(visited), "last_visit_ip": text(visitIP), "last_visit_device": text(visitDevice),
		"avatar_url": text(avatar), "additional_info": nil, "password_fail_count": nil,
	}
	if len(info) > 0 {
		doc["additional_info"] = json.RawMessage(info)
	}
	if failures.Valid {
		doc["password_fail_count"] = failures.Int64
	}
	roles := []any{}
	rows, e := a.dx().QueryContext(ctx, `SELECT v1 FROM casbin_rule WHERE ptype='g' AND v0=$1 ORDER BY id`, id)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var role sql.NullString
		if rows.Scan(&role) != nil {
			return nil, ErrUnavailable
		}
		roles = append(roles, text(role))
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	doc["user_roles"] = roles
	doc["address"] = nil
	var aid int64
	var s [11]sql.NullString
	var createdTime, updatedTime sql.NullTime
	switch e := a.dx().QueryRowContext(ctx, `SELECT id,country,province,city,district,street,detailed_address,postal_code,address_label,longitude,latitude,additional_info,created_time,updated_time FROM user_address WHERE user_id=$1 LIMIT 1`, id).Scan(
		&aid, &s[0], &s[1], &s[2], &s[3], &s[4], &s[5], &s[6], &s[7], &s[8], &s[9], &s[10], &createdTime, &updatedTime); {
	case e == nil:
		doc["address"] = map[string]any{"id": aid, "country": text(s[0]), "province": text(s[1]), "city": text(s[2]), "district": text(s[3]),
			"street": text(s[4]), "detailed_address": text(s[5]), "postal_code": text(s[6]), "address_label": text(s[7]),
			"longitude": text(s[8]), "latitude": text(s[9]), "additional_info": text(s[10]),
			"created_time": stamp(createdTime), "updated_time": stamp(updatedTime)}
	case !errors.Is(e, sql.ErrNoRows):
		return nil, ErrUnavailable
	}
	out, e := json.Marshal(doc)
	if e != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}
