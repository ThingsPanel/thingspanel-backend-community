# 开发帮助

开发中尽量考虑代码的复用，以便于后期维护。

## 注意
如果生产环境在外网，就需要设置不能让用户自定义AccessToken，否则有设备数据泄露的风险

## vernemq相关
- vernemq需要所有MQTT协议直连设备的MQTTclientID唯一，生产使用vernemq需要对程序稍作调整

## 时序数据库相关

- timescaleDB数据库也是一个时序数据库，它在查询方面有很大的优势，但在高并发写入方面有很大的限制，还有水平扩展的问题。
- 根据配置文件中dbType的配置可以选择将设备数据写入其他数据库，目前共两张表的数据（ts_kv_latest、ts_kv）
- 通过grpc接口获取非timescaleDB数据库的数据

## 在线离线相关

- 目前在线离线状态是通过订阅device/status获取，将其存储在ts_kv_latest的SYS_ONLINE（str_v 1-在线 0-离线）,并将状态缓存到redis(key:"status"+diviceID)
- 当数据类型不是timescaledb的时候，SYS_ONLINE仍然存储在timescaledb的ts_kv_latest的SYS_ONLINE字段

- 有时候device/status会因为各种原因（大多时候是因为broker没将状态的改变获取到），可能会漏掉状态上报
- 设备在线检测修复方法**checkDeviceOnline()**，在设备上报数据的时候会去检查设备状态是否是在线，如果不是会修复为在线。

- 设备详情-运维信息页面，字段**离线时间阈值**不为0的时候，所有在线离线状态查询接口都不根据SYS_ONLINE字段来判断在线离线状态。而是根据**上次推送时间**来判断设备的在线离线（这个阈值在设备表的additional_info字段里的thresholdTime例如：{"runningInfo":{"thresholdTime":0}}）

- 如果设备本身不发送心跳，由于tcp本身的原理，我们无法判断一个设备是否与broker保持连接，所以有时候设备断电还显示在线（很大可能是因为broker没将状态的改变获取到）这个问题暂没解决办法（vernemq暂没出现这个问题，gmqtt有出现）
- 子设备默认在线离线阈值是60秒，在创建设备的时候设置

## 上次推送时间

- 设备列表中接口响应报文字段，取的是设备当前值中最近推送的一条时间。
- 当数据类型不是timescaledb的时候，需要通过grpc接口获取


