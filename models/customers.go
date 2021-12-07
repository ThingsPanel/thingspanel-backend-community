package models

type Customer struct {
	ID             string `json:"id" gorm:"primaryKey,size:36"`
	AdditionalInfo string `json:"additional_info" gorm:"type:longtext"`
	Address        string `json:"address" gorm:"type:longtext"`
	Address2       string `json:"address2" gorm:"type:longtext"`
	City           string `json:"city"`
	Country        string `json:"country"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
	SearchText     string `json:"search_text"`
	State          string `json:"state"`
	Title          string `json:"title"`
	Zip            string `json:"zip"`
}

func (Customer) TableName() string {
	return "customers"
}
