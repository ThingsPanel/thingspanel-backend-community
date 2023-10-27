package models

type SharedVisualization struct {
	DashboardID    string `json:"dashboard_id" gorm:"primaryKey,size:36"` // 可视化id
	ShareID    	string `json:"share_id" ` // 分享id
	DeviceList      string `json:"device_list"` // 设备id list
	CreatedAt      int64  `json:"created_at,omitempty"`
	
}

func (SharedVisualization) TableName() string {
	return "shared_visualization"
}
