package authcore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	_ "embed"
	"encoding/base64"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"net/mail"
	"sort"
	"strings"
)

//go:embed default_board.json
var defaultBoard string

func (s *Service) decodePassword(ctx context.Context, password, salt string) (string, error) {
	enabled, e := s.Accounts.Flag(ctx, "frontend_res")
	if e != nil {
		return "", e
	}
	if !enabled {
		return password, nil
	}
	if s.PrivateKey == nil {
		return "", ErrUnavailable
	}
	if salt == "" || len(salt) > 128 || len(password) > 4096 {
		return "", ErrUnauthorized
	}
	ciphertext, e := base64.StdEncoding.DecodeString(password)
	if e != nil {
		return "", ErrUnauthorized
	}
	plain, e := rsa.DecryptPKCS1v15(rand.Reader, s.PrivateKey, ciphertext)
	if e != nil || !strings.HasSuffix(string(plain), salt) {
		return "", ErrUnauthorized
	}
	return strings.TrimSuffix(string(plain), salt), nil
}

// Signup preserves the existing opt-in email-registration feature. It never
// creates an account from an unbound external identity.
func (s *Service) Signup(ctx context.Context, in input) (User, error) {
	enabled, e := s.Accounts.Flag(ctx, "enable_reg")
	if e != nil {
		return User{}, e
	}
	if !enabled {
		return User{}, ErrForbidden
	}
	if in.ConfirmPassword != "" && in.ConfirmPassword != in.Password {
		return User{}, ErrUnauthorized
	}
	password, e := s.decodePassword(ctx, in.Password, in.Salt)
	if e != nil {
		return User{}, e
	}
	parsed, e := mail.ParseAddress(in.Email)
	if e != nil || parsed.Address != in.Email || !validPassword(password) || in.PhoneNumber == "" || in.PhonePrefix == "" || len(in.PhoneNumber) > 32 || len(in.PhonePrefix) > 8 || len(in.VerifyCode) != 6 {
		return User{}, ErrUnauthorized
	}
	n, e := s.Redis.Eval(ctx, `if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end;redis.call('DEL',KEYS[1]);return 1`, []string{in.Email + "_code"}, in.VerifyCode).Int()
	if e != nil {
		return User{}, ErrUnavailable
	}
	if n != 1 {
		return User{}, ErrUnauthorized
	}
	a, ok := s.Accounts.(*SQLAccounts)
	if !ok {
		return User{}, ErrUnavailable
	}
	return a.signup(ctx, in.Email, password, in.PhonePrefix+" "+in.PhoneNumber)
}
func (a *SQLAccounts) signup(ctx context.Context, email, password, phone string) (User, error) {
	hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return User{}, ErrUnavailable
	}
	tx, e := a.DB.BeginTx(ctx, nil)
	if e != nil {
		return User{}, ErrUnavailable
	}
	defer tx.Rollback()
	locks := []string{"signup-email:" + email, "signup-phone:" + phone}
	sort.Strings(locks)
	for _, key := range locks {
		if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); e != nil {
			return User{}, ErrUnavailable
		}
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1 OR phone_number=$2)`, email, phone).Scan(&exists); e != nil {
		return User{}, ErrUnavailable
	}
	if exists {
		return User{}, ErrConflict
	}
	u := User{ID: uuid.NewString(), Email: email, Name: email, TenantID: uuid.NewString(), Authority: "TENANT_ADMIN", Status: "N"}
	_, e = tx.ExecContext(ctx, `INSERT INTO users(id,name,email,phone_number,password,status,authority,tenant_id,created_at,updated_at,password_last_updated,remark) VALUES($1,$2,$2,$3,$4,'N','TENANT_ADMIN',$5,now(),now(),now(),(now()+interval '365 days')::text)`, u.ID, email, phone, string(hash), u.TenantID)
	if e != nil {
		return User{}, ErrConflict
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO boards(id,name,config,tenant_id,created_at,updated_at,home_flag) VALUES($1,'Home',$2,$3,now(),now(),'Y')`, uuid.NewString(), defaultBoard, u.TenantID)
	if e != nil {
		return User{}, ErrUnavailable
	}
	if tx.Commit() != nil {
		return User{}, ErrUnavailable
	}
	return u, nil
}
