package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"encore.app/l3-core/notification"
)

type serviceRenewPolicy struct {
	Version         int    `json:"version"`
	DeploymentID    string `json:"deployment_id"`
	TenantID        string `json:"tenant_id"`
	CredentialsFile string `json:"credentials_file"`
	RuntimeFile     string `json:"runtime_file"`
	ReceiptFile     string `json:"receipt_file"`
}

func readStrictPrivateJSON(path string, value any) error {
	if err := requireRootPrivatePath(path, false); err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 4<<20 {
		return errors.New("private renewal input unavailable")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return errors.New("invalid private renewal input")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("invalid private renewal input")
	}
	return nil
}

func validateServicePair(policy serviceRenewPolicy, pair notification.ProvisionedSourceCredentials, ttl time.Duration) error {
	if policy.Version != 1 || policy.DeploymentID == "" || policy.TenantID == "" || ttl <= 0 || ttl > 8*time.Hour || pair.DeploymentID != policy.DeploymentID || len(pair.AllowedTenantIDs) != 1 || pair.AllowedTenantIDs[0] != policy.TenantID || len(pair.SourceCredential) < 32 || len(pair.ProjectionCredential) < 32 || pair.SourceCredential == pair.ProjectionCredential {
		return errors.New("service credential scope rejected")
	}
	return nil
}

func runServiceRenew(policyFile string, ttl time.Duration) error {
	var policy serviceRenewPolicy
	if err := readStrictPrivateJSON(policyFile, &policy); err != nil {
		return err
	}
	var pair notification.ProvisionedSourceCredentials
	if err := readStrictPrivateJSON(policy.CredentialsFile, &pair); err != nil {
		return err
	}
	if err := validateServicePair(policy, pair, ttl); err != nil {
		return err
	}
	if err := requireRootPrivatePath(policy.RuntimeFile, false); err != nil {
		return err
	}
	if err := requireRootPrivatePath(policy.ReceiptFile, true); err != nil {
		return err
	}
	// Connection settings use the same validated Encore infra mapping as issuance.
	db, err := openNotificationDB(policy.RuntimeFile)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := notification.NewStore(notification.StoreOptions{DB: db})
	if err != nil {
		return errors.New("notification renewal store unavailable")
	}
	source := sha256.Sum256([]byte(pair.SourceCredential))
	projection := sha256.Sum256([]byte(pair.ProjectionCredential))
	expires := time.Now().UTC().Add(ttl)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.RenewSourceProjectionCredentialPair(ctx, policy.DeploymentID, []string{policy.TenantID}, hex.EncodeToString(source[:]), hex.EncodeToString(projection[:]), expires); err != nil {
		return errors.New("existing active credential pair could not be renewed")
	}
	pair.ExpiresAt = expires
	if err := replaceRootPrivateJSON(policy.CredentialsFile, pair); err != nil {
		return err
	}
	return replaceRootPrivateJSON(policy.ReceiptFile, struct {
		Status     string    `json:"status"`
		Deployment string    `json:"deployment_id"`
		Tenant     string    `json:"tenant_id"`
		Expires    time.Time `json:"expires_at"`
		Renewed    time.Time `json:"renewed_at"`
	}{"passed", policy.DeploymentID, policy.TenantID, expires, time.Now().UTC()})
}

func replaceRootPrivateJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("renewal result encoding failed")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".notification-renew-*")
	if err != nil {
		return errors.New("renewal result unavailable")
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("renewal result write failed")
	}
	if os.Rename(temporary, path) != nil {
		return errors.New("renewal result publish failed")
	}
	return nil
}
