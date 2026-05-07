# LearnFuture - 永续合约模拟学习平台

模拟真实交易所架构的永续合约学习平台。用户使用虚拟资金体验完整的永续合约交易流程，在操盘过程中学习合约的工作原理。

## 架构

```
下单 → 风控校验 → 撮合引擎(订单簿) → 清算(盈亏/手续费) → 账务(余额变更) → 结算(对账)
                                          ↑
                        价格监控(Monitor) ─┘
                        ├─ 止盈止损(TPSL)   用户设定触发价
                        ├─ 限价单触发        价格到达时自动撮合
                        ├─ 强平(Liquidation) 保证金不足时强制平仓
                        ├─ 强盈(ForceTP)     收益率过高时强制止盈
                        └─ ADL              保险基金不足时对手方减仓
```

### 核心模块

| 模块 | 说明 |
|------|------|
| 订单簿 | 价格优先/时间优先撮合，支持市价/限价单 |
| 做市商机器人 | 跟踪外部价格，自动挂单提供流动性 |
| 标记价格 | 现货/合约双源 EMA 平滑，防闪崩操纵 |
| 强平 | 保证金率低于维持保证金率时强制平仓，防止穿仓 |
| 强盈 | 收益率达上限时强制止盈，保护对手方和保险基金 |
| 止盈止损(TPSL) | 用户设定的自动平仓触发价，价格到达时自动执行 |
| 保险基金 | 吸收强平穿仓损失，来源为强平盈余 |
| ADL | 保险基金不足时，按盈利排序对手方自动减仓，破产价结算 |
| Maker/Taker 费率 | 6级 VIP 体系，做市商返佣 |
| 资金费率 | 每 8h 结算，多空互付，锚定现货价格 |
| K线聚合 | 层级聚合 1s→5s→15s→1m→5m→15m→1h→4h→1d，相邻K线价格连续 |
| 教学系统 | 14 个知识点，场景化触发 |

## 技术栈

- **后端**: Go + go-zero
- **数据库**: PostgreSQL
- **缓存**: Go 内存 (无 Redis)
- **前端**: 微信小程序原生
- **价格源**: Gate.io WebSocket + REST

## 快速开始

### 本地开发

```bash
# 1. 启动 PostgreSQL 并建表
createdb learn_future
psql -d learn_future -f scripts/init_db.sql

# 2. 启动服务
make run

# 3. 验证
curl http://localhost:8888/health
curl http://localhost:8888/api/v1/market/price
curl http://localhost:8888/api/v1/market/depth?limit=5
```

### Docker 部署

```bash
cd deploy
docker-compose up -d
```

### 运行测试

```bash
make test          # 单元测试
make test-all      # 全部测试 + 覆盖率
```

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/auth/wx-login | 微信登录 |
| GET | /api/v1/account/info | 账户信息 |
| POST | /api/v1/account/reset | 重置余额 |
| POST | /api/v1/order/place | 下单 |
| POST | /api/v1/order/cancel | 取消挂单 |
| POST | /api/v1/position/close | 平仓 |
| GET | /api/v1/position/list | 持仓列表 |
| GET | /api/v1/market/price | 实时价格 |
| GET | /api/v1/market/depth | 订单簿深度 |
| GET | /api/v1/market/mark-price | 标记价格 |
| GET | /api/v1/market/klines | K线数据 |
| GET | /api/v1/market/funding-rate | 资金费率 |
| GET | /api/v1/market/insurance-fund | 保险基金 |
| WS | /ws?token=xxx | 实时推送 |

## 项目结构

```
internal/
  engine/
    orderbook/     订单簿 (价格优先/时间优先)
    trading/       撮合引擎 (纯内存)
    clearing/      清算 + 结算
    markprice/     标记价格引擎
    insurance/     保险基金
    adl/           自动减仓
    fee/           Maker/Taker 费率
    funding/       资金费率结算
    pricefeed/     价格源 + K线聚合
    marketmaker/   做市商机器人
    position/      PnL/强平价计算
    liquidation/   强平判断
  handler/         HTTP 处理器
  logic/           业务逻辑
  model/           数据库模型
  cache/           内存缓存
  tutorial/        教学系统
  ws/              WebSocket
miniprogram/       微信小程序前端
```

## 声明

本平台为模拟交易学习工具，使用虚拟资金，不涉及真实交易。
