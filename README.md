# Private Release

面向研究机构共享统计结果的差分隐私 HTTP 后端。项目由管理员一次性创建，公开接口只返回带 Laplace 噪声的数量、总量或固定分箱直方图，不返回用户 ID、原始值或精确聚合。

## 隐私语义

- 邻接数据集定义为增加或删除一个用户，每个用户只能贡献一个测量值。
- 数量使用 Google Differential Privacy `dpagg.Count`，敏感度为 1。
- 总量使用 `dpagg.BoundedSumInt64`，值先裁剪到公开上下界，敏感度为 `max(abs(lower), abs(upper))`。
- 直方图使用公开、固定的连续分箱；边界归属为 `[lower, upper)`，最后一箱为 `[lower, upper]`。每个用户只落入一箱，整组向量的 L1 敏感度为 1。
- 三种机制均使用官方库默认纯 epsilon Laplace 噪声，delta 为 0，不接受随机种子。
- 空箱也会稳定返回。噪声结果可能为负数，这是 Laplace 机制的正常输出。

## 预算与复用

- 每个项目独立累计 epsilon。
- epsilon 使用十进制精确会计；相同 `operation` 和数值相等的 epsilon 是同一查询。
- 已发布查询永久复用 SQLite 中保存的带噪结果，不重新采样、不再次扣费。
- 不同查询的 epsilon 相加；余额不足时返回 `402 Payment Required`，且不扣减预算。
- 预算检查、结果保存和扣减在同一个 SQLite 事务中提交；服务内还按项目串行化并发查询。

## 启动

```bash
ADMIN_KEY='choose-a-long-secret' \
ADDR=':8080' \
DB_PATH='private-release.db' \
go run ./cmd/private-release
```

`ADMIN_KEY` 是必填的启动配置。只有创建项目需要请求头 `X-Admin-Key`。

## 创建项目

```bash
curl -sS -X POST 'http://127.0.0.1:8080/v1/projects/' \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Key: choose-a-long-secret' \
  -d '{
    "id": "lab-1",
    "lower": 0,
    "upper": 10,
    "edges": [0, 5, 10],
    "total_epsilon": "1.5",
    "records": [
      {"user_id": "u1", "value": 4},
      {"user_id": "u2", "value": 5},
      {"user_id": "u3", "value": 99}
    ]
  }'
```

普通区间的 `edges` 必须严格递增，首项等于 `lower`，末项等于 `upper`；`lower == upper` 的单点区间使用单元素 `edges: [lower]`。重复 `user_id`、非法上下界、非法 epsilon 都会被拒绝。`99` 会裁剪为 `10`。

## 查询项目元数据

```bash
curl -sS 'http://127.0.0.1:8080/v1/projects/lab-1'
```

响应只包含公开项目参数和预算，不包含用户 ID 或记录值。

## 发布统计

```bash
curl -sS -X POST 'http://127.0.0.1:8080/v1/projects/lab-1/queries' \
  -H 'Content-Type: application/json' \
  -d '{"operation": "count", "epsilon": "0.5"}'

curl -sS -X POST 'http://127.0.0.1:8080/v1/projects/lab-1/queries' \
  -H 'Content-Type: application/json' \
  -d '{"operation": "sum", "epsilon": "0.5"}'

curl -sS -X POST 'http://127.0.0.1:8080/v1/projects/lab-1/queries' \
  -H 'Content-Type: application/json' \
  -d '{"operation": "histogram", "epsilon": "0.5"}'
```

`operation` 只能是 `count`、`sum` 或 `histogram`；`epsilon` 必须是正数的十进制字符串。响应包含：

- `epsilon`：本次请求的 epsilon。
- `spent_epsilon`：该项目累计消耗。
- `remaining_epsilon`：剩余预算。
- `result`：带噪数量、带噪总量或所有公开分箱的带噪计数。

重复发送完全相同的 `operation` 和 `epsilon` 会返回首次保存的结果，余额保持不变。

## 开发与测试

运行 `go test ./...`、`go test -race ./...` 和 `go build ./cmd/private-release`。

源码按职责分层：`internal/domain` 负责输入校验、上下界、分箱和精确十进制；`internal/privacy` 负责官方差分隐私机制；`internal/store` 负责 SQLite 事务和持久化；`internal/service` 负责用例编排和项目级并发串行化；`internal/httpapi` 负责 HTTP；`cmd/private-release` 是启动入口。
