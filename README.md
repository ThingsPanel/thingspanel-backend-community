ThingsPanel是一款**轻量级、组件化**的开源物联网应用支撑平台，旨在通过可复用的插件，减少开发工作，加速物联网项目构建。

ThingsPanel的插件主要有如下几类，分别是：
- **设备功能模板**：整合物模型与图表。
- **设备配置模板**：整合设备功能模板、协议插件。
- **协议接入插件**：解决各类协议接入的问题。
- **服务接入插件**：通过第三方平台接入设备。
- **看板卡片**：扩展看板展示能力
- **可视化插件**：扩展可视化大屏功能
- **依赖型插件**：行业解决方案积木

通过以上插件的**复用**，可以大大提高研发效率。
## 使用示例
1. [有人M300网关接温湿度传感器使用MQTT接入ThingsPanel-v1.0.0](https://www.thingspanel.cn/posts/80)
2. [Fox-Shifu 接入 ThingsPanel](https://bianwuji.feishu.cn/docx/LQS4dyVf4o5WMrxzPlKcP5Ftnpg)
3. [Fox-Edge灵狐物联网边缘计算平台对接ThingsPanel](http://docs.fox-tech.cn/#/fox-edge-3rd-cloud-thingspanel)
4. [ThingsPanel通过ESP8266遥控风扇转速](http://thingspanel.cn/posts/72)
5. [使用ESP-8266和BMP280传感器测量大气压-ThingsPanel](http://thingspanel.cn/posts/71)
## 产品截图
![系统架构](http://thingspanel.io/assets/images/ThingsPanel-0.6.0-homepage-27308c5423090237c9e13e5560b7162e.png)
## 演示
地址：http://demo.thingspanel.cn

账户：test@test.cn

密码：123456
## 快速安装使用
通过容器化部署是搭建ThingsPanel的最快方式。

1. 获取docker-compose源码
   
    ```bash
    git clone https://github.com/ThingsPanel/thingspanel-docker.git
    ```
1. 进入目录并启动服务

    ```bash
    cd thingspanel-docker
    docker-compose -f docker-compose.yml up
    ```
1. 登录
    ```text
    输入网址:服务器ip:8080
    输入账户:super@super.cn
    输入密码:123456
    ```
## 产品用途
- 设备统一管理
- 物联网中台
- 设备商设备管理后台
## 解决问题
- **爱好者**：开放的架构释放创造的乐趣。
- **集成商**：一套平台交付所有智能化项目。
- **方案商**：节省时间和成本快速实现业务目的。
- **设备商**：实现目的只需要做好设备，不再需要关注软件。
- **最终客户**：一套平台实现所有设备接入，实现物联网数据中台。
## 独特优势
- 易用性：简化了物联网，让物联网更容易理解。
- 兼容性：兼容各类设备协议，降低系统扩展成本。
- 组件化：开放架构，多种组件设计、快速搭建。
## 功能概要
- **多租户功能**： 超级管理员管理、租户账户管理业务系统、租户用户管理设备查看数据
- **设备接入**： 编辑创建项目、按照分组添加管理设备、查看设备推送状态、设备插件接入、网关与子设备接入、Modbus RTU/TCP协议接入、TCP协议接入、GB28181安防摄像头接入、自定- 义协议插件接入
- **监控看板**： 设备添加后的监控图表、可设置看板为菜单或首页，创建多个看板。
- **设备功能模板**：设置物模型、Web和App图表，可导出JSON。 
- **设备配置模板**：关联设备、属性与功能、协议配置、数据处理、自动化、告警、扩展信息、设备设置、一型一密设置
- **设备地图**： 根据项目与分组筛选设备、设备类型筛选
- **可视化**： 可视化编辑基本功能、开放式架构、预绑定数据图表、添加自己的图元、和系统松耦合，支持组态、大屏、3D、Three.js
- **产品管理**： 创建产品、批量管理、二维码数据、手动激活、预注册管理
- **固件升级**： 为产品添加固件、创建升级任务、固件升级报表
- **自动化**： 场景联动、场景日志、定时触发、设备触发、多种触发
- **告警信息**： 根据项目和分组显示告警、时间段筛选
- **通知功能**：短信、邮件、电话、webhook多种通知方式
- **系统日志**： IP访问路径、设备操作记录
- **应用管理**： 设备插件管理、插件生成器、插件安装、应用市场
- **协议接入**： 开发自定义协议配置、配置后的接入参数
- **服务接入**： 通过第三方平台接入设备
- **用户管理**： Casbin方案、页面权限控制、项目权限控制、多角色定义
- **规则引擎**： 数据转发第三方、接收设备数据并转换、接入各种协议、实时数据计算
- **数据网关**：OpenAPI，数据库SQL-to-HTTP，对接三方系统，限制IP与数据范围，授权读取
- **系统设置**： 更换Logo、更换系统标题、更换主题风格
- **物联网APP**： Uniapp开发、扫码添加设备、查看监测值、切换项目和设备分组、手动控制、设置控制策略、查看操作日志、个人账号管理、手机验证码登录
- **依赖型插件**： 依赖型插件为行业解决方案、基于设备插件和其他功能与数据、可视化调用、iframe代码引入、插件复用

## 技术栈
* Golang：天生优异的并发性能，节省硬件成本，可适用于边缘设备。
* Vue.js(3)：简单易上手
* Node.js(16.13)：免费、开源、跨平台。 
* 数据库
  * PostgreSQL：广泛的社区与低成本。
  * TimescaleDB ：时序数据库，PostgreSQL插件。
  * TDengine ：   国产高性能时序数据库
  * Cassandra：开源分布式Key-Value存储系统
  * TDSQL-PostgreSQL：腾讯自主研发的分布式数据库系统
  *  PloarDB-PostgreSQL：阿里云自主研发的高性能云原生分布式数据库
  *  KingBase：人大金仓
* Nginx：高性能Web服务器。
* MQTT broker
  * GMQTT：高性能消息队列处理。
  * VerneMQ：高性能分布式MQTT消息代理
* Redis：NoSQL缓存数据库
## 贡献指南
可直接克隆项目修改后提交PR。
## API文档链接
https://docs.qq.com/doc/DZVZKc2FCTE1EblBX
## 许可和商用授权

ThingsPanel是在[GNU Affero General Public License version 3 (AGPLv3)](https://www.gnu.org/licenses/agpl-3.0.html)下发布的，这意味着您可以下载、使用和修改代码，前提是任何修改后的版本也必须在同一许可证下发布，并且如果您通过网络运行修改后的程序，您必须向网络用户提供源代码。

对于希望在商业环境中使用ThingsPanel的用户，我们提供商用授权选项。商用授权允许您在不完全遵守AGPLv3开源许可要求的情况下使用ThingsPanel，例如在不向最终用户公开源代码的情况下使用ThingsPanel。这为企业提供了更大的灵活性和保密性。

如果您对商用授权感兴趣，或者需要更多信息，请通过以下联系方式与我们沟通：[商业授权查询](mailto:zjh@jiyikeji.cn)，或者加入微信群、QQ群与我们的商务联系。我们将提供详细的授权选项，以满足您的商业需求。

## 社区与支持
```
qq群①：260150504（已满）
qq群②：371794256
```
## 致谢
感谢您对ThingsPanel的贡献!
感谢[paddy235](https://gitee.com/paddy235)开发的ThingsPanel仿真测试脚本,可用于仿真和压力测试，脚本地址是：https://gitee.com/paddy235/thingspanel_simulation_python


<a href="https://github.com/ThingsPanel/ThingsPanel-Go/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=ThingsPanel/ThingsPanel-Go" />
</a>
