# 永续合约模拟学习平台 - TDD 测试方案

## 测试策略总览

采用 **分层测试** 策略，从纯计算单元到集成场景逐层覆盖：

```
                    ┌──────────────┐
                    │  E2E 测试     │  ← 少量关键场景
                    │  (API级别)    │
                ┌───┴──────────────┴───┐
                │   集成测试             │  ← 引擎 + PG 联合
                │   (Engine + DB)       │
            ┌───┴──────────────────────┴───┐
            │       单元测试                 │  ← 核心：纯逻辑
            │  (PnL/强平/强盈/资金费率/教学) │
            └──────────────────────────────┘
```

**原则：**
- 先写测试，再写实现 (Red → Green → Refactor)
- 交易引擎的数学计算必须 100% 单元测试覆盖
- 数据库相关用 dockertest 起真实 PG
- 内存缓存用接口抽象，测试时可 mock

---

## Phase 1 测试：基础设施

### 1.1 内存缓存层 (`internal/cache/`)

#### price_cache_test.go — 价格缓存
```
Test_PriceCache_SetAndGet
  ✓ 设置价格后能正确读取
  ✓ 并发读写安全 (1000 goroutine)
  ✓ 未设置时返回零值

Test_PriceCache_Ticker
  ✓ 设置 ticker 后返回完整的 high/low/change
  ✓ high 只更新更高值
  ✓ low 只更新更低值
```

#### position_cache_test.go — 持仓缓存
```
Test_PositionCache_CRUD
  ✓ 添加持仓后能查到
  ✓ 按用户ID查询返回该用户所有活跃持仓
  ✓ 删除持仓后查不到
  ✓ 获取所有活跃持仓

Test_PositionCache_Concurrent
  ✓ 并发添加/删除安全
```

#### order_cache_test.go — 挂单缓存
```
Test_OrderCache_Add
  ✓ 添加挂单后按价格排序
  ✓ 多单按价格降序，空单按价格升序

Test_OrderCache_Remove
  ✓ 删除挂单后不再返回

Test_OrderCache_GetTriggered
  ✓ 当前价 <= 限价时返回待触发的多单
  ✓ 当前价 >= 限价时返回待触发的空单
  ✓ 无触发条件时返回空
```

### 1.2 K线聚合器 (`internal/engine/pricefeed/`)

#### kline_aggregator_test.go
```
Test_KlineAggregator_OnTrade
  ✓ 第一笔交易初始化 OHLC
  ✓ 后续交易更新 high/low/close
  ✓ 不更新 open

Test_KlineAggregator_IntervalClose
  ✓ 1m K线 60秒后闭合，输出完整K棒
  ✓ 闭合后新交易开启新K棒
  ✓ 多 interval 同时聚合互不影响

Test_KlineAggregator_GetCachedKlines
  ✓ 返回指定 interval 的缓存K线
  ✓ 缓存数量不超过上限 (如1m最多1440根)
  ✓ 按时间倒序返回
```

### 1.3 价格源 (`internal/engine/pricefeed/`)

#### manager_test.go
```
Test_PriceFeedManager_OnMessage
  ✓ 解析交易消息更新价格缓存
  ✓ 触发回调 (K线聚合、引擎检查)

Test_PriceFeedManager_Reconnect
  ✓ 连接断开后自动重连
  ✓ 重连使用指数退避
```

---

## Phase 2 测试：核心交易

### 2.1 PnL 计算 (`internal/engine/position/`)

#### pnl_test.go — 纯数学，最关键
```
Test_CalcUnrealizedPnL
  ✓ 多仓盈利: entry=60000, current=65000, qty=0.1, side=1 → +500
  ✓ 多仓亏损: entry=60000, current=55000, qty=0.1, side=1 → -500
  ✓ 空仓盈利: entry=60000, current=55000, qty=0.1, side=-1 → +500
  ✓ 空仓亏损: entry=60000, current=65000, qty=0.1, side=-1 → -500
  ✓ 价格不变: pnl=0

Test_CalcRealizedPnL
  ✓ 多仓止盈: (close-entry)*qty*1 - fee
  ✓ 空仓止盈: (entry-close)*qty*1 - fee  (side=-1 flipped)
  ✓ 扣除手续费后实际盈亏

Test_CalcROI
  ✓ 收益率 = unrealizedPnl / margin * 100%
  ✓ 10x杠杆, 价格涨10% → ROI=100%

Test_CalcMarginRatio
  ✓ 保证金率 = (margin + upnl) / (qty * currentPrice)
  ✓ 亏损接近保证金时保证金率趋近0
```

### 2.2 强平价/强盈价计算

#### liquidation_price_test.go
```
Test_CalcLiquidationPrice
  ✓ 多仓: liqPrice = entryPrice * (1 - 1/leverage + 0.005)
    - entry=60000, lev=10 → liqPrice = 60000*(1-0.1+0.005) = 54300
    - entry=60000, lev=20 → liqPrice = 60000*(1-0.05+0.005) = 57300
    - entry=60000, lev=100 → liqPrice = 60000*(1-0.01+0.005) = 59700
  ✓ 空仓: liqPrice = entryPrice * (1 + 1/leverage - 0.005)
    - entry=60000, lev=10 → liqPrice = 60000*(1+0.1-0.005) = 65700
  ✓ 1x杠杆多仓: liqPrice趋近0 (不会被强平)

Test_CalcForceTpPrice
  ✓ 多仓: ftpPrice = entryPrice * (1 + 5/leverage)
    - entry=60000, lev=10 → ftpPrice = 60000*(1+0.5) = 90000
  ✓ 空仓: ftpPrice = entryPrice * (1 - 5/leverage)
    - entry=60000, lev=10 → ftpPrice = 60000*(1-0.5) = 30000
  ✓ 高杠杆强盈价更接近开仓价
```

### 2.3 交易引擎 (`internal/engine/trading/`)

#### engine_test.go
```
Test_PlaceMarketOrder_Long
  ✓ 余额充足 → 扣款、创建订单、创建持仓
  ✓ 返回正确的 quantity = margin * leverage / price
  ✓ 返回正确的强平价和强盈价
  ✓ 手续费 = positionValue * 0.0004

Test_PlaceMarketOrder_Short
  ✓ 空仓创建成功，side=-1

Test_PlaceMarketOrder_InsufficientBalance
  ✓ 余额不足 → 返回错误，不创建订单

Test_PlaceMarketOrder_NoPriceAvailable
  ✓ 无价格数据 → 返回错误

Test_ClosePosition
  ✓ 平仓后状态变为 closed
  ✓ 保证金归还账户
  ✓ 盈利入账 / 亏损扣除
  ✓ 已实现盈亏正确计算
  ✓ 平仓后从活跃持仓缓存移除
```

### 2.4 账户管理 (`internal/logic/`)

#### account_logic_test.go
```
Test_AccountReset
  ✓ 余额重置为 10000
  ✓ 有活跃持仓时拒绝重置
  ✓ reset_count 增加

Test_AccountInfo
  ✓ 返回余额、冻结、总盈亏
```

### 2.5 JWT 认证 (`pkg/jwt/`)

#### jwt_test.go
```
Test_GenerateToken
  ✓ 生成有效 token
  ✓ 解析后得到正确的 user_id

Test_ValidateToken
  ✓ 有效 token 验证通过
  ✓ 过期 token 返回错误
  ✓ 篡改 token 返回错误

Test_RefreshToken
  ✓ 快过期的 token 能刷新
  ✓ 已过期的 refresh token 返回错误
```

---

## Phase 3 测试：高级交易

### 3.1 限价单监控 (`internal/engine/trading/`)

#### monitor_test.go
```
Test_LimitOrderMonitor_LongTrigger
  ✓ 限价多单 price=59000, 当前价跌到 58900 → 触发成交
  ✓ 当前价 59100 → 不触发

Test_LimitOrderMonitor_ShortTrigger
  ✓ 限价空单 price=61000, 当前价涨到 61100 → 触发成交
  ✓ 当前价 60900 → 不触发

Test_LimitOrderMonitor_Cancel
  ✓ 取消后不再触发
  ✓ 冻结保证金返还
```

### 3.2 止盈止损

#### tpsl_test.go
```
Test_TakeProfit_Long
  ✓ 多仓设置 tp=65000, 价格涨到 65100 → 自动平仓, 盈利入账

Test_StopLoss_Long
  ✓ 多仓设置 sl=58000, 价格跌到 57900 → 自动平仓, 亏损扣除

Test_TakeProfit_Short
  ✓ 空仓设置 tp=55000, 价格跌到 54900 → 自动平仓

Test_StopLoss_Short
  ✓ 空仓设置 sl=62000, 价格涨到 62100 → 自动平仓

Test_UpdateTPSL
  ✓ 修改止盈止损价格成功
  ✓ 止盈价格方向校验 (多仓tp必须>开仓价)
```

### 3.3 强平引擎 (`internal/engine/liquidation/`)

#### engine_test.go
```
Test_Liquidation_Long
  ✓ 多仓保证金率降到0.5%以下 → 触发强平
  ✓ 仓位状态变为 liquidated (status=3)
  ✓ 保证金全部亏损，余额不变
  ✓ 从活跃持仓移除

Test_Liquidation_Short
  ✓ 空仓保证金率降到0.5%以下 → 触发强平

Test_Liquidation_NotTriggered
  ✓ 保证金率>0.5% → 不触发

Test_ForceTakeProfit_Long
  ✓ 多仓收益率达500% → 触发强盈
  ✓ 仓位状态变为 force_tp (status=4)
  ✓ 盈利入账

Test_ForceTakeProfit_Short
  ✓ 空仓收益率达500% → 触发强盈

Test_ForceTakeProfit_NotTriggered
  ✓ 收益率<500% → 不触发
```

### 3.4 资金费率 (`internal/engine/funding/`)

#### scheduler_test.go
```
Test_FundingSettlement_LongPaysShort
  ✓ rate=0.0001 (正), 多仓 → 支付资金费
  ✓ payment = positionValue * rate * (-1)

Test_FundingSettlement_ShortPaysLong
  ✓ rate=-0.0001 (负), 空仓 → 支付资金费

Test_FundingSettlement_MultipePositions
  ✓ 所有活跃持仓同时结算

Test_FundingSettlement_AffectsBalance
  ✓ 支付方余额减少
  ✓ 收取方余额增加 (模拟环境从系统获得)

Test_FundingScheduler_Timing
  ✓ 在 00:00/08:00/16:00 UTC 触发结算
```

### 3.5 K线聚合 (补充)

#### kline_aggregator_integration_test.go (需要 PG)
```
Test_KlineClose_PersistToPG
  ✓ K线闭合后写入数据库
  ✓ 数据库中 OHLCV 值正确
  ✓ 相同 interval+open_time 不重复插入
```

---

## Phase 4 测试：教学系统

### 4.1 教学触发逻辑 (`internal/tutorial/`)

#### trigger_test.go
```
Test_ShouldShowTutorial
  ✓ 用户未学过该知识点 → 返回 tutorial 内容
  ✓ 用户已学过该知识点 → 返回 nil
  ✓ show_once=false 的知识点每次都返回

Test_TriggerOnFirstOrder
  ✓ 首次下单触发 "perpetual_intro"

Test_TriggerOnLeverageChange
  ✓ 选择杠杆触发 "leverage_explained"

Test_TriggerOnLiquidation
  ✓ 被强平后触发 "post_liquidation"
  ✓ 内容包含实际的强平价和保证金数值

Test_TriggerOnForceTP
  ✓ 被强盈后触发 "force_tp_adl"
  ✓ 内容解释ADL机制
```

#### topics_test.go
```
Test_AllTopicsDefined
  ✓ 13个知识点全部定义
  ✓ 每个知识点有 id, title, content

Test_TopicContent
  ✓ 每个知识点内容非空
  ✓ 包含公式的知识点有 formula 字段
```

#### progress_test.go (集成测试)
```
Test_MarkComplete
  ✓ 标记知识点已完成
  ✓ 重复标记不报错 (幂等)

Test_GetProgress
  ✓ 返回已完成的知识点列表
  ✓ 返回总进度 (完成数/总数)
```

---

## Phase 3B 测试：标记价格引擎

### markprice/engine_test.go
```
Test_MarkPrice_InitialState
  ✓ 初始标记价格为零

Test_MarkPrice_FirstPrice
  ✓ 首次设置价格后标记价格等于该价格

Test_MarkPrice_Smoothed
  ✓ 最新价=指数价时标记价等于两者

Test_MarkPrice_BetweenLastAndIndex
  ✓ 最新价偏离指数价时标记价在两者之间

Test_MarkPrice_Converges
  ✓ 持续更新后标记价收敛到最新价

Test_MarkPrice_ManipulationProtection
  ✓ 闪崩到54000时标记价仍保持在59000+以上
  ✓ 防止价格操纵导致不公平强平
```

---

## Phase 3C 测试：保险基金

### insurance/fund_test.go
```
Test_Fund_InitialBalance
  ✓ 初始余额正确

Test_Fund_Contribute
  ✓ 贡献后余额增加
  ✓ 负数贡献被忽略

Test_Fund_Cover_FullyCovered
  ✓ 基金足够时完全覆盖亏损
  ✓ 不需要 ADL

Test_Fund_Cover_Partial_NeedsADL
  ✓ 基金不足时部分覆盖
  ✓ 返回 needADL=true
  ✓ 基金余额归零

Test_CalcLiquidationSurplus_LongSurplus
  ✓ 多仓强平有盈余: (54300-54000)*qty > 0

Test_CalcLiquidationSurplus_ShortSurplus
  ✓ 空仓强平有盈余

Test_CalcLiquidationSurplus_Deficit
  ✓ 穿仓产生亏损: surplus < 0

Test_ProcessLiquidation_Surplus
  ✓ 盈余入保险基金

Test_ProcessLiquidation_Deficit_Covered
  ✓ 亏损由保险基金覆盖

Test_ProcessLiquidation_Fund_Depleted
  ✓ 保险基金耗尽触发 ADL
```

---

## Phase 3D 测试：ADL 自动减仓

### adl/engine_test.go
```
Test_RankPositions_SortByProfitability
  ✓ 最赚钱的仓位排在最前

Test_RankPositions_OnlyProfitable
  ✓ 只有盈利仓位参与 ADL 排序

Test_RankPositions_FilterBySide
  ✓ 只选择目标方向的仓位

Test_CalcADLQuantity
  ✓ deficit=600, price=60000 → qty=0.01

Test_ExecuteADL_FullyClosed
  ✓ 仓位数量不够覆盖时全部平仓

Test_ExecuteADL_PartiallyClosed
  ✓ 仓位足够时只减仓部分

Test_GetADLIndicator
  ✓ ROI 50% → 1灯
  ✓ ROI 150% → 2灯
  ✓ ROI 250% → 3灯
  ✓ ROI 350% → 4灯
  ✓ ROI 450% → 5灯
```

---

## Phase 3E 测试：Maker/Taker 费率

### fee/calculator_test.go
```
Test_GetTier_Default
  ✓ 零交易量返回普通用户等级

Test_GetTier_VIP1
  ✓ 交易量10万返回VIP1

Test_GetTier_VIP3
  ✓ 交易量500万返回VIP3

Test_GetTier_MarketMaker
  ✓ 交易量5000万返回做市商
  ✓ 做市商 Maker 费率为负 (返佣)

Test_CalcFee_Taker
  ✓ 默认 Taker 费率 0.04%: 10000U → 4U

Test_CalcFee_Maker
  ✓ 默认 Maker 费率 0.02%: 10000U → 2U

Test_CalcFee_MakerRebate
  ✓ 做市商 Maker 费率为负 → 负手续费 (返佣)

Test_CalcFee_VIPDiscount
  ✓ VIP 费率低于普通用户
```

---

## Phase 5 测试：API 集成 (E2E)

### 5.1 完整交易流程

#### e2e_trade_test.go
```
Test_E2E_MarketOrder_FullCycle
  1. 登录获取 token
  2. 查询余额 = 10000
  3. 下市价多单: margin=100, leverage=10
  4. 查询持仓: 1个持仓，方向=多
  5. 查询余额: 10000 - 100 - fee
  6. 平仓
  7. 查询持仓: 0
  8. 查询余额: 恢复 (有盈亏)
  9. 查询历史订单: 2条 (开仓+平仓)
  10. 查询成交记录: 2条

Test_E2E_LimitOrder_TriggerAndFill
  1. 下限价多单，价格低于当前价
  2. 查询挂单: 1个
  3. 注入价格触发
  4. 查询挂单: 0
  5. 查询持仓: 1个

Test_E2E_Liquidation_Flow
  1. 高杠杆(100x)开多仓
  2. 注入价格下跌触发强平
  3. 持仓状态=强平
  4. 保证金全部亏损

Test_E2E_ForceTakeProfit_Flow
  1. 10x杠杆开多仓
  2. 注入价格大幅上涨触发强盈
  3. 持仓状态=强盈
  4. 盈利入账

Test_E2E_FundingRate_Settlement
  1. 开多仓
  2. 触发资金费率结算
  3. 查询资金费记录
  4. 余额变化正确
```

### 5.2 教学流程

#### e2e_tutorial_test.go
```
Test_E2E_Tutorial_FirstOrder
  1. 新用户首次下单
  2. 响应包含 tutorial 字段
  3. 查询进度: 1/13

Test_E2E_Tutorial_ProgressTracking
  1. 完成多个操作触发不同知识点
  2. 查询进度递增
  3. 已学过的不再返回
```

---

## 测试基础设施

### testutil 包

```go
// testutil/db.go — 集成测试用真实 PG
func SetupTestDB(t *testing.T) *sql.DB          // dockertest 起 PG
func TeardownTestDB(t *testing.T, db *sql.DB)   // 清理

// testutil/price.go — 模拟价格注入
func NewMockPriceCache() *cache.PriceCache      // 可控价格
func InjectPrice(pc *cache.PriceCache, price string)

// testutil/fixtures.go — 测试数据工厂
func NewTestUser(db *sql.DB) (*model.User, *model.Account)
func NewTestPosition(db *sql.DB, userID int64, opts ...PositionOption) *model.Position
```

### 运行方式

```bash
# 全部单元测试 (无需外部依赖)
make test-unit

# 集成测试 (需要 Docker，自动起 PG)
make test-integration

# E2E 测试
make test-e2e

# 全部测试 + 覆盖率
make test-all

# 只跑某个包
go test ./internal/engine/position/... -v
go test ./internal/engine/liquidation/... -v
```

### Makefile 目标

```makefile
test-unit:
	go test ./internal/cache/... ./internal/engine/position/... \
	  ./internal/engine/trading/... ./internal/engine/liquidation/... \
	  ./internal/engine/funding/... ./internal/tutorial/... \
	  ./pkg/jwt/... -v -count=1

test-integration:
	go test ./internal/model/... ./internal/engine/pricefeed/... \
	  -tags=integration -v -count=1

test-e2e:
	go test ./test/e2e/... -tags=e2e -v -count=1

test-all:
	go test ./... -v -count=1 -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
```

---

## 覆盖率目标

| 包 | 目标覆盖率 | 说明 |
|----|-----------|------|
| `internal/engine/position/` | **95%+** | PnL 计算是核心 |
| `internal/engine/liquidation/` | **95%+** | 强平/强盈不容错 |
| `internal/engine/trading/` | **90%+** | 订单处理 |
| `internal/engine/funding/` | **90%+** | 资金费率 |
| `internal/cache/` | **90%+** | 缓存正确性 |
| `internal/tutorial/` | **85%+** | 教学逻辑 |
| `pkg/jwt/` | **90%+** | 认证安全 |
| `internal/engine/markprice/` | **95%+** | 标记价格防操纵 |
| `internal/engine/insurance/` | **95%+** | 保险基金不容错 |
| `internal/engine/adl/` | **90%+** | ADL 排序和执行 |
| `internal/engine/fee/` | **90%+** | 费率计算 |
| `internal/handler/` | **70%+** | HTTP 层薄，逻辑在 logic |
| **整体** | **85%+** | |

---

## TDD 执行节奏

每个 Phase 的开发流程：

```
1. 写测试文件 (Red)
   - 定义接口/类型签名
   - 编写所有测试用例
   - 运行测试 → 全部 FAIL (编译错误或断言失败)

2. 实现代码 (Green)
   - 最小实现让测试通过
   - 不追求完美，先让绿灯亮

3. 重构 (Refactor)
   - 消除重复
   - 优化命名
   - 确保测试仍然全部通过

4. 提交
   - 每个 "Red→Green→Refactor" 循环一个 commit
   - commit message 标注 [TDD] 前缀
```
