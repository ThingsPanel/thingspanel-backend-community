package models

type TpDataTranspon struct {
	Id         string `json:"id,omitempty"`
	Desc       string `json:"desc,omitempty"`
	Status     int    `json:"status,omitempty"`
	TenantId   string `json:"tenant_id,omitempty"`
	Script     string `json:"script,omitempty"`
	CreateTime int    `json:"create_time,omitempty"`
}

func (TpDataTranspon) TableName() string {
	return "tp_data_transpond"
}

// create table tp_data_transpond
// (
// 	id varchar(36) not null
// 		constraint tp_data_transpond_pk
// 			primary key,
// 	"desc" varchar,
// 	status integer not null,
// 	tenant_id varchar(36) not null,
// 	script text,
// 	create_time integer
// );
