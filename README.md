# private-release

面向研究机构共享统计的差分隐私发布 HTTP 后端。基于 Google Differential Privacy
Go 库（v3.0.0）的纯 ε 安全 Laplace 噪声，SQLite 持久化隐私预算与发布结果。

## 隐私模型

- 邻接定义：增删一个用户（add/remove-one-user）。
- 每用户贡献一个整数测量值；重复用户 ID 拒绝。值裁剪到公开上下界 `[lower, upper]`。
- 数量（count）：L1 敏感度 1；总量（sum）：敏感度 `max(|lower|, |upper|)`；
  直方图（histogram）：整体 L1 敏感度 1，每个分箱独立加噪，所有公开分箱（含空箱）都返回。
- 公开接口只返回带噪结果，绝不返回用户 ID、原始值或精确聚合。
- 客户端只能指定正的 `epsilon`，不能指定随机种子。

## 预算会计

- 每个项目独立累计预算，采用精确十进制（有理数）会计，无浮点误差。
- 相同操作 + 数值相等的 epsilon（如 `0.5`、`0.50`、`5e-1`）视为同一查询：
  持久复用带噪结果，不重新采样、不扣费。
- 不同查询按 epsilon 相加消耗预算；预算检查、结果保存、扣减在单个 SQLite
  事务中原子提交，成功后才返回。并发相同查询只发布一次；失败不扣费；重启保留结果与余额。
- 超预算返回 `403` 及明确错误信息。

## 构建与启动

```sh
go build -o bin/server ./cmd/server
ADMIN_KEY=topsecret ./bin/server -addr 127.0.0.1:8080 -db release.db
```

管理员工密钥通过 `ADMIN_KEY` 环境变量或 `-admin-key` 提供，管理接口需请求头
`X-Admin-Key`。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/projects/` | （管理员）创建不可变项目 |
| POST | `/projects/{id}/contributions` | （管理员）提交 `user_id` + `value` |
| GET | `/projects/{id}/count?epsilon=0.5` | 公开：DP 数量 |
| GET | `/projects/{id}/sum?epsilon=0.25` | 公开：DP 总量 |
| GET | `/projects/{id}/histogram?epsilon=1` | 公开：DP 直方图 |

创建项目示例：

```sh
curl -H "X-Admin-Key: topsecret" -X POST localhost:8080/projects/ \
  -d '{"id":"survey","lower":0,"upper":100,"bins":4,"epsilon_budget":"2"}'
```

发布响应示例：

```json
{
  "count": 5,
  "operation": "count",
  "privacy": {
    "epsilon": "0.500000000000",
    "epsilon_spent": "1.750000000000",
    "epsilon_remaining": "0.250000000000",
    "reused": true
  }
}
```

## 代码结构

- `internal/decimal` — 精确十进制（有理数）预算会计与输入校验
- `internal/dp` — 裁剪、固定分箱、Laplace 噪声机制（官方库）
- `internal/store` — SQLite 持久化、原子预算扣减、查询复用
- `internal/httpapi` — HTTP 路由、管理员鉴权、公开发布接口
- `cmd/server` — 启动入口

## 测试

```sh
go test ./...
```

覆盖：十进制会计与非法输入、裁剪/分箱唯一归属、噪声参数校验、重复用户拒绝、
项目不可变、同查询复用不扣费、超预算拒绝且失败不扣费、并发同查询只发布一次、
重启持久化、HTTP 端到端流程。

