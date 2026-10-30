package devicemetadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
)

const nativeCredentialWriteMaxBytes = 64 << 10

type nativeCreateCredentialInput struct {
	Kind             string
	PublicIdentifier *string
	Secret           *string
	CertificatePEM   *string
}

type nativeCredentialSecret struct {
	Credential NativeCredentialSummary `json:"credential"`
	Secret     *string                 `json:"secret"`
	ExpiresAt  time.Time               `json:"expiresAt"`
}

type nativeCredentialSecretResponse struct {
	Data nativeCredentialSecret `json:"data"`
	Meta RequestMeta            `json:"meta"`
}

type nativeCredentialWriteIdentity struct {
	nativeDeviceReadIdentity
	subjectID string
}

func currentNativeCredentialWriteIdentity() (nativeCredentialWriteIdentity, error) {
	read, err := currentNativeDeviceReadIdentity()
	if err != nil {
		return nativeCredentialWriteIdentity{}, err
	}
	data := authn.CurrentData()
	if !authz.Can(data, "device", "credentials:write") {
		return nativeCredentialWriteIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to write device credentials"}
	}
	if data == nil || !validAccessTemplateText(data.UserID, 1, 128, true) {
		return nativeCredentialWriteIdentity{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated subject"}
	}
	return nativeCredentialWriteIdentity{nativeDeviceReadIdentity: read, subjectID: data.UserID}, nil
}

func decodeNativeCreateCredential(body []byte) (nativeCreateCredentialInput, error) {
	var out nativeCreateCredentialInput
	fields, err := strictJSONObject(body)
	if err != nil {
		return out, err
	}
	if err := rejectUnknown(fields, setOf("kind", "publicIdentifier", "secret", "certificatePem")); err != nil {
		return out, err
	}
	if err := requiredEnum(fields, "kind", []string{"token", "username_password", "x509"}, &out.Kind); err != nil {
		return out, err
	}
	if raw, ok := fields["publicIdentifier"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var value string
		if err := decodeString(raw, "publicIdentifier", &value); err != nil {
			return out, err
		}
		if utf8.RuneCountInString(value) > 256 {
			return out, invalid("publicIdentifier", "must be at most 256 characters")
		}
		if strings.IndexByte(value, 0) >= 0 {
			return out, invalid("publicIdentifier", "must not contain NUL")
		}
		if value != "" {
			out.PublicIdentifier = &value
		}
	}
	if raw, ok := fields["secret"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var value string
		if err := decodeString(raw, "secret", &value); err != nil {
			return out, err
		}
		if len([]byte(value)) < 1 || len([]byte(value)) > maxCredentialSecretBytes {
			return out, invalid("secret", "must be between 1 and 8192 UTF-8 bytes")
		}
		out.Secret = &value
	}
	if raw, ok := fields["certificatePem"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var value string
		if err := decodeString(raw, "certificatePem", &value); err != nil {
			return out, err
		}
		if len([]byte(value)) < 1 || len([]byte(value)) > 32768 {
			return out, invalid("certificatePem", "must be between 1 and 32768 UTF-8 bytes")
		}
		out.CertificatePEM = &value
	}
	if out.Kind == "x509" {
		if out.Secret != nil {
			return out, invalid("secret", "must not be supplied for x509 credentials")
		}
		if out.CertificatePEM == nil {
			return out, invalid("certificatePem", "is required for x509 credentials")
		}
	} else if out.CertificatePEM != nil {
		return out, invalid("certificatePem", "is only allowed for x509 credentials")
	}
	return out, nil
}

func parseNativePublicCertificate(pemText string) ([]byte, error) {
	trimmed := bytes.TrimSpace([]byte(pemText))
	if !bytes.HasPrefix(trimmed, []byte("-----BEGIN CERTIFICATE-----")) || bytes.Contains(bytes.ToUpper(trimmed), []byte("PRIVATE KEY")) {
		return nil, invalid("certificatePem", "must contain exactly one public X.509 certificate")
	}
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, invalid("certificatePem", "must contain exactly one public X.509 certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, invalid("certificatePem", "must contain a valid public X.509 certificate")
	}
	now := time.Now()
	if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		return nil, invalid("certificatePem", "certificate is outside its validity period")
	}
	return append([]byte(nil), block.Bytes...), nil
}

func createNativeDeviceCredential(ctx context.Context, identity nativeCredentialWriteIdentity, deviceID, key string, body []byte, requestID string, protector *CredentialSecretProtector) (*metadataWriteResponse, error) {
	deviceID, err := nativeReadUUID(deviceID)
	if err != nil {
		return nil, invalid("deviceId", "must be a UUID")
	}
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeCreateCredential(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	if protector == nil {
		return nil, credentialProtectionUnavailable()
	}
	var material []byte
	responseSecret := input.Secret
	var publicIdentifier = input.PublicIdentifier
	var certificateDER []byte
	if input.Kind == "x509" {
		certificateDER, err = parseNativePublicCertificate(*input.CertificatePEM)
		if err != nil {
			return nil, err
		}
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "createDeviceCredential",
		Method: http.MethodPost, Path: "/api/v2/devices/" + deviceID + "/credentials", Key: key,
		RequestID: requestID, CanonicalBody: canonical,
	}
	owner := CredentialAAD{TenantID: identity.tenantID, OwnerKind: CredentialOwnerDevice, OwnerID: deviceID, Purpose: CredentialPurposeDeviceAccess}
	result, err := runCredentialSecretWriteTx(ctx, command, owner, protector,
		func(tx *sqldb.Tx) error {
			data := authn.CurrentData()
			if data == nil || data.UserID != identity.subjectID || data.TenantID != identity.tenantID {
				return &errs.Error{Code: errs.Unauthenticated, Message: "authenticated credential identity changed"}
			}
			if !authz.Can(data, "device", "credentials:write") {
				return &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to write device credentials"}
			}
			if !identity.permits(deviceID) {
				return &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			var liveDevice string
			err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`, identity.tenantID, deviceID).Scan(&liveDevice)
			if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			return err
		},
		func(tx *sqldb.Tx, aad CredentialAAD, expiresAt time.Time) (metadataWriteResponse, metadataWriteAudit, error) {
			mode := "encrypted"
			var protected ProtectedCredential
			var fingerprint []byte
			fingerprintKeyID := ""
			publicCertificate := ""
			if input.Kind == "x509" {
				mode = "public_certificate"
				fingerprintSum := sha256.Sum256(certificateDER)
				fingerprint = fingerprintSum[:]
				publicCertificate = *input.CertificatePEM
			} else {
				if responseSecret == nil {
					generated, generateErr := GenerateCredentialSecret()
					if generateErr != nil {
						return metadataWriteResponse{}, metadataWriteAudit{}, credentialProtectionUnavailable()
					}
					responseSecret = &generated
				}
				material = []byte(*responseSecret)
				var sealErr error
				protected, sealErr = protector.Seal(aad, material)
				if sealErr != nil {
					return metadataWriteResponse{}, metadataWriteAudit{}, credentialProtectionUnavailable()
				}
				summary, fpErr := protector.Fingerprint(aad, material)
				if fpErr != nil {
					return metadataWriteResponse{}, metadataWriteAudit{}, credentialProtectionUnavailable()
				}
				fingerprint = summary.Fingerprint
				fingerprintKeyID = summary.KeyID
				if input.Kind == "username_password" && publicIdentifier == nil {
					username := "tp-" + aad.CredentialID
					publicIdentifier = &username
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO device_credentials(
				id,tenant_id,device_id,purpose,kind,protection_mode,status,public_identifier,verifier,
				fingerprint_key_id,fingerprint,ciphertext,key_id,cipher_suite,nonce,public_certificate,
				created_at,created_by,updated_at,updated_by
			) VALUES($1,$2,$3,'device_access',$4,$5,'pending',NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9::bytea,$10::bytea,NULLIF($11,''),NULLIF($12,''),$13::bytea,NULLIF($14,''),$15,$16,$15,$16)`,
				aad.CredentialID, identity.tenantID, deviceID, input.Kind, mode,
				credentialString(publicIdentifier), "", fingerprintKeyID, fingerprint,
				protected.Ciphertext, protected.KeyID, protected.CipherSuite, protected.Nonce,
				publicCertificate, expiresAt.Add(-credentialSecretReplayWindow), identity.subjectID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			// The first usable credential of a device goes live through the runtime: it
			// is sent to Community as the device's voucher and becomes active once the
			// receipt confirms it. A device that already has a live credential keeps this
			// one waiting as the replacement for a rotation. Certificates cannot be sent.
			if input.Kind != "x509" {
				if err := enqueueFirstCredentialActivationTx(ctx, tx, identity.tenantID, identity.subjectID, deviceID, aad.CredentialID, requestID); err != nil {
					return metadataWriteResponse{}, metadataWriteAudit{}, err
				}
			}
			summary := NativeCredentialSummary{ID: aad.CredentialID, DeviceID: deviceID, Kind: input.Kind, Status: "pending", PublicIdentifier: publicIdentifier, Fingerprint: hex.EncodeToString(fingerprint), CreatedAt: expiresAt.Add(-credentialSecretReplayWindow).UTC()}
			secretResponse := nativeCredentialSecretResponse{Data: nativeCredentialSecret{Credential: summary, Secret: responseSecret, ExpiresAt: expiresAt.UTC()}, Meta: RequestMeta{RequestID: requestID}}
			bodyBytes, err := json.Marshal(secretResponse)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
			location := command.Path + "/" + aad.CredentialID
			response := metadataWriteResponse{Status: http.StatusCreated, Body: bodyBytes, Location: location, RequestID: requestID}
			redacted, _ := json.Marshal(map[string]any{"kind": input.Kind, "status": "pending", "protectionMode": mode})
			return response, metadataWriteAudit{ResourceKind: "device_credential", ResourceID: aad.CredentialID, RedactedChanges: redacted}, nil
		})
	if len(material) > 0 {
		for i := range material {
			material[i] = 0
		}
	}
	return result, err
}

func credentialString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func writeNativeCredentialInputProblem(w http.ResponseWriter, requestID, instance string, err error) {
	paths := map[string]struct{}{"kind": {}, "publicIdentifier": {}, "secret": {}, "certificatePem": {}, "deviceId": {}}
	writeNativeMetadataInputProblem(w, requestID, instance, "credential", paths, err)
}

func writeNativeCredentialMutationError(w http.ResponseWriter, r *http.Request, requestID string, err error) {
	var inputErr *NativeDeviceInputError
	if errors.As(err, &inputErr) {
		writeNativeCredentialInputProblem(w, requestID, r.URL.Path, err)
		return
	}
	var apiErr *errs.Error
	if !errors.As(err, &apiErr) {
		rlog.Error("device credential create failed", "request_id", requestID, "code", "internal")
		err = &errs.Error{Code: errs.Internal, Message: "device credential creation failed"}
	} else if apiErr.Code == errs.Internal || apiErr.Code == errs.Unavailable {
		rlog.Error("device credential create failed", "request_id", requestID, "code", apiErr.Code.String())
	}
	writeGroupProblem(w, requestID, r.URL.Path, err, 0, true)
}

func writeNativeCredentialMutationResponse(w http.ResponseWriter, response *metadataWriteResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", response.RequestID)
	if response.Location != "" {
		w.Header().Set("Location", response.Location)
	}
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/credentials
func (s *Service) CreateNativeDeviceCredential(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	w.Header().Set("Cache-Control", "no-store")
	identity, err := currentNativeCredentialWriteIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeNativeCredentialMutationError(w, r, requestID, invalid("deviceId", "must be a UUID"))
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := readNativeCredentialBody(w, r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	if _, err := decodeNativeCreateCredential(body); err != nil {
		writeNativeCredentialMutationError(w, r, requestID, err)
		return
	}
	protector, err := NewCredentialSecretProtectorFromEnv()
	if err != nil {
		writeNativeCredentialMutationError(w, r, requestID, credentialProtectionUnavailable())
		return
	}
	result, err := createNativeDeviceCredential(r.Context(), identity, deviceID, key, body, requestID, protector)
	if err != nil {
		writeNativeCredentialMutationError(w, r, requestID, err)
		return
	}
	writeNativeCredentialMutationResponse(w, result)
}

func readNativeCredentialBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, nativeCredentialWriteMaxBytes))
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "credential request body is too large or unreadable"}
	}
	return body, nil
}
