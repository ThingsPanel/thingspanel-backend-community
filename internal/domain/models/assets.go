package models

import "database/sql"

type Asset struct {
	ID             string         `json:"id" gorm:"primaryKey"`
	AdditionalInfo sql.NullString `json:"additional_info"`
	CustomerID     sql.NullString `json:"customer_id"`
	Name           sql.NullString `json:"name"`
	Labal          sql.NullString `json:"labal"`
	SearchText     sql.NullString `json:"search_text"`
	Type           sql.NullString `json:"type"`
	ParentID       sql.NullString `json:"parent_id"`
	Tier           int64          `json:"tier"`
	BusinessID     sql.NullString `json:"business_id"`
}
type Device struct {
	ID             string `json:"id "gorm:"primaryKey"`
	AssetID        string `json:"asset_id"`
	Token          string `json:"token"`
	AdditionalInfo string `json:"additional_info"`
}
