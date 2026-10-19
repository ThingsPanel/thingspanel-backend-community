# 交付制品当前可用性核查

核查时间：2026-10-20 06:36:49 +08:00。仅做只读核查，打包源码基准为候选 `e11dcdc7d88b644df092ed88040a32a030a85836`；未构建、未启动服务、未补跑测试。

- `docker context show`：exit 0，输出 `orbstack`。
- `docker image inspect thingspanel-community-broker:ingestion-20261020`：exit 1，`No such image`。
- 按历史ID `sha256:ac1a075beb345ea8475d385d2b37d0af9a5bde5cbdcc66ae906f0f061b07fa8e` inspect：exit 1，`No such image`。
- Luna在 `/tmp/tp-mqtt-20261019` 与本证据目录未找到 `.tar/.tar.gz/.tgz/.oci/.docker` 等可直接load的镜像归档。这是指定目录的文件盘点，不是对全机器、其他daemon或远程registry的穷尽搜索；未推断镜像不可用的原因。
- [历史SBOM与provenance](sbom/provenance.json)是当时冻结镜像的静态记录，保留历史效力，但不包含镜像本体，也不能证明该镜像目前可取用。

候选 `Dockerfile.production` 构建并复制plugin、owner、readiness、audit工具以及Mosquitto。cutover/route属于独立管理工具，reconcile为离线判定器，tools-client由可信Host调用；这些不是Broker runtime所必需的同容器进程，不能据未打入Broker镜像认定功能缺失。AI API由ThingsPanel后端承载，真实聊天BFF尚未实现。

运行手册的镜像内容描述补上已存在的audit工具；Dockerfile和功能代码未改。待用户确定功能范围后，最终交付应从选定源码重新构建、记录镜像身份并做必要冒烟，不能把本次文档核查当成重新构建或运行成功。
