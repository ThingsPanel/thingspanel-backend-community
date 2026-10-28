package automationcompat

import "github.com/gin-gonic/gin"

// Routes registers the legacy paths on a group that is already rooted where the
// old endpoints live.
//
// The table is the same in both mounts. Shadow mode roots it at /api/v1/compat
// so real traffic keeps hitting the old engine; takeover mode roots it at
// /api/v1. Nothing else about the adapter changes, which is the point: the code
// that was rehearsed is the code that takes over.
//
// Takeover is not wired into router_init yet. The old engine is still running
// (A33 has not happened), so registering these paths next to the old handlers
// would either panic on a duplicate route or double-execute every activation.
// A33 drops the old registrations in the same commit that mounts this one — see
// docs/A30-consumer-ledger-2026-10-26.md, "接管步骤".
func (h *Handlers) Routes(g *gin.RouterGroup) {
	// Scenes: the old "manual" object — actions only, executed on demand.
	g.GET("/scene", h.SceneList)
	g.GET("/scene/detail/:id", h.SceneDetail)
	g.POST("/scene/active/:id", h.SceneActive)

	// Scene automations: the old "trigger → actions" object.
	g.GET("/scene_automations/list", h.AutomationList)
	g.GET("/scene_automations/detail/:id", h.AutomationDetail)
	g.POST("/scene_automations/switch/:id", h.AutomationSwitch)
	g.GET("/scene_automations/log", h.AutomationLog)

	// Alarms: the App's card, its history tab and the detail link carried by a
	// push notification.
	g.GET("/alarm/info", h.AlarmInfo)
	g.GET("/alarm/info/history", h.AlarmHistory)
	g.GET("/alarm/info/history/:id", h.AlarmDetail)
}

// OpsRoutes registers the operational endpoints. They always live under
// /compat, in both mounts, so a probe can never shadow a real endpoint.
func (h *Handlers) OpsRoutes(v1 *gin.RouterGroup) {
	g := v1.Group("/compat")
	g.GET("/stats", h.Stats)
	g.GET("/metrics", h.Metrics)
}

// MountShadow attaches the adapter under /api/v1/compat for rehearsal.
//
// This is what runs today: old clients keep talking to the old engine, and the
// adapter answers the same requests on a parallel path, so the smoke script can
// replay real payloads without a single device being switched twice.
func MountShadow(v1 *gin.RouterGroup, h *Handlers) {
	h.Routes(v1.Group("/compat"))
	h.OpsRoutes(v1)
}

// Mount registers the adapter at the real legacy paths. A33 calls this.
func Mount(v1 *gin.RouterGroup, h *Handlers) {
	h.Routes(v1)
	h.OpsRoutes(v1)
}
