# 永续合约模拟学习平台 - 架构设计

## 1. 系统概览

为学习目的搭建的微信小程序永续合约模拟平台。用户微信登录后获得模拟 USDT，可以开多/开空，体验完整的永续合约交易流程。**核心目标：在操盘过程中教会用户永续合约的完整工作原理**。

**技术栈：**
- 后端: Go + go-zero (单体服务)
- 数据库: PostgreSQL (唯一持久化层)
- 缓存: Go 内存 (无 Redis)
- 前端: 微信小程序原生
- 交易对: 仅 BTC/USDT

---

## 2. 系统架构图

```
┌─────────────────────────────────────────────────────────┐
│              微信小程序 (WeChat Mini Program)              │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌──────┐  │
│  │行情/学习│ │ 交易   │ │ 持仓   │ │ 历史   │ │ 我的 │  │
│  └───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘ └──┬───┘  │
│      └──────────┴──────────┴──────────┴─────────┘       │
│         WebSocket (实时价格/PnL)  +  HTTP REST API        │
└──────────┬──────────────────────────────────┬────────────┘
           │ wss://                            │ https://
┌──────────▼──────────────────────────────────▼────────────┐
│                    Nginx (反向代理+SSL)                    │
└──────────┬──────────────────────────────────┬────────────┘
┌──────────▼──────────────────────────────────▼────────────┐
│                Go 后端 (单体服务, go-zero)                  │
│                                                           │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────┐  │
│  │ HTTP API     │  │ WS Hub       │  │ 价格源 WS      │  │
│  │ (REST 接口)   │  │ (推送给客户端) │  │ Client         │  │
│  └──────┬───────┘  └──────┬───────┘  │ (btcusdt@trade)│  │
│         │                 │          └───────┬────────┘  │
│  ┌──────▼─────────────────▼──────────────────▼────────┐  │
│  │                 核心引擎层                            │  │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────────┐  │  │
│  │  │ 价格管理    │ │ 交易引擎    │ │ 仓位管理       │  │  │
│  │  │ + K线聚合   │ │ 市价/限价   │ │ PnL/保证金     │  │  │
│  │  └────────────┘ └────────────┘ └────────────────┘  │  │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────────┐  │  │
│  │  │ 强平/强盈   │ │ 资金费率    │ │ 教学引擎       │  │  │
│  │  │ 引擎       │ │ 8h结算     │ │ 知识点+提示    │  │  │
│  │  └────────────┘ └────────────┘ └────────────────┘  │  │
│  └────────────────────────────────────────────────────┘  │
│                                                           │
│  ┌────────────────────────────────────────────────────┐  │
│  │              内存缓存层 (替代 Redis)                  │  │
│  │  • 最新价格 (atomic)     • K线当前K棒 (mutex)        │  │
│  │  • 活跃持仓 (sync.Map)   • 挂单列表 (mutex+slice)    │  │
│  │  • WS 连接池 (sync.Map)  • JWT: 无状态不需缓存       │  │
│  │  ※ 全部数据同时持久化到 PG，重启从 PG 恢复           │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────────────────┬───────────────────────────────┘
                    ┌──────▼──────┐
                    │ PostgreSQL  │
                    │ (唯一持久化) │
                    └─────────────┘
```

---

## 3. 内存缓存设计 — 重启安全性

| 内存数据 | Go 实现 | 重启恢复方式 | 丢失影响 |
|---------|---------|-------------|---------|
| BTC 最新价格 | `atomic.Value` | WS 重连后 <1s 恢复 | 无 |
| K线当前未闭合K棒 | `sync.Mutex` + struct | 丢失当前不完整的1根K棒 | 极小 |
| 活跃持仓列表 | `sync.Map` | 启动时从 PG 加载 `status=1` | 无丢失 |
| 挂单(限价单) | `sync.Mutex` + 排序slice | 启动时从 PG 加载 `status=1` | 无丢失 |
| 用户 WS 连接 | `sync.Map` | 客户端自动重连 | 无丢失 |
| JWT token | 无状态(签名验证) | 无需恢复 | 无 |

**核心原则：内存仅作缓存加速层，所有业务数据实时持久化到 PostgreSQL。重启 = PG恢复 + WS重连。**

---

## 4. 教学系统设计 (核心差异化)

### 4.1 设计理念

**将教学融入交易的每一步操作中**，用户在"做"的过程中"学"。不是单独的教程页面。

### 4.2 教学触发机制 — 场景化知识卡片

| 触发时机 | 知识点ID | 示例内容 |
|---------|---------|---------|
| 首次下单 | `perpetual_intro` | "永续合约没有到期日，可以无限期持有。它通过资金费率机制锚定现货价格" |
| 选择杠杆 | `leverage_explained` | "10x杠杆意味着用100U保证金控制1000U的仓位。杠杆放大收益也放大亏损" |
| 开多/开空 | `long_short` | "开多=看涨，价格上涨盈利；开空=看跌，价格下跌盈利" |
| 持仓中 | `unrealized_pnl` | "未实现盈亏 = (当前价-开仓价) × 数量 × 方向。平仓前都是浮动的" |
| 设置TP/SL | `tp_sl_risk` | "止盈止损是预设的自动平仓价格，是风控的核心工具" |
| 资金费率结算 | `funding_rate` | "资金费率每8h结算，用于锚定合约价格到现货。费率>0时多头付给空头" |
| 接近强平 | `liquidation` | "保证金率低于0.5%时将被强平，杠杆越高离强平越近" |
| 被强平后 | `post_liquidation` | "仓位在{liq_price}被强平，损失全部保证金{margin}U" |
| 被强盈后 | `force_tp_adl` | "你的仓位收益触发了对手方LP的风控上限被强制平仓" |
| 平仓后 | `realized_pnl` | "本次盈亏={pnl}U，手续费={fee}U，资金费用={funding}U" |
| 市价单 | `market_order` | "市价单以当前市场价格立即成交，确保成交但不保证价格" |
| 限价单 | `limit_order` | "限价单在价格达到指定价位时才触发，可以获得更好的成交价" |
| 手续费 | `trading_fee` | "手续费 = 仓位价值 × 费率(0.04%)，开仓和平仓各收一次" |

### 4.3 API 响应附带教学字段

```json
{
  "code": 0,
  "data": { ... },
  "tutorial": {
    "id": "leverage_explained",
    "title": "什么是杠杆？",
    "content": "10x杠杆意味着...",
    "formula": "保证金 = 仓位价值 / 杠杆倍数",
    "example": "100U × 10倍 = 控制1000U仓位",
    "show_once": true
  }
}
```

### 4.4 学习进度追踪 (13个知识点)

```
永续合约学习进度: 7/13 ██████░░░░░ 54%
✅ 什么是永续合约    ✅ 杠杆与保证金
✅ 多空机制          ✅ 市价/限价单
✅ 未实现盈亏        ✅ 手续费计算
✅ 止盈止损          ⬜ 资金费率
⬜ 强制平仓          ⬜ 强盈与ADL
⬜ 标记价格          ⬜ 仓位管理技巧
⬜ 风险控制
```

---

## 5. 强平与强盈引擎

### 5.1 强平 (Liquidation) — 亏损方向

```
强平价(多) = 开仓价 × (1 - 1/杠杆 + 0.005)
强平价(空) = 开仓价 × (1 + 1/杠杆 - 0.005)
保证金率 = (保证金 + 未实现盈亏) / (数量 × 当前价)
当 保证金率 <= 0.5% → 触发强平
```

### 5.2 强盈 (Forced Take-Profit / ADL) — 盈利方向

```
强盈触发条件: 未实现收益率 >= 500% (仓位价值达保证金6倍)
强盈价(多) = 开仓价 × (1 + 5/杠杆)
强盈价(空) = 开仓价 × (1 - 5/杠杆)
```

---

## 6. API 设计

### 认证
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/wx-login` | 微信登录 |
| POST | `/api/v1/auth/refresh` | 刷新token |

### 账户
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/account/info` | 余额、总盈亏、学习进度 |
| POST | `/api/v1/account/reset` | 重置余额为10000U |

### 交易
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/order/place` | 下单 |
| POST | `/api/v1/order/cancel` | 取消限价单 |
| POST | `/api/v1/position/close` | 平仓 |
| POST | `/api/v1/position/update-tpsl` | 修改止盈止损 |
| GET | `/api/v1/position/list` | 当前持仓 |
| GET | `/api/v1/order/pending` | 挂单列表 |

### 行情
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/market/price` | BTC/USDT 当前价格 |
| GET | `/api/v1/market/klines` | K线数据 |
| GET | `/api/v1/market/funding-rate` | 当前资金费率 |

### 历史
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/history/orders` | 历史订单 |
| GET | `/api/v1/history/trades` | 成交记录 |
| GET | `/api/v1/history/funding` | 资金费率结算记录 |
| GET | `/api/v1/history/pnl` | 盈亏汇总 |

### 排行榜
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/leaderboard/pnl` | 总盈亏排名 |
| GET | `/api/v1/leaderboard/roi` | 收益率排名 |

### 教学
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/tutorial/progress` | 学习进度 |
| POST | `/api/v1/tutorial/complete` | 标记知识点已学习 |

### WebSocket
```
wss://domain/ws?token=<jwt>

推送消息类型:
- ticker: 实时行情
- position_pnl: 持仓盈亏更新 (每次价格变化)
- order_filled: 订单成交 (maker 限价单被吃时通知)
- liquidated: 强平通知
- force_tp: 强盈通知
- adl: ADL 减仓通知
- funding_settled: 资金费率结算
```

---

## 7. 数据库设计 (PostgreSQL)

### 表结构

| 表名 | 说明 | 关键字段 |
|------|------|---------|
| `users` | 用户 | openid, nickname, avatar_url |
| `accounts` | 账户 | balance, frozen, total_pnl, reset_count |
| `positions` | 持仓 | side, leverage, entry_price, quantity, margin, liq_price, force_tp_price, status |
| `orders` | 订单 | side, order_type, leverage, price, quantity, margin_cost, status |
| `trades` | 成交 | price, quantity, fee, realized_pnl, is_close, close_reason |
| `funding_rates` | 资金费率历史 | rate, settle_time |
| `funding_settlements` | 资金费结算记录 | position_id, rate, position_value, payment |
| `klines` | K线 | interval, open_time, OHLCV |
| `tutorial_progress` | 学习进度 | user_id, topic_id |

### 关键索引
- `idx_positions_user_status`: positions(user_id, status)
- `idx_orders_user_status`: orders(user_id, status)
- `idx_trades_user`: trades(user_id, created_at DESC)
- `idx_klines_query`: klines(interval, open_time DESC)

---

## 8. 撮合引擎详细设计

### 8.1 撮合模型

本平台采用**用户 vs 系统**的对手盘模型，系统作为拥有无限资金的虚拟流动性提供者(LP)。虽然不需要真正的 orderbook 双边撮合，但完全模拟真实交易所的撮合和清结算流程。

```
                    ┌─────────────────────���───┐
                    │      撮合引擎            │
                    │   (trading/engine.go)    │
                    └────────┬────────────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
    ┌───��─────────┐  ┌────────────┐  ┌────────────┐
    │  市价单撮合  │  │ 限价单监控  │  │ TP/SL触发  │
    │ 立即以最新   │  │ 价格穿越时  │  │ 价格到达时  │
    │ 价格��交    │  │ 按限价成交  │  │ 自动平仓    │
    └─────┬───────┘  └─────┬──────┘  └─────┬──────┘
          │                │               │
          ▼                ▼               ▼
    ┌──────────────────────────────────────────┐
    │            清结算引擎                      │
    │  1. 资金划转 (保证金扣除/冻结/归还)        │
    │  2. 仓位生命周期管理                       │
    │  3. 手续费计算与扣除                       │
    │  4. 盈亏结算 (已实现/未实现)               │
    │  5. 成交记录生成                           │
    └──────────────────────────────────────────┘
```

### 8.2 Order-First 架构

一次用户操作 = 一个 Order，撮合产生 N 笔 Trade（一个价格档位一笔）。
Order 在调用方创建（EventCreateOrder），Trade 在 UpdatePosition 中创建（EventTrade）。
Order ID 由 book.NextOrderID() 统一分配，内存和 DB 共享同一套 ID。

```
数据流:
  调用方 → 创建 Order (ID=book.NextOrderID()) → EventCreateOrder → settler 写 DB
  调用方 → 订单簿撮合 → N 笔 orderbook.Trade
  ProcessTrades → 对每笔 trade 调 UpdatePosition(orderID)
    → handleOpen/Increase/Reduce → 创建 model.Trade(OrderID=orderID) → EventTrade → settler 写 DB

ID 一致性:
  orderbook.Order.ID = model.Order.ID = book.NextOrderID()
  启动时从 DB MAX(id) 初始化，保证重启后不冲突

系统用户 (UserID≤0, 做市商/强平引擎):
  内存持仓正常维护，但跳过 DB trade 记录写入
```

### 8.3 市价单撮合流程

```
1. [校验] side ∈ {1,-1}, leverage ∈ [1,125], margin ≥ 1 USDT
2. [取价] currentPrice = PriceCache.GetPrice() (必须非零)
3. [预检] 余额 ≥ margin + fee (乐观检查，真正扣款在 UpdatePosition)
4. [创建 Order] orderID = book.NextOrderID(), submitOrder → EventCreateOrder
5. [撮合] PlaceMarket → N 笔 orderbook.Trade
6. [持仓变更] ProcessTrades(trades, userID, side, leverage, orderID)
   - 第1笔 → handleOpen: 创建仓位 + trade(orderID)
   - 第2~N笔 → handleIncrease: 加权均价合并 + trade(orderID)
   - 对手方(maker): trade 引用 maker 的 orderbook order ID
7. [返回] 仓位信息 + 教学卡片，跳转到持仓页
```

### 8.4 限价单撮合流程

```
挂单阶段:
1. [校验] 同市价单 + limitPrice > 0 + 价格偏差 ≤ 10%
2. [冻结] memAccounts.Freeze(totalCost)
3. [创建 Order] orderID = obOrder.ID, submitOrder(status=pending) → EventCreateOrder
4. [放入订单簿] book.PlaceLimit
   - 价格交叉 → 立即成交 (走 taker 路径)
   - 未交叉 → resting 在订单簿，加入 orderCache

成交阶段 (由订单簿自动撮合，不由 Monitor 轮询):
  做市商每500ms刷新报价 → 新单进入订单簿 → 价格交叉 → 撮合成交
  → onTrades 回调 → ProcessTrades → maker 的 trade 引用已有 orderID
  → WS 推送 order_filled 通知 maker
```

### 8.5 平仓清结算流程

```
1. [校验] 仓位存在 && status=active && 属于当前用户
2. [CAS] TrySetState(Closing) — 防止并发重复平仓
3. [创建 Order] closeOrderID = book.NextOrderID(), submitOrder → EventCreateOrder
4. [撮合] PlaceMarket(反向) → N 笔 trade
5. [持仓变更] ProcessTrades → handleReduce
   - 计算 PnL = (closePrice - entryPrice) × qty × side - fee + fundingPnl
   - 内存: ReturnMarginWithPnl (退保证金+盈亏)
   - settler: EventTrade (trade + position 状态 + balance 退还)
   - 强平: 走 TakeOver (不走订单簿), 用户失去全部保证金
6. [WS推送] 根据 close_reason 推送不同消息
```

### 8.6 保证金账户模型

```
┌──────────────────────────────────────┐
│         Account (per user)           │
│                                      │
│  balance ──→ 可用余额 (可下单/提取)   │
│  frozen  ──→ 冻结金额 (限价单占用)    │
│  total_pnl → 累计已实现盈亏          │
│                                      │
│  可用余额 = balance - frozen          │
│                                      │
│  资金流转:                            │
│  ┌─────────┐     ┌─────────┐         │
│  │ balance  │────→│ frozen  │ 挂限价单│
│  │         │←────│         │ 取消/成交│
│  └────┬────┘     └─────────┘         │
│       │                              │
│       │ 市价开仓: -margin-fee        │
│       │ 平仓: +margin+netPnl        │
│       │ 强平: 0 (保证金已扣)         │
│       │ 资金费: ±payment             │
│       ▼                              │
│  total_pnl: 累加每笔已实现盈亏       │
└──────────────────────────────────────┘
```

### 8.6 风控引擎 (Monitor)

Monitor 在每次价格更新时基于持仓快照执行检查（优先级从高到低）。
限价单通过订单簿自动撮合（做市商刷新报价时价格交叉触发），不由 Monitor 轮询。
执行层通过 CAS（TrySetState）保证幂等性，防止并发重复操作。

```
OnPriceUpdate(lastPrice):
  1. 取快照: allPositions = positionCache.GetAll()
  2. 遍历所有活跃持仓:
     a. 强平检查 (用 markPrice): 保证金率 ≤ 0.5% → TakeOver (不走订单簿)
     b. 强盈检查: 收益率 ≥ 500% → ClosePositionInternal (市价单走订单簿)
     c. 止盈检查: lastPrice 穿过 TP 价 → ClosePositionInternal
     d. 止损检查: lastPrice 穿过 SL 价 → ClosePositionInternal
     e. 推送 PnL: 计算并推送实时未实现盈亏/保证金率/ROI/ADL灯
  3. 强平引擎: OnTick 尝试处置接管的持仓 (IOC限价单)
```

### 8.7 PnL 计算公式

```
未实现盈亏 = (当前价 - 开仓价) × 数量 × 方向
收益率(ROI) = 未实现盈亏 / 保证金 × 100%
已实现盈亏 = (平仓价 - 开仓价) × 数量 × 方向 - 平仓手续费
保证金率 = (保证金 + 未实现盈亏) / (数量 × 当前价)
净盈亏 = 已实现盈亏 + 累计资金费
```

### 8.8 资金费率结算

```
结算时间: 每8小时 (00:00/08:00/16:00 UTC)
费率来源: 外部 REST API 获取真实费率

结算流程:
1. 获取最新费率 → 写入 funding_rates 表
2. 遍历所有活跃持仓:
   - positionValue = quantity × currentPrice
   - payment = positionValue × rate × (-side)
   - rate > 0: 多头付钱(payment为负), 空���收钱(payment为正)
   - rate < 0: 反之
3. 更新 positions.funding_pnl += payment
4. 更新 accounts.balance += payment
5. 写入 funding_settlements 表
6. WS 推送 funding_settled
```

---

## 8A. 标记价格引擎 (Mark Price)

### 为什么需要标记价格？

在真实交易所中，如果使用最新成交价计算强平，攻击者可以通过刷一笔异常低价交易来触发大量用户的强制平仓。标记价格通过多层防护机制防止这种操纵。

### 双价格源设计 (对标真实交易所)

即使只有一个价格源（如 Binance），也可以取两种不同类型的价格来产生真实基差：

```
                    ┌─ Binance ─────────────────────┐
                    │                                │
    现货 API ───────┤  api.binance.com               │
    (REST, 每5秒)    │  /api/v3/ticker/price          │──→ 指数价格 (Index Price)
                    │  BTCUSDT 现货最新价              │     = 现货价, 难以操纵(无杠杆)
                    │                                │
    合约 WS ────────┤  fstream.binance.com            │
    (实时, 每笔)     │  /ws/btcusdt@trade             │──→ 最新成交价 (Last Price)
                    │  BTCUSDT 永续合约成交            │     = 合约价, 含杠杆溢价
                    └────────────────────────────────┘

    现货 vs 合约天然有基差:
    现货: 60000 (无杠杆, 稳定)
    合约: 60120 (有杠杆溢价, 0.2% premium)
    基差 = (60120-60000)/60000 = +0.2%
```

### 计算公式

```
指数价格 = 现货价格 (通过 REST API 每5秒获取)
最新成交价 = 合约价格 (通过 WS 实时推送)
基差 = (合约价 - 现货价) / 现货价
EMA基差 = α × 当前基差 + (1-α) × 上次EMA基差   (α=0.1)
标记价格 = 指数价格 × (1 + EMA基差)
```

### 使用场景

| 场景 | 使用价格 | 原因 |
|------|---------|------|
| 开仓/平仓成交 | 合约最新成交价 | 反映合约市场真实供需 |
| 强平判断 | **标记价格** | 基于现货, 防止合约价格操纵 |
| 未实现盈亏显示 | 合约最新成交价 | 反映实时市场状况 |
| 资金费率计算 | 标记价格 | 防止操纵 |

### 防操纵效果 (测试验证)

```
场景: 有人在合约市场制造闪崩 (54000)
现货: 仍然 60000 (现货无杠杆, 闪崩不影响)
标记价格: 59400 (跟随现货, 不跟随合约闪崩)
结果: 强平价 55000 的用户不会被误杀 ✓
```

### 实现

```
engine/markprice/engine.go
- UpdateLastPrice(price):  合约 WS 每笔成交调用
- UpdateIndexPrice(price): REST 现货价格定期更新
- GetMarkPrice():          获取标记价格 (用于强平判断)
- Start():                 启动现货价格定期抓取 (每5秒)
- GetBasis():              获取当前基差 (教学展示)
```

---

## 8B. 保险基金 (Insurance Fund)

### 机制说明

保险基金是交易所的风险缓冲池，用于吸收强平产生的穿仓损失。

```
强平时的资金流:

正常强平 (有余额):
  强平价格 > 破产价格 → 剩余保证金 → 保险基金 (盈余)

穿仓强平 (价格跳空):
  强平价格 < 破产价格 → 亏损超出保证金 → 保险基金承担 (亏损)

保险基金耗尽:
  → 触发 ADL (自动减仓)
```

### 关键公式

```
破产价格(多) = 开仓价 × (1 - 1/杠杆)     // 保证金归零的价格
强平价格(多) = 开仓价 × (1 - 1/杠杆 + 0.5%)  // 提前强平的价格

盈余 = (强平价 - 破产价) × 数量 × 方向
  > 0: 保险基金获得盈余
  < 0: 穿仓，保险基金承担
```

### 实现

```
engine/insurance/fund.go
- Contribute(): 强平盈余存入基金
- Cover(): 穿仓时基金承担损失
- 初始资金: 100万虚拟 USDT

强平清算由 trading/liquidation_engine.go 的 TakeOver + OnTick 处理
```

---

## 8C. ADL 自动减仓系统 (Auto-Deleveraging)

### 触发条件

当保险基金无法覆盖穿仓损失时，系统必须从对手方仓位中强制减仓来弥补亏损。

### ADL 优先级排序

```
对手方仓位按以下公式排序 (从高到低):
ADL分数 = 收益率(ROI) × 杠杆倍数

最赚钱 + 最高杠杆 = 最先被 ADL
```

### ADL 指示灯

```
用户看到的 ADL 风险指示 (1-5灯):
  5灯: ROI > 400% — 极高风险，很可能被 ADL
  4灯: ROI > 300%
  3灯: ROI > 200%
  2灯: ROI > 100%
  1灯: ROI ≤ 100% — 低风险
```

### 实现

```
engine/adl/engine.go
- RankPositions(): 按 ADL 优先级排序对手方仓位
- ExecuteADL(): 从高优先级开始逐个减仓直到弥补亏损
- GetADLIndicator(): 返回 1-5 的风险等级
```

---

## 8D. Maker/Taker 费率体系

### 费率结构

| 等级 | 30天交易量 | Maker费率 | Taker费率 | 说明 |
|------|-----------|-----------|-----------|------|
| 普通用户 | < 10万U | 0.02% | 0.04% | 默认 |
| VIP1 | ≥ 10万U | 0.018% | 0.035% | |
| VIP2 | ≥ 50万U | 0.015% | 0.03% | |
| VIP3 | ≥ 200万U | 0.01% | 0.025% | |
| VIP4 | ≥ 1000万U | 0.005% | 0.02% | |
| 做市商 | ≥ 5000万U | **-0.005%** | 0.015% | Maker返佣! |

### Maker vs Taker

- **Taker**: 市价单，"吃掉"订单簿流动性 → 费率高
- **Maker**: 限价单，"提供"订单簿流动性 → 费率低 (甚至返佣)

### 实现

```
engine/fee/calculator.go
- GetTier(): 根据30天交易量确定费率等级
- CalcFee(): 根据Maker/Taker计算手续费
- 做市商返佣: Maker费率为负，下单反而赚钱
```

---

## 8E. T+1 对账系统 (Reconciliation)

### 检查项

| 检查 | 说明 | 比较精度 |
|------|------|---------|
| 余额对账 | 内存余额 vs DB 余额 | 8 位小数 |
| 持仓对账 | 内存活跃持仓 vs DB 活跃持仓 | 数量 + 存在性 |
| 零和校验 | ∑多头数量 ≡ ∑空头数量 | 精确 |

### 运行机制

- 启动后 1 分钟首次运行，之后每天 UTC 04:00 运行
- 发现差异时日志告警 + 邮件通知管理员
- 只报警不自动修复

### 实现

```
engine/reconcile/reconcile.go
- checkBalances(): 逐用户比较内存 vs DB 余额
- checkPositions(): 逐持仓比较内存 vs DB 数量
- checkZeroSum(): 验证 ∑longs ≡ ∑shorts
- sendAlert(): 差异邮件通知
```

---

## 9. 价格源架构

```
外部 WebSocket ──(btcusdt@trade)──> PriceFeedManager
                                         │
                                         ├─> 内存 (atomic最新价格)
                                         ├─> K线聚合器 (内存桶 → PG)
                                         ├─> WS Hub → 小程序客户端 (200ms批量)
                                         ├─> 交易引擎 (限价单/TP/SL 触发检查)
                                         ├─> 强平引擎 (保证金率检查)
                                         └─> 强盈引擎 (收益率检查)
```

**合规：前端不展示价格来源，API 不包含 "binance" 字样。**

---

## 10. 项目目录结构

```
learn_future/
├── cmd/server/main.go
├── etc/config.yaml
├── internal/
│   ├── config/config.go
│   ├── handler/
│   ├── logic/
│   ├── middleware/auth_middleware.go
│   ├── model/
│   ├── types/types.go
│   ├── engine/
│   │   ├── pricefeed/
│   │   ├── trading/
│   │   ├── position/manager.go
│   │   ├── reconcile/reconcile.go
│   │   └── funding/scheduler.go
│   ├── cache/
│   ├── tutorial/
│   ├── ws/
│   └── svc/service_context.go
├── pkg/
│   ├── jwt/jwt.go
│   └── response/response.go
├── miniprogram/
├── scripts/init_db.sql
├── deploy/
├── go.mod
└── Makefile
```

---

## 11. K线支持 & 内存开销

### K线时间单位
`1m`, `5m`, `15m`, `1h`, `4h`, `1d`

### 内存缓存

| Interval | 缓存数量 | 覆盖时长 |
|----------|---------|---------|
| 1m | 1440 根 | 24小时 |
| 5m | 288 根 | 24小时 |
| 15m | 192 根 | 2天 |
| 1h | 168 根 | 7天 |
| 4h | 180 根 | 30天 |
| 1d | 365 根 | 1年 |

### 内存开销 (单交易对)

| 数据项 | 内存 |
|-------|------|
| 最新价格 | ~100B |
| K线缓存 (2633根) | ~210 KB |
| 活跃持仓 (1000用户×3) | ~1.5 MB |
| 挂单列表 (5000单) | ~1.5 MB |
| WS连接 (1000) | ~6 MB |
| Go运行时 | ~20 MB |
| **总计 (1000并发)** | **~30 MB** |

---

## 12. 实施阶段

| Phase | 内容 | 依赖 |
|-------|------|------|
| **Phase 1** | 基础设施：项目脚手架、PG建表、模型层、内存缓存、价格源WS、HTTP健康检查 | 无 |
| **Phase 2** | 核心交易：微信登录+JWT、账户管理、市价单、仓位管理+PnL、WS Hub | Phase 1 |
| **Phase 3** | 高级交易：限价单、止盈止损、强平/强盈引擎、资金费率、K线聚合 | Phase 2 |
| **Phase 4** | 教学系统：13个知识点、触发逻辑、学习进度 | Phase 2 |
| **Phase 5** | 小程序前端：所有页面 + 教学组件 | Phase 3+4 |
