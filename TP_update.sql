alter table business
    add sort int not null;;
comment on column business.sort is '排序';

alter table device
    add sort int not null;;
comment on column device.sort is '排序';

alter table asset
    add sort int not null;;
comment on column asset.sort is '排序';
