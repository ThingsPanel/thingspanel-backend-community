package fileassets

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func writeAssetProblem(w http.ResponseWriter, r *http.Request, id string, err error, status int) {
	w.Header().Set("Cache-Control", "no-store")
	nativehttp.WriteProblem(w, id, r.URL.Path, err, status, false)
}

//encore:api auth raw method=POST path=/api/v2/file-assets
func (s *Service) CreateFileAsset(w http.ResponseWriter, r *http.Request) {
	id := uuid.NewString()
	principal, err := currentAssetPrincipal("write")
	if err != nil {
		writeAssetProblem(w, r, id, err, 0)
		return
	}
	if !validEmptyAssetQuery(r) {
		writeAssetProblem(w, r, id, errAssetRequest("query parameters are not supported"), 400)
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !validAssetIdempotencyKey(keys[0]) {
		writeAssetProblem(w, r, id, errAssetRequest("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters"), 400)
		return
	}
	contents, meta, err, status := readAssetMultipart(w, r)
	if err != nil {
		writeAssetProblem(w, r, id, err, status)
		return
	}
	result, err := s.createAsset(r.Context(), principal, keys[0], contents, meta, id)
	if err != nil {
		writeAssetProblem(w, r, id, err, 0)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-Id", result.requestID)
	w.Header().Set("Location", result.location)
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(result.body)
}

//encore:api auth raw method=GET path=/api/v2/file-assets/:assetID
func (s *Service) GetFileAsset(w http.ResponseWriter, r *http.Request) {
	id := uuid.NewString()
	principal, err := currentAssetPrincipal("read")
	if err != nil {
		writeAssetProblem(w, r, id, err, 0)
		return
	}
	if !validEmptyAssetQuery(r) {
		writeAssetProblem(w, r, id, errAssetRequest("query parameters are not supported"), 400)
		return
	}
	asset, err := s.getAsset(r.Context(), principal, encore.CurrentRequest().PathParams.Get("assetID"))
	if err != nil {
		writeAssetProblem(w, r, id, err, 0)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	nativehttp.WriteJSON(w, id, r.URL.Path, 200, assetETag(asset), FileAssetResponse{Data: *asset, Meta: nativehttp.RequestMeta{RequestID: id}})
}

type ValidateTemplateImageRequest struct {
	AssetID string `json:"assetId"`
}
type ValidateTemplateImageResponse struct {
	AssetID string `json:"assetId"`
}

// ValidateTemplateImage checks an existing reference without issuing a URL or
// writing a lease. The tenant comes from the original verified auth context.
//
//encore:api private
func (s *Service) ValidateTemplateImage(ctx context.Context, req *ValidateTemplateImageRequest) (*ValidateTemplateImageResponse, error) {
	principal, err := currentAssetPrincipal("read")
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errAssetRequest("asset ID is required")
	}
	tx, err := BeginTenantScopedTx(ctx, principal.tenantID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	asset, err := LoadTx(ctx, tx, principal.tenantID, req.AssetID)
	if err != nil {
		return nil, err
	}
	if asset.Purpose != "template-image" {
		return nil, &errs.Error{Code: errs.NotFound, Message: "template image asset not found"}
	}
	if err := tx.Commit(); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "file asset validation could not be completed"}
	}
	return &ValidateTemplateImageResponse{AssetID: asset.ID}, nil
}

func validEmptyAssetQuery(r *http.Request) bool { return r.URL.RawQuery == "" && !r.URL.ForceQuery }
func errAssetRequest(message string) error {
	return &errs.Error{Code: errs.InvalidArgument, Message: message}
}

func readAssetMultipart(w http.ResponseWriter, r *http.Request) ([]byte, ImageMetadata, error, int) {
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return nil, ImageMetadata{}, errAssetRequest("multipart/form-data is required"), 400
	}
	ct, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || ct != "multipart/form-data" || params["boundary"] == "" {
		return nil, ImageMetadata{}, errAssetRequest("multipart/form-data is required"), 400
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxTemplateImageBytes+64*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, ImageMetadata{}, errAssetRequest("invalid multipart body"), 400
	}
	var contents []byte
	var meta ImageMetadata
	seenFile, seenPurpose := false, false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ImageMetadata{}, errAssetRequest("invalid or oversized multipart body"), 400
		}
		// Exactly two fields; no additional hidden metadata or second file.
		if len(part.Header.Values("Content-Disposition")) != 1 || len(part.Header.Values("Content-Type")) > 1 {
			_ = part.Close()
			return nil, ImageMetadata{}, errAssetRequest("invalid multipart field headers"), 400
		}
		switch part.FormName() {
		case "purpose":
			if seenPurpose || part.FileName() != "" {
				_ = part.Close()
				return nil, ImageMetadata{}, errAssetRequest("purpose must occur exactly once"), 422
			}
			seenPurpose = true
			purpose, readErr := io.ReadAll(io.LimitReader(part, 64))
			if readErr != nil {
				_ = part.Close()
				return nil, ImageMetadata{}, errAssetRequest("invalid multipart body"), 400
			}
			if ValidateTemplateImagePurpose(string(purpose)) != nil {
				_ = part.Close()
				return nil, ImageMetadata{}, errAssetRequest("purpose must be template-image"), 422
			}
		case "file":
			if seenFile || part.FileName() == "" {
				_ = part.Close()
				return nil, ImageMetadata{}, errAssetRequest("file must occur exactly once"), 422
			}
			seenFile = true
			contents, meta, err = ReadTemplateImageUpload(part, part.Header.Get("Content-Type"))
			if err != nil {
				_ = part.Close()
				var validation *ValidationError
				if errors.As(err, &validation) {
					return nil, ImageMetadata{}, errAssetRequest(validation.Message), 422
				}
				return nil, ImageMetadata{}, errAssetRequest("invalid or oversized multipart body"), 400
			}
		default:
			_ = part.Close()
			return nil, ImageMetadata{}, errAssetRequest("unknown multipart field"), 422
		}
		if part.Close() != nil {
			return nil, ImageMetadata{}, errAssetRequest("invalid multipart body"), 400
		}
	}
	if !seenFile || !seenPurpose {
		return nil, ImageMetadata{}, errAssetRequest("file and purpose are required"), 422
	}
	// MIME parsing ignores epilogues. Count/drain remaining transport bytes so
	// the global cap also covers an oversized epilogue.
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return nil, ImageMetadata{}, errAssetRequest("invalid or oversized multipart body"), 400
	}
	return contents, meta, nil, 0
}
