---创建锅型SQL----
INSERT INTO "pot_type" ("id","name","image","create_at","is_del","soup_standard","pot_type_id") VALUES ('b28e0be0-dd9f-f5c0-4250-0aec70159e28','测试锅型','./files/logo/2023-04-20/47528482e5236f56022536b02baa7bf0.jpeg',1681996506,false,10000,'100000000')


---创建配方SQL----
    INSERT INTO "recipe" ("id","bottom_pot_id","bottom_pot","pot_type_id","pot_type_name","materials","materials_id","taste_id","taste","bottom_properties","soup_standard","create_at","delete_at","is_del","current_water_line","asset_id") VALUES ('3821de97-f7c8-2bd3-3f43-ab6ff6176d10','2023-4-20','测试锅底2023-4-20','100000000','','测试物料肉肉100100','abb062e5-91dc-774d-b1f9-62345bff750a','b9edc6c6-e81a-7e95-6f54-96d1655891a7','测试口味肉肉10g','辣',10000,1681996746,'0000-00-00 00:00:00',false,200,'1000')
    INSERT INTO "materials" ("id","name","dosage","unit","water_line","station","recipe_id") VALUES ('abb062e5-91dc-774d-b1f9-62345bff750a','测试物料肉肉',100,'100',0,'鲜料工位','3821de97-f7c8-2bd3-3f43-ab6ff6176d10')
    INSERT INTO "taste" ("id","name","taste_id","materials_name","dosage","unit","create_at","delete_at","is_del","water_line","station","recipe_id") VALUES ('b9edc6c6-e81a-7e95-6f54-96d1655891a7','','','',10,'g',1681996746,'0000-00-00 00:00:00',false,0,'','3821de97-f7c8-2bd3-3f43-ab6ff6176d10')
