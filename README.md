# LearnFuture - 永续合约模拟学习平台

模拟真实交易所架构的永续合约学习平台。用户使用虚拟资金体验完整的永续合约交易流程，在操盘过程中学习合约的工作原理。

## 架构

```
交易流程：
  下单 → 风控校验 → 订单簿撮合 → Trade[] → 双方持仓变更(updatePosition) → 异步落盘

风控监控（每次价格变化触发）：
  Monitor → 止盈止损(TPSL)    市价单走订单簿平仓
         → 强盈(ForceTP)      市价单走订单簿平仓
         → 强平(Liquidation)  不走订单簿 → 强平引擎接管
         → ADL               不走订单簿 → 直接对手方减仓

强平流程：
  ① 标记价格触发强平 → 仓位以破产价转给强平引擎（不走订单簿，∑多≡∑空维持）
  ② 强平引擎定期以 IOC 限价单在订单簿上逐步平仓（限 1% 滑点）
  ③ 平仓盈亏差额归保险基金
  ④ 保险基金不足 → ADL（以破产价从对手方减仓，结算价不低于对手方开仓价）
```

### 核心设计

**统一持仓变更**：所有订单走同一条路径：订单簿撮合 → `ProcessTrades` → 对买卖双方调 `updatePosition`，自动判断开仓/加仓/减仓/平仓。

**零和系统**：每笔成交同时处理双方持仓。做市商（UserID=0）和强平引擎（UserID=-1）都有真实账户和持仓追踪。用户盈亏 + 对手方盈亏 + 手续费 = 0，∑多头数量 ≡ ∑空头数量。

**强平引擎**：参考 Vega Protocol 和 MEXC 设计。强平不走订单簿（避免流动性不足导致失败），而是以破产价将仓位转给系统方，再由强平引擎逐步在市场上平仓。

**价格监控(Monitor)**：每次外部价格变化时，重新读取所有活跃持仓（防止 stale snapshot），检查止盈止损、强平、强盈。止盈止损和强盈走订单簿（市价单），强平走强平引擎（TakeOver）。

**限价单自动撮合**：限价单放入订单簿后，当任何新单进入订单簿（如做市商每500ms刷新报价），价格交叉时由订单簿自动撮合成交，无需 Monitor 轮询。

**ADL 安全保证**：结算价不低于对手方开仓价（不会让对手方亏钱），只在市场价和破产价都盈利的对手方中选择，按盈利率×杠杆排序。

### 核心模块

| 模块 | 说明 |
|------|------|
| 订单簿 | 价格优先/时间优先撮合，支持市价/限价单 |
| 持仓引擎(updatePosition) | 统一处理开/平/加/减仓，双方对称 |
| 做市商机器人 | 跟踪外部价格，自动挂单提供流动性，成交时回调引擎 |
| 标记价格 | 现货/合约双源 EMA 平滑，防闪崩操纵 |
| 强平引擎 | 以破产价接管仓位，IOC 限价单逐步平仓，盈亏归保险基金 |
| 强盈 | 收益率达上限时强制止盈，保护对手方和保险基金 |
| 止盈止损(TPSL) | 用户设定的触发价，市价单走订单簿平仓，失败下次 tick 重试 |
| 保险基金 | 吸收强平穿仓损失，来源为强平盈余 |
| ADL | 保险基金不足时，按盈利排序对手方自动减仓，结算价有下限保护 |
| Maker/Taker 费率 | 6级 VIP 体系，做市商返佣 |
| 资金费率 | 每 8h 结算，多空互付，余额不足时从保证金扣，锚定现货价格 |
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
    trading/       撮合引擎 + 统一持仓变更 + 强平引擎
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
