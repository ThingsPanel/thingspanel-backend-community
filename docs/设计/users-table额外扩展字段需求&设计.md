# 01-额外扩展字段需求&设计

## 需求
【腾讯文档】个人信息地址需求待确认问题
https://docs.qq.com/doc/DY0xndFVmQkx1bUZ3

### 原user表结构

```sql
-- public.users definition

-- Drop table

-- DROP TABLE public.users;

CREATE TABLE public.users (
	id varchar(36) NOT NULL,
	"name" varchar(255) NULL,
	phone_number varchar(50) NOT NULL,
	email varchar(255) NOT NULL,
	status varchar(2) NULL, -- 用户状态 F-冻结 N-正常
	authority varchar(50) NULL, -- 权限类型 TENANT_ADMIN-租户管理员 TENANT_USER-租户用户 SYS_ADMIN-系统管理员
	"password" varchar(255) NOT NULL,
	tenant_id varchar(36) NULL,
	remark varchar(255) NULL,
	additional_info json DEFAULT '{}'::json NULL,
	created_at timestamptz(6) NULL,
	updated_at timestamptz(6) NULL,
	password_last_updated timestamptz(6) NULL,
	last_visit_time timestamptz NULL, -- 上次访问时间
	CONSTRAINT users_pkey PRIMARY KEY (id),
	CONSTRAINT users_un UNIQUE (email)
);
COMMENT ON TABLE public.users IS '用户';

-- Column comments

COMMENT ON COLUMN public.users.status IS '用户状态 F-冻结 N-正常';
COMMENT ON COLUMN public.users.authority IS '权限类型 TENANT_ADMIN-租户管理员 TENANT_USER-租户用户 SYS_ADMIN-系统管理员';
COMMENT ON COLUMN public.users.last_visit_time IS '上次访问时间';
```

### user表增加字段

|字段名|描述|类型|说明示例|
|:-:|:-:|:-:|:-:|
|last_visit_ip|上次访问IP|string(30)|192.168.1.15|
|last_visit_device|上次访问设备信息摘要|string(200)|Windows11&Chrome128|
|organization|用户所属组织机构名称|string(200)|上海智联科技有限公司|
|timezone|所在时区|string(50)|Asia/Shanghai|
|default_language|默认语言|string(10)|zh-CN|
|password_fail_count|密码错误次数|int|3|

#### pg表结构

```sql
-- 为user表添加新字段
ALTER TABLE users 
ADD COLUMN last_visit_ip VARCHAR(30),
ADD COLUMN last_visit_device VARCHAR(200),
ADD COLUMN organization VARCHAR(200),
ADD COLUMN timezone VARCHAR(50),
ADD COLUMN default_language VARCHAR(10),
ADD COLUMN password_fail_count INTEGER DEFAULT 0,
ADD COLUMN last_login_time TIMESTAMPTZ;

-- 添加字段注释
COMMENT ON COLUMN users.last_visit_ip IS '上次访问IP';
COMMENT ON COLUMN users.last_visit_device IS '上次访问设备信息摘要';
COMMENT ON COLUMN users.organization IS '用户所属组织机构名称';
COMMENT ON COLUMN users.timezone IS '所在时区';
COMMENT ON COLUMN users.default_language IS '默认语言';
COMMENT ON COLUMN users.password_fail_count IS '密码错误次数';
```

### 额外增加地址表user_address表（用户地址表 - 1对1）


|字段名|描述|类型|说明示例|
|:-:|:-:|:-:|:-:|
|id|地址ID|int|主键，自增|
|user_id|用户ID|string(36)|外键关联用户表，唯一索引|
|country|国家|string(50)|中国|
|province|省份|string(50)|上海市|
|city|城市|string(50)|上海市|
|district|区县|string(50)|浦东新区|
|street|街道/乡镇|string(100)|陆家嘴街道|
|detailed_address|详细地址|string(200)|世纪大道100号东方明珠大厦8楼|
|postal_code|邮政编码|string(10)|200120|
|address_label|地址标签|string(50)|家/公司/学校|
|longitude|经度|string(20)|121.5057300|
|latitude|纬度|string(20)|31.2459800|
|additional_info|附加信息|string(500)|门禁密码：1234，联系楼管张师傅|
|created_time|创建时间|timestamptz|2025-08-08 14:29:48+08:00|
|updated_time|更新时间|timestamptz|2025-08-08 14:29:48+08:00|

#### pg表结构

```sql
-- 创建用户地址表
CREATE TABLE user_address (
   id SERIAL PRIMARY KEY,
   user_id VARCHAR(36) NOT NULL,
   country VARCHAR(50),
   province VARCHAR(50),
   city VARCHAR(50),
   district VARCHAR(50),
   street VARCHAR(100),
   detailed_address VARCHAR(200),
   postal_code VARCHAR(10),
   address_label VARCHAR(50),
   longitude VARCHAR(20),
   latitude VARCHAR(20),
   additional_info VARCHAR(500),
   created_time TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
   updated_time TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- 创建唯一索引确保1对1关系
CREATE UNIQUE INDEX uk_user_address_user_id ON user_address(user_id);

-- 创建外键约束（假设主表名为users）
ALTER TABLE user_address 
ADD CONSTRAINT fk_user_address_user_id 
FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- 添加表注释
COMMENT ON TABLE user_address IS '用户地址表（1对1关系）';
COMMENT ON COLUMN user_address.id IS '地址ID，主键自增';
COMMENT ON COLUMN user_address.user_id IS '用户ID，外键关联用户表';
COMMENT ON COLUMN user_address.country IS '国家';
COMMENT ON COLUMN user_address.province IS '省份';
COMMENT ON COLUMN user_address.city IS '城市';
COMMENT ON COLUMN user_address.district IS '区县';
COMMENT ON COLUMN user_address.street IS '街道/乡镇';
COMMENT ON COLUMN user_address.detailed_address IS '详细地址';
COMMENT ON COLUMN user_address.postal_code IS '邮政编码';
COMMENT ON COLUMN user_address.address_label IS '地址标签';
COMMENT ON COLUMN user_address.longitude IS '经度';
COMMENT ON COLUMN user_address.latitude IS '纬度';
COMMENT ON COLUMN user_address.additional_info IS '附加信息';
COMMENT ON COLUMN user_address.created_time IS '创建时间';
COMMENT ON COLUMN user_address.updated_time IS '更新时间';
```

## 相关接口变更

- /api/v1/user POST 
- /api/v1/user PUT
- /api/v1/login POST
- /api/v1/user GET
- /api/v1/user/{id} GET
- /api/v1/user/{id} DELETE
- 新增/api/v1/user/address/{id} PUT
- /api/v1/user/detail GET
- /api/v1/board/user/info GET