package devicemetadata

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// ThingModelBinding is the fixed, published ThingModel reference resolved from
// an access-template selection. The semantic and presentation-resource
// digests remain separate and are sourced from the immutable snapshot owner.
type ThingModelBinding struct {
	ModelID           string `json:"modelId"`
	SnapshotID        string `json:"snapshotId"`
	PublishedRevision uint64 `json:"publishedRevision"`
	SnapshotDigest    string `json:"snapshotDigest"`
	ResourcesDigest   string `json:"resourcesDigest"`
}

var accessTemplateDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// resolveAccessTemplateModelBindingTx resolves a validated selection inside
// the caller's trusted tenant-local transaction. It does not authorize the
// caller; template/model read authorization remains with the service owner.
// A null selection is a valid unmodeled template and returns nil.
func resolveAccessTemplateModelBindingTx(ctx context.Context, tx *sqldb.Tx, tenantID string, selection json.RawMessage) (*ThingModelBinding, error) {
	if tx == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "template model resolution requires an existing transaction"}
	}
	tenantUUID, err := uuid.Parse(tenantID)
	if err != nil || tenantUUID == uuid.Nil || tenantUUID.String() != tenantID {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	selection = bytes.TrimSpace(selection)
	if bytes.Equal(selection, []byte("null")) {
		return nil, nil
	}
	var selected struct {
		ModelID           string      `json:"modelId"`
		PublishedRevision json.Number `json:"publishedRevision"`
	}
	decoder := json.NewDecoder(bytes.NewReader(selection))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&selected); err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid ThingModel selection"}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid ThingModel selection"}
	}
	var rawSelection map[string]json.RawMessage
	_ = json.Unmarshal(selection, &rawSelection)
	revisionToken := bytes.TrimSpace(rawSelection["publishedRevision"])
	if len(revisionToken) == 0 || revisionToken[0] == '"' {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid ThingModel selection"}
	}
	modelUUID, err := uuid.Parse(selected.ModelID)
	if err != nil || modelUUID == uuid.Nil || modelUUID.String() != selected.ModelID {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid ThingModel selection"}
	}
	revision, err := strconv.ParseInt(string(selected.PublishedRevision), 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid ThingModel selection"}
	}

	var lifecycle string
	var canonical bool
	err = tx.QueryRow(ctx, `
		SELECT status, canonical_model IS NOT NULL
		FROM thing_models
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
		FOR SHARE`, tenantID, modelUUID.String()).Scan(&lifecycle, &canonical)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "ThingModel not found"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to read ThingModel"}
	}
	if !canonical || lifecycle == "ARCHIVED" {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "ThingModel is not available for template binding"}
	}

	var out ThingModelBinding
	var canonicalSnapshot bool
	var canonicalContent []byte
	var snapshotChecksum, resourcesChecksum sql.NullString
	var resourcesContent []byte
	err = tx.QueryRow(ctx, `
		SELECT id, version, canonical_snapshot, canonical_content, canonical_checksum,
		       resources_content, resources_checksum
		FROM thing_model_snapshots
		WHERE tenant_id=$1 AND thing_model_id=$2 AND version=$3`,
		tenantID, modelUUID.String(), revision,
	).Scan(&out.SnapshotID, &out.PublishedRevision, &canonicalSnapshot, &canonicalContent, &snapshotChecksum, &resourcesContent, &resourcesChecksum)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "ThingModel version not found"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to read ThingModel version"}
	}
	if !canonicalSnapshot || len(canonicalContent) == 0 || !snapshotChecksum.Valid || len(resourcesContent) == 0 || !resourcesChecksum.Valid {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "ThingModel version is not a complete canonical snapshot"}
	}
	out.SnapshotDigest = strings.TrimSpace(snapshotChecksum.String)
	out.ResourcesDigest = strings.TrimSpace(resourcesChecksum.String)
	if !accessTemplateDigestPattern.MatchString(out.SnapshotDigest) || !accessTemplateDigestPattern.MatchString(out.ResourcesDigest) {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "ThingModel version is missing canonical digests"}
	}
	out.ModelID = modelUUID.String()
	if out.PublishedRevision != uint64(revision) {
		return nil, &errs.Error{Code: errs.Internal, Message: fmt.Sprintf("ThingModel version mismatch for model %s", modelUUID)}
	}
	return &out, nil
}
