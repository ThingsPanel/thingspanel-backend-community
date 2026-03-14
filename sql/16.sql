-- ✅ 2026/03/14 Task-03: ThingsVis 仪表盘挂载为租户级首页菜单
CREATE TABLE IF NOT EXISTS public.tenant_dashboard_menus (
    id varchar(36) PRIMARY KEY,
    tenant_id varchar(36) NOT NULL,
    dashboard_id varchar(99) NOT NULL,
    dashboard_name varchar(99) NOT NULL,
    menu_name varchar(99) NOT NULL,
    parent_code varchar(50) NOT NULL DEFAULT 'home',
    sort int2 NOT NULL DEFAULT 1,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_dashboard_menu_unique
    ON public.tenant_dashboard_menus (tenant_id, dashboard_id);

COMMENT ON TABLE public.tenant_dashboard_menus IS '租户级 ThingsVis 仪表盘菜单绑定';
COMMENT ON COLUMN public.tenant_dashboard_menus.tenant_id IS '租户ID';
COMMENT ON COLUMN public.tenant_dashboard_menus.dashboard_id IS 'ThingsVis 仪表盘ID';
COMMENT ON COLUMN public.tenant_dashboard_menus.dashboard_name IS '仪表盘名称快照';
COMMENT ON COLUMN public.tenant_dashboard_menus.menu_name IS '菜单显示名称';
COMMENT ON COLUMN public.tenant_dashboard_menus.parent_code IS '父菜单编码，首版固定 home';
COMMENT ON COLUMN public.tenant_dashboard_menus.sort IS '排序';
COMMENT ON COLUMN public.tenant_dashboard_menus.enabled IS '是否启用';
