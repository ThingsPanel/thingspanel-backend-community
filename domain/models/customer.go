package models

import "database/sql"

type Customer struct {
	ID               string         `json:"id" gorm:"primaryKey,size:36"`
	AdditionalInfo   sql.NullString `json:"additional_info" gorm:"type:longtext"`
	PrimaryAddress   sql.NullString `json:"address" gorm:"type:longtext"`
	SecondaryAddress sql.NullString `json:"address2" gorm:"type:longtext"`
	City             sql.NullString `json:"city"`
	Country          sql.NullString `json:"country"`
	Email            sql.NullString `json:"email"`
	Phone            sql.NullString `json:"phone"`
	SearchText       sql.NullString `json:"search_text"`
	State            sql.NullString `json:"state"`
	Title            sql.NullString `json:"title"`
	Zip              sql.NullString `json:"zip"`
}
