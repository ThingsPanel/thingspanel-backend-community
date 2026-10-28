package devicemetadata

import (
	"context"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

type CustomCommandControl struct {
	ID           string    `json:"id"`
	ThingModelID string    `json:"thing_model_id"`
	TenantID     string    `json:"tenant_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Content      string    `json:"content"`
	EnableStatus string    `json:"enable_status"`
	ControlType  string    `json:"control_type,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CustomCommandControlListReq struct {
	pageRequest
	ThingModelID string `query:"thing_model_id"`
}

//encore:api auth method=GET path=/api/custom-command-controls
func (s *Service) ListCustomCommandControls(ctx context.Context, req *CustomCommandControlListReq) (*listResponse[CustomCommandControl], error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	req.normalize()

	if req.ThingModelID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "thing_model_id is required"}
	}

	var total int
	if err = db.QueryRow(ctx,
		`SELECT COUNT(*) FROM custom_command_controls WHERE thing_model_id = $1 AND tenant_id = $2`,
		req.ThingModelID, tenantID,
	).Scan(&total); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "count custom controls")
	}

	rows, err := db.Query(ctx,
		`SELECT id, thing_model_id, tenant_id, name, COALESCE(description,''), content, enable_status, COALESCE(control_type,''), created_at, updated_at
		 FROM custom_command_controls
		 WHERE thing_model_id = $1 AND tenant_id = $2
		 ORDER BY created_at ASC
		 LIMIT $3 OFFSET $4`,
		req.ThingModelID, tenantID, req.PageSize, (req.Page-1)*req.PageSize,
	)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "list custom controls")
	}
	defer rows.Close()

	items := make([]CustomCommandControl, 0)
	for rows.Next() {
		var c CustomCommandControl
		if err = rows.Scan(&c.ID, &c.ThingModelID, &c.TenantID, &c.Name, &c.Description, &c.Content, &c.EnableStatus, &c.ControlType, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, errs.WrapCode(err, errs.Internal, "scan custom control")
		}
		items = append(items, c)
	}

	return &listResponse[CustomCommandControl]{Items: items, Page: req.Page, PageSize: req.PageSize, Total: total}, nil
}

//encore:api auth method=POST path=/api/custom-command-controls
func (s *Service) CreateCustomCommandControl(ctx context.Context, req *CustomCommandControl) (*CustomCommandControl, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}

	if req.ThingModelID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "thing_model_id is required"}
	}
	if req.Name == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "name is required"}
	}
	if req.Content == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "content is required"}
	}
	if req.EnableStatus == "" {
		req.EnableStatus = "enable"
	}

	// Verify thing_model belongs to this tenant before inserting.
	if _, err = getThingModelByID(ctx, tenantID, req.ThingModelID); err != nil {
		return nil, &errs.Error{Code: errs.NotFound, Message: "thing model not found"}
	}

	req.ID = uuid.New().String()
	req.TenantID = tenantID

	if _, err = db.Exec(ctx,
		`INSERT INTO custom_command_controls (id, thing_model_id, tenant_id, name, description, content, enable_status, control_type)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		req.ID, req.ThingModelID, tenantID, req.Name, req.Description, req.Content, req.EnableStatus, req.ControlType,
	); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "insert custom control")
	}

	return getCustomCommandControl(ctx, tenantID, req.ID)
}

//encore:api auth method=PUT path=/api/custom-command-controls
func (s *Service) UpdateCustomCommandControl(ctx context.Context, req *CustomCommandControl) (*CustomCommandControl, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}

	if req.ID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "id is required"}
	}

	res, err := db.Exec(ctx,
		`UPDATE custom_command_controls
		 SET name=$1, description=$2, content=$3, enable_status=$4, control_type=$5, updated_at=NOW()
		 WHERE id=$6 AND tenant_id=$7`,
		req.Name, req.Description, req.Content, req.EnableStatus, req.ControlType, req.ID, tenantID,
	)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "update custom control")
	}
	if n := res.RowsAffected(); n == 0 {
		return nil, &errs.Error{Code: errs.NotFound, Message: "custom control not found"}
	}

	return getCustomCommandControl(ctx, tenantID, req.ID)
}

//encore:api auth method=DELETE path=/api/custom-command-controls/:id
func (s *Service) DeleteCustomCommandControl(ctx context.Context, id string) error {
	tenantID, err := mustTenantID()
	if err != nil {
		return err
	}

	res, err := db.Exec(ctx,
		`DELETE FROM custom_command_controls WHERE id=$1 AND tenant_id=$2`,
		id, tenantID,
	)
	if err != nil {
		return errs.WrapCode(err, errs.Internal, "delete custom control")
	}
	if n := res.RowsAffected(); n == 0 {
		return &errs.Error{Code: errs.NotFound, Message: "custom control not found"}
	}
	return nil
}

func getCustomCommandControl(ctx context.Context, tenantID, id string) (*CustomCommandControl, error) {
	var c CustomCommandControl
	err := db.QueryRow(ctx,
		`SELECT id, thing_model_id, tenant_id, name, COALESCE(description,''), content, enable_status, COALESCE(control_type,''), created_at, updated_at
		 FROM custom_command_controls WHERE id=$1 AND tenant_id=$2`,
		id, tenantID,
	).Scan(&c.ID, &c.ThingModelID, &c.TenantID, &c.Name, &c.Description, &c.Content, &c.EnableStatus, &c.ControlType, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, errs.WrapCode(err, errs.NotFound, "custom control not found")
	}
	return &c, nil
}
