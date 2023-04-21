----配方表创建开始------
CREATE TABLE "public"."recipe" (
                                   "id" varchar(64) COLLATE "pg_catalog"."default" NOT NULL,
                                   "bottom_pot_id" varchar(32) COLLATE "pg_catalog"."default" NOT NULL,
                                   "bottom_pot" varchar(64) COLLATE "pg_catalog"."default" NOT NULL,
                                   "pot_type_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                   "pot_type_name" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                   "materials" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                   "materials_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                   "taste_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                   "taste" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                   "bottom_properties" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                   "soup_standard" int8 NOT NULL,
                                   "create_at" int8 NOT NULL,
                                   "update_at" timestamp(0) DEFAULT CURRENT_TIMESTAMP,
                                   "delete_at" timestamp(0),
                                   "is_del" bool DEFAULT false,
                                   "current_water_line" int8,
                                   "asset_id" varchar(20) COLLATE "pg_catalog"."default",
                                   CONSTRAINT "recipe_pkey" PRIMARY KEY ("id")
)
;

ALTER TABLE "public"."recipe"
    OWNER TO "postgres";

COMMENT ON COLUMN "public"."recipe"."bottom_pot_id" IS '锅底ID';

COMMENT ON COLUMN "public"."recipe"."bottom_pot" IS '锅底名称';

COMMENT ON COLUMN "public"."recipe"."pot_type_id" IS '锅型ID';

COMMENT ON COLUMN "public"."recipe"."pot_type_name" IS '锅型名称';

COMMENT ON COLUMN "public"."recipe"."materials" IS '物料';

COMMENT ON COLUMN "public"."recipe"."materials_id" IS '物料ID';

COMMENT ON COLUMN "public"."recipe"."taste_id" IS '口味ID';

COMMENT ON COLUMN "public"."recipe"."taste" IS '口味';

COMMENT ON COLUMN "public"."recipe"."bottom_properties" IS '锅底属性';

COMMENT ON COLUMN "public"."recipe"."soup_standard" IS '加汤标准';

COMMENT ON COLUMN "public"."recipe"."create_at" IS '创建时间';

COMMENT ON COLUMN "public"."recipe"."update_at" IS '更新时间';

COMMENT ON COLUMN "public"."recipe"."delete_at" IS '删除时间';

COMMENT ON COLUMN "public"."recipe"."is_del" IS '是否删除';

COMMENT ON COLUMN "public"."recipe"."current_water_line" IS '当前水位线';

COMMENT ON COLUMN "public"."recipe"."asset_id" IS '分组ID';
----配方表创建结束------

----口味表创建开始------
CREATE TABLE "public"."taste" (
                                  "id" varchar(50) COLLATE "pg_catalog"."default" NOT NULL,
                                  "name" varchar(50) COLLATE "pg_catalog"."default" NOT NULL,
                                  "taste_id" varchar(255) COLLATE "pg_catalog"."default" NOT NULL,
                                  "materials_name" varchar(50) COLLATE "pg_catalog"."default" NOT NULL,
                                  "dosage" int8 NOT NULL,
                                  "unit" varchar(50) COLLATE "pg_catalog"."default" NOT NULL,
                                  "create_at" int8 NOT NULL,
                                  "update_at" timestamp(0) DEFAULT CURRENT_TIMESTAMP,
                                  "delete_at" timestamp(0),
                                  "is_del" bool,
                                  "water_line" int8,
                                  "station" varchar(50) COLLATE "pg_catalog"."default",
                                  "recipe_id" varchar(100) COLLATE "pg_catalog"."default",
                                  "material" varchar(50) COLLATE "pg_catalog"."default",
                                  CONSTRAINT "taste_pkey" PRIMARY KEY ("id")
);

ALTER TABLE "public"."taste"
    OWNER TO "postgres";

COMMENT ON COLUMN "public"."taste"."name" IS '口味名称';

COMMENT ON COLUMN "public"."taste"."taste_id" IS '口味ID';

COMMENT ON COLUMN "public"."taste"."materials_name" IS '物料名称';

COMMENT ON COLUMN "public"."taste"."dosage" IS '用量';

COMMENT ON COLUMN "public"."taste"."unit" IS '单位';

COMMENT ON COLUMN "public"."taste"."create_at" IS '创建时间';

COMMENT ON COLUMN "public"."taste"."update_at" IS '更新时间';

COMMENT ON COLUMN "public"."taste"."delete_at" IS '删除时间';

COMMENT ON COLUMN "public"."taste"."is_del" IS '是否删除';

COMMENT ON COLUMN "public"."taste"."water_line" IS '加汤水位标准';

COMMENT ON COLUMN "public"."taste"."station" IS '工位';

COMMENT ON COLUMN "public"."taste"."recipe_id" IS '配方ID';

COMMENT ON COLUMN "public"."taste"."material" IS '物料名称';
----口味表创建结束------


----锅型表创建开始------
CREATE TABLE "public"."pot_type" (
                                     "id" varchar(64) COLLATE "pg_catalog"."default" NOT NULL,
                                     "name" varchar(255) COLLATE "pg_catalog"."default",
                                     "image" varchar(255) COLLATE "pg_catalog"."default",
                                     "create_at" int8,
                                     "update_at" timestamp(0) DEFAULT CURRENT_TIMESTAMP,
                                     "is_del" bool DEFAULT false,
                                     "soup_standard" int8,
                                     "pot_type_id" varchar(100) COLLATE "pg_catalog"."default",
                                     CONSTRAINT "pot_type_pkey" PRIMARY KEY ("id")
)
;

ALTER TABLE "public"."pot_type"
    OWNER TO "postgres";

COMMENT ON COLUMN "public"."pot_type"."id" IS '锅型ID';

COMMENT ON COLUMN "public"."pot_type"."name" IS '锅型名称';

COMMENT ON COLUMN "public"."pot_type"."image" IS '图片';

COMMENT ON COLUMN "public"."pot_type"."create_at" IS '创建时间';

COMMENT ON COLUMN "public"."pot_type"."is_del" IS '是否删除';

COMMENT ON COLUMN "public"."pot_type"."soup_standard" IS '加汤水位线标准';

COMMENT ON COLUMN "public"."pot_type"."pot_type_id" IS '锅型ID';
----锅型表创建结束------


----物料表创建开始------
CREATE TABLE "public"."materials" (
                                      "id" varchar(50) COLLATE "pg_catalog"."default" NOT NULL,
                                      "name" varchar(50) COLLATE "pg_catalog"."default",
                                      "dosage" int8,
                                      "unit" varchar(50) COLLATE "pg_catalog"."default",
                                      "water_line" int8,
                                      "station" varchar(50) COLLATE "pg_catalog"."default",
                                      "recipe_id" varchar(100) COLLATE "pg_catalog"."default",
                                      CONSTRAINT "materials_pkey" PRIMARY KEY ("id")
)
;

ALTER TABLE "public"."materials"
    OWNER TO "postgres";

COMMENT ON COLUMN "public"."materials"."name" IS '物料名称';

COMMENT ON COLUMN "public"."materials"."dosage" IS '用量';

COMMENT ON COLUMN "public"."materials"."unit" IS '单位';

COMMENT ON COLUMN "public"."materials"."water_line" IS '加汤水位标准';

COMMENT ON COLUMN "public"."materials"."station" IS '工位';

COMMENT ON COLUMN "public"."materials"."recipe_id" IS '配方ID';
----物料表创建结束------

---创建锅型SQL----
INSERT INTO "pot_type" ("id","name","image","create_at","is_del","soup_standard","pot_type_id") VALUES ('b28e0be0-dd9f-f5c0-4250-0aec70159e28','测试锅型','./files/logo/2023-04-20/47528482e5236f56022536b02baa7bf0.jpeg',1681996506,false,10000,'100000000')


---创建配方SQL----
    INSERT INTO "recipe" ("id","bottom_pot_id","bottom_pot","pot_type_id","pot_type_name","materials","materials_id","taste_id","taste","bottom_properties","soup_standard","create_at","delete_at","is_del","current_water_line","asset_id") VALUES ('3821de97-f7c8-2bd3-3f43-ab6ff6176d10','2023-4-20','测试锅底2023-4-20','100000000','','测试物料肉肉100100','abb062e5-91dc-774d-b1f9-62345bff750a','b9edc6c6-e81a-7e95-6f54-96d1655891a7','测试口味肉肉10g','辣',10000,1681996746,'0000-00-00 00:00:00',false,200,'1000')
    INSERT INTO "materials" ("id","name","dosage","unit","water_line","station","recipe_id") VALUES ('abb062e5-91dc-774d-b1f9-62345bff750a','测试物料肉肉',100,'100',0,'鲜料工位','3821de97-f7c8-2bd3-3f43-ab6ff6176d10')
    INSERT INTO "taste" ("id","name","taste_id","materials_name","dosage","unit","create_at","delete_at","is_del","water_line","station","recipe_id") VALUES ('b9edc6c6-e81a-7e95-6f54-96d1655891a7','','','',10,'g',1681996746,'0000-00-00 00:00:00',false,0,'','3821de97-f7c8-2bd3-3f43-ab6ff6176d10')
