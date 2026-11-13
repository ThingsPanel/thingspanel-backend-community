package authcore

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
)

// SQLAccounts uses the existing community account schema. Only the community
// SQL migration runner owns this schema; Encore must not create a second users DB.
type SQLAccounts struct{ DB *sql.DB }

const userColumns = `id,email,COALESCE(name,''),COALESCE(tenant_id,''),COALESCE(authority,''),COALESCE(status,''),password,auth_version`

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	e := row.Scan(&u.ID, &u.Email, &u.Name, &u.TenantID, &u.Authority, &u.Status, &u.Password, &u.Version)
	if errors.Is(e, sql.ErrNoRows) {
		return u, ErrUnauthorized
	}
	if e != nil {
		return u, ErrUnavailable
	}
	return u, nil
}
func (a *SQLAccounts) User(ctx context.Context, key string, login bool) (User, error) {
	if login {
		// Ambiguous phone aliases must not choose an arbitrary account.
		rows, e := a.DB.QueryContext(ctx, `SELECT `+userColumns+` FROM users WHERE email=$1 OR phone_number=$1 OR ($1 ~ '^[0-9]{6,20}$' AND split_part(phone_number,' ',2)=$1) LIMIT 2`, key)
		if e != nil {
			return User{}, ErrUnavailable
		}
		defer rows.Close()
		if !rows.Next() {
			if rows.Err() != nil {
				return User{}, ErrUnavailable
			}
			return User{}, ErrUnauthorized
		}
		u, e := scanUser(rows)
		if e != nil {
			return u, e
		}
		if rows.Next() {
			return User{}, ErrUnauthorized
		}
		if rows.Err() != nil {
			return User{}, ErrUnavailable
		}
		return u, nil
	}
	return scanUser(a.DB.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, key))
}
func (a *SQLAccounts) Flag(ctx context.Context, name string) (bool, error) {
	var flag string
	e := a.DB.QueryRowContext(ctx, `SELECT enable_flag FROM sys_function WHERE name=$1`, name).Scan(&flag)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, ErrUnavailable
	}
	return flag == "enable", nil
}
func (a *SQLAccounts) FindIdentity(ctx context.Context, i Identity) (User, error) {
	var uid string
	e := a.DB.QueryRowContext(ctx, `SELECT user_id FROM auth_external_bindings WHERE issuer=$1 AND subject=$2 AND provider=$3 AND client_id=$4 AND enabled`, i.Issuer, i.Subject, i.Provider, i.ClientID).Scan(&uid)
	if errors.Is(e, sql.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	if e != nil {
		return User{}, ErrUnavailable
	}
	return a.User(ctx, uid, false)
}
func (a *SQLAccounts) Bind(ctx context.Context, u User, i Identity) error {
	if e := IdentityValid(i); e != nil {
		return e
	}
	tx, e := a.DB.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	current, e := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, u.ID))
	if e != nil {
		return e
	}
	if admissible(current) != nil || current.Version != u.Version {
		return ErrUnauthorized
	}
	// The unique index arbitrates races across accounts. Never update ownership.
	_, e = tx.ExecContext(ctx, `INSERT INTO auth_external_bindings(id,user_id,issuer,subject,provider,client_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(issuer,subject) DO NOTHING`, uuid.NewString(), u.ID, i.Issuer, i.Subject, i.Provider, i.ClientID)
	if e != nil {
		return ErrUnavailable
	}
	var owner, provider, client string
	var enabled, rebind bool
	e = tx.QueryRowContext(ctx, `SELECT user_id,provider,client_id,enabled,(self_unlinked OR COALESCE(rebind_until>now(),false)) FROM auth_external_bindings WHERE issuer=$1 AND subject=$2`, i.Issuer, i.Subject).Scan(&owner, &provider, &client, &enabled, &rebind)
	if e != nil {
		return ErrUnavailable
	}
	if owner != u.ID || provider != i.Provider || client != i.ClientID || (!enabled && !rebind) {
		return ErrConflict
	}
	if !enabled {
		if _, e = tx.ExecContext(ctx, `UPDATE auth_external_bindings SET enabled=true,self_unlinked=false,disabled_at=NULL,rebind_until=NULL WHERE issuer=$1 AND subject=$2 AND user_id=$3`, i.Issuer, i.Subject, u.ID); e != nil {
			return ErrUnavailable
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO auth_audit_events(actor_id,user_id,event,provider) VALUES($1,$1,'identity_bound',$2)`, u.ID, i.Provider)
	if e != nil {
		return ErrUnavailable
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}
func (a *SQLAccounts) Bindings(ctx context.Context, actor User) ([]Binding, error) {
	rows, e := a.DB.QueryContext(ctx, `SELECT id,provider,enabled,created_at,self_unlinked FROM auth_external_bindings WHERE user_id=$1 ORDER BY created_at`, actor.ID)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		var b Binding
		if rows.Scan(&b.ID, &b.Provider, &b.Enabled, &b.CreatedAt, &b.SelfUnlinked) != nil {
			return nil, ErrUnavailable
		}
		out = append(out, b)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}
func (a *SQLAccounts) Disable(ctx context.Context, actor User, bindingID string) error {
	return a.disable(ctx, actor, bindingID, false)
}

// Unlink requires a fresh password in the HTTP adapter. Keep the identity owner
// reserved so another platform account cannot claim a released identity.
func (a *SQLAccounts) Unlink(ctx context.Context, actor User, bindingID string) error {
	return a.disable(ctx, actor, bindingID, true)
}

func (a *SQLAccounts) disable(ctx context.Context, actor User, bindingID string, self bool) error {
	if actor.Delegated {
		return ErrForbidden
	}
	tx, e := a.DB.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	var uid string
	e = tx.QueryRowContext(ctx, `SELECT user_id FROM auth_external_bindings WHERE id=$1`, bindingID).Scan(&uid)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrForbidden
	}
	if e != nil {
		return ErrUnavailable
	}
	// Serialize with bind and all other credential version updates on this user.
	target, e := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, uid))
	if e != nil {
		return e
	}
	if self {
		if actor.ID != target.ID || actor.Version != target.Version || admissible(target) != nil {
			return ErrForbidden
		}
	} else if actor.Authority != "SYS_ADMIN" && !(actor.Authority == "TENANT_ADMIN" && actor.TenantID == target.TenantID && target.Authority == "TENANT_USER") {
		return ErrForbidden
	}
	// Self-service cannot turn an administrator's block into a voluntary unlink.
	result, e := tx.ExecContext(ctx, `UPDATE auth_external_bindings SET enabled=false,self_unlinked=$2,disabled_at=now(),rebind_until=NULL WHERE id=$1 AND (enabled OR (NOT $2 AND (self_unlinked OR rebind_until IS NOT NULL)))`, bindingID, self)
	if e != nil {
		return ErrUnavailable
	}
	n, e := result.RowsAffected()
	if e != nil {
		return ErrUnavailable
	}
	if n > 0 {
		if _, e = tx.ExecContext(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, uid); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.ExecContext(ctx, `UPDATE app_push_devices SET active=false WHERE user_id=$1`, uid); e != nil {
			return ErrUnavailable
		}
		event := "identity_disabled"
		if self {
			event = "identity_unlinked"
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO auth_audit_events(actor_id,user_id,event,provider) SELECT $1,user_id,$3,provider FROM auth_external_bindings WHERE id=$2`, actor.ID, bindingID, event); e != nil {
			return ErrUnavailable
		}
	} else if self {
		return ErrConflict
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

// AllowRebind grants a short window to the ORIGINAL owner only. It neither
// enables login nor moves ownership. Bind still requires password + fresh proof.
func (a *SQLAccounts) AllowRebind(ctx context.Context, actor User, bindingID string) error {
	if actor.Delegated {
		return ErrForbidden
	}
	tx, e := a.DB.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	var uid string
	if e = tx.QueryRowContext(ctx, `SELECT user_id FROM auth_external_bindings WHERE id=$1`, bindingID).Scan(&uid); e != nil {
		return ErrForbidden
	}
	target, e := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, uid))
	if e != nil {
		return e
	}
	if actor.Authority != "SYS_ADMIN" && !(actor.Authority == "TENANT_ADMIN" && actor.TenantID == target.TenantID && target.Authority == "TENANT_USER") {
		return ErrForbidden
	}
	if admissible(target) != nil {
		return ErrForbidden
	}
	result, e := tx.ExecContext(ctx, `UPDATE auth_external_bindings SET rebind_until=now()+interval '10 minutes' WHERE id=$1 AND NOT enabled`, bindingID)
	if e != nil {
		return ErrUnavailable
	}
	n, e := result.RowsAffected()
	if e != nil {
		return ErrUnavailable
	}
	if n != 1 {
		return ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO auth_audit_events(actor_id,user_id,event,provider) SELECT $1,user_id,'identity_rebind_approved',provider FROM auth_external_bindings WHERE id=$2`, actor.ID, bindingID); e != nil {
		return ErrUnavailable
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}
