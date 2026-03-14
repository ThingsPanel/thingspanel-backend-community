package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type DashboardMenu struct{}

func (*DashboardMenu) Init(Router *gin.RouterGroup) {
	url := Router.Group("dashboard-menu")
	{
		url.GET(":dashboardId", api.Controllers.DashboardMenuApi.GetDashboardMenu)
		url.PUT(":dashboardId", api.Controllers.DashboardMenuApi.SaveDashboardMenu)
		url.DELETE(":dashboardId", api.Controllers.DashboardMenuApi.DeleteDashboardMenu)
	}
}
