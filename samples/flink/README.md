# Flink 样例

一个最小 DataStream 作业：读 `device,temperature` 行 → 超过阈值 → 对每条发一个告警判定。

```java
lines.map(new ParseLine())
     .filter(new Above(threshold))
     .addSink(new VerdictSink(cfg));
```

没有窗口、没有状态后端、没有 checkpoint 配置。样例要说清的是**线上合同**，不是 Flink 怎么用。

## 跑之前

需要一个已登记的执行者，和一条**允许这个执行者走 event 通道**的告警策略。用冒烟脚本的辅助函数建一个最省事：

```bash
cd encore-api && TP_AUTH_MODE=dev encore run --port 14000

# 另一个终端，在仓库根目录
python3 - <<'PY'
import sys; sys.path.insert(0, "test/automation-rewrite")
import executor_smoke as s
s.DEVICE = "dev-a29-flink"
s.POLICY_KEY = "policy.a29.flink"
_, reg, _ = s.call("POST", "/v1/executors", {"name": "a29-flink", "contract": s.contract(s.DEVICE)})
_, pol, _ = s.call("POST", "/v1/rules", {"name": "a29 flink", "definition": s.alarm_definition(reg["id"]), "enabled": True})
print(f'--executor-id={reg["id"]} --contract-hash={reg["contract_hash"]} --epoch={reg["epoch"]} '
      f'--policy-id={pol["id"]} --policy-revision={pol["revision"]}')
PY
```

## 编译与运行

本机需要 JDK 17+（Flink 1.20 支持 17/21）。Homebrew 的 JDK 是 keg-only，所以要显式给路径：

```bash
cd samples/flink
export JAVA_HOME=/opt/homebrew/opt/openjdk@21

mvn -q compile
mvn -q dependency:build-classpath -Dmdep.outputFile=/tmp/a29-cp.txt

cat > /tmp/a29-samples.txt <<'EOF'
# device,temperature
dev-a29-flink,35.5
dev-a29-flink,20.0
dev-a29-flink,41.0
bad-line
EOF

java --add-opens java.base/java.util=ALL-UNNAMED \
     --add-opens java.base/java.lang=ALL-UNNAMED \
     --add-opens java.base/java.lang.reflect=ALL-UNNAMED \
     --add-opens java.base/java.text=ALL-UNNAMED \
     --add-opens java.base/java.time=ALL-UNNAMED \
     --add-opens java.base/java.math=ALL-UNNAMED \
  -cp "target/classes:$(cat /tmp/a29-cp.txt)" \
  com.thingspanel.automation.sample.VerdictJob \
  "--base=http://127.0.0.1:14000" \
  "--token=<令牌>" \
  "--executor-id=<id>" "--contract-hash=<hash>" "--epoch=1" \
  "--policy-id=<policy>" "--policy-revision=1" \
  "--threshold=30" "--input=/tmp/a29-samples.txt"
```

`--add-opens` 不是可选项：Java 17+ 的模块系统默认不允许 Flink 用到的 Kryo（chill）反射访问 `java.util`，缺了它会死在 `InaccessibleObjectException` 上——而报错栈落在 `ContinuousFileMonitoringFunction`，看起来像文件源的问题，其实是 JVM 参数的问题。集群部署时这些参数由 `flink-conf.yaml` 的 `env.java.opts.all` 提供，本地跑就得自己写。

期望输出（4 行输入里 2 行超阈值；`20.0` 不触发，`bad-line` 被丢掉）：

```
[verdict] seq=1793023617033 dev-a29-flink 35.5 -> qualified
[verdict] seq=1793023617034 dev-a29-flink 41.0 -> no_transition
```

第二行是重点：`41.0` 也是超阈值，但它**没有开第二条告警**。同一个 (策略, 主体, 维度) 上已经有一条 active，平台记的是"没有生命周期变化"（`no_transition`），而不是"又来了一条告警"。外部引擎不需要知道这件事，它只要报事实。

`--socket=host:port` 换掉 `--input` 就是流式来源（`nc -lk 9999` 灌数据即可）。

## 三个刻意的选择

**序号用时钟做基线。** `seq` 存在 sink 里，`open()` 时取 `System.currentTimeMillis()`。这样**跨重启也单调**——固定从 1 开始的话，第二次启动的第一条消息会被判成 `replayed_sequence`。生产里应该把计数器放进 Flink 状态（checkpoint），样例不引入状态是因为它会让代码翻倍，而它要说明的只是"序号必须单调"。

**并行度固定 1。** 因为 `seq` 是每个 sink 实例的计数器。要扩并行就按设备分区、每个分区一个计数器——超出样例范围，但代码里写明了，不是忘了。

**拒绝要打印出来。** `accepted: false` 的 body 里带 `issues[].field`，作业把它逐行打出来：

```
[verdict] seq=... dev-a29-flink 35.5 -> REFUSED stale_policy_revision
           policy_revision: 判定基于第 0 版策略，当前已发布第 1 版
           不要原样重试：按上面的字段改请求；序号没有前进时换新 key。
```

一个只判断 HTTP 200 的作业会把"被拒绝"当成"已告警"，而这正是这条通道要防的事。

## 这一版没做的

- 没有连真集群（YARN/K8s），只在 `local` 模式下跑过。
- 没有 checkpoint、没有 exactly-once。判定的去重靠平台的 `idempotency_key` + `seq`，不靠 Flink 的状态。
- 没有登记动作：样例假设执行者已经登记好（`epoch` 和 `contract_hash` 从命令行给）。一个作业自己去登记是可行的，但那样"谁在什么时候扩了权限"就没人看得见了。
