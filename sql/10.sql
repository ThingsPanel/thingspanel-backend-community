-- ✅2025/7/28 自动化动作表字段action_param_type扩展长度

ALTER TABLE public.action_info ALTER COLUMN action_param_type TYPE varchar(50) USING action_param_type::varchar(50);

-- ✅2025/8/4 添加open_api_keys表created_id字段
ALTER TABLE public.open_api_keys ADD created_id varchar(50) NULL;

-- ✅2025/8/10 用户表添加字段，新增用户地址表

-- 为user表添加新字段
ALTER TABLE users 
ADD COLUMN last_visit_ip VARCHAR(30),
ADD COLUMN last_visit_device VARCHAR(200),
ADD COLUMN organization VARCHAR(200),
ADD COLUMN timezone VARCHAR(50),
ADD COLUMN default_language VARCHAR(10),
ADD COLUMN password_fail_count INTEGER DEFAULT 0;

-- 添加字段注释
COMMENT ON COLUMN users.last_visit_ip IS '上次访问IP';
COMMENT ON COLUMN users.last_visit_device IS '上次访问设备信息摘要';
COMMENT ON COLUMN users.organization IS '用户所属组织机构名称';
COMMENT ON COLUMN users.timezone IS '所在时区';
COMMENT ON COLUMN users.default_language IS '默认语言';
COMMENT ON COLUMN users.password_fail_count IS '密码错误次数';

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

-- ✅2025/8/13 添加用户头像URL/路径
ALTER TABLE public.users 
ADD COLUMN avatar_url varchar(500) NULL;

COMMENT ON COLUMN public.users.avatar_url IS '用户头像URL或文件路径';