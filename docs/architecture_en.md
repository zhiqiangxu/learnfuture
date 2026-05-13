# Perpetual Futures Simulation Learning Platform - Architecture Design

## 1. System Overview

A WeChat Mini Program-based perpetual futures simulation platform built for learning purposes. After logging in via WeChat, users receive simulated USDT and can go Long/Short, experiencing the complete perpetual futures trading process. **Core objective: teach users the complete working principles of perpetual futures through hands-on trading**.

**Tech Stack:**
- Backend: Go + go-zero (monolithic service)
- Database: PostgreSQL (sole persistence layer)
- Cache: Go in-memory (no Redis)
- Frontend: WeChat Mini Program (native)
- Trading pair: BTC/USDT only

---

## 2. System Architecture Diagram

```
┌─────────────────────────────────────────────────────────┐
│              WeChat Mini Program                         │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌──────┐  │
│  │Market/  │ │ Trade  │ │Position│ │History │ │Profile│  │
│  │Learning │ │        │ │        │ │        │ │      │  │
│  └───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘ └──┬───┘  │
│      └──────────┴──────────┴──────────┴─────────┘       │
│         WebSocket (real-time price/PnL)  +  HTTP REST API│
└──────────┬──────────────────────────────────┬────────────┘
           │ wss://                            │ https://
┌──────────▼──────────────────────────────────▼────────────┐
│                    Nginx (Reverse Proxy + SSL)            │
└──────────┬──────────────────────────────────┬────────────┘
┌──────────▼──────────────────────────────────▼────────────┐
│                Go Backend (Monolithic, go-zero)           │
│                                                           │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────┐  │
│  │ HTTP API     │  │ WS Hub       │  │ Price Feed WS  │  │
│  │ (REST)       │  │ (Push to     │  │ Client         │  │
│  │              │  │  clients)    │  │ (btcusdt@trade)│  │
│  └──────┬───────┘  └──────┬───────┘  └───────┬────────┘  │
│  ┌──────▼─────────────────▼──────────────────▼────────┐  │
│  │                 Core Engine Layer                    │  │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────────┐  │  │
│  │  │ Price Mgmt │ │ Trading    │ │ Position Mgmt  │  │  │
│  │  │ + K-line   │ │ Engine     │ │ PnL/Margin     │  │  │
│  │  │ Aggregation│ │ Market/    │ │                │  │  │
│  │  │            │ │ Limit      │ │                │  │  │
│  │  └────────────┘ └────────────┘ └────────────────┘  │  │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────────┐  │  │
│  │  │ Liquidation│ │ Funding    │ │ Teaching       │  │  │
│  │  │ / Force TP │ │ Rate       │ │ Engine         │  │  │
│  │  │ Engine     │ │ 8h Settle  │ │ Knowledge +    │  │  │
│  │  │            │ │            │ │ Hints          │  │  │
│  │  └────────────┘ └────────────┘ └────────────────┘  │  │
│  └────────────────────────────────────────────────────┘  │
│                                                           │
│  ┌────────────────────────────────────────────────────┐  │
│  │          In-Memory Cache Layer (replaces Redis)     │  │
│  │  • Latest price (atomic)    • K-line current bar   │  │
│  │                               (mutex)              │  │
│  │  • Active positions         • Pending orders       │  │
│  │    (sync.Map)                 (mutex+slice)        │  │
│  │  • WS connection pool       • JWT: stateless,      │  │
│  │    (sync.Map)                 no cache needed      │  │
│  │  * All data also persisted to PG; restored from    │  │
│  │    PG on restart                                   │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────────────────┬───────────────────────────────┘
                    ┌──────▼──────┐
                    │ PostgreSQL  │
                    │ (sole       │
                    │ persistence)│
                    └─────────────┘
```

---

## 3. In-Memory Cache Design — Restart Safety

| In-Memory Data | Go Implementation | Recovery on Restart | Impact of Loss |
|---------|---------|-------------|---------|
| BTC latest price | `atomic.Value` | Recovered <1s after WS reconnection | None |
| K-line current unclosed bar | `sync.Mutex` + struct | Lose current incomplete single bar | Minimal |
| Active positions list | `sync.Map` | Load from PG at startup where `status=1` | No loss |
| Pending orders (limit) | `sync.Mutex` + sorted slice | Load from PG at startup where `status=1` | No loss |
| User WS connections | `sync.Map` | Clients auto-reconnect | No loss |
| JWT token | Stateless (signature verification) | No recovery needed | None |

**Core principle: In-memory serves only as a cache acceleration layer; all business data is persisted to PostgreSQL in real time. Restart = PG recovery + WS reconnection.**

---

## 4. Teaching System Design (Core Differentiator)

### 4.1 Design Philosophy

**Integrate teaching into every step of trading** — users "learn" while "doing." Not a separate tutorial page.

### 4.2 Teaching Trigger Mechanism — Context-Aware Knowledge Cards

| Trigger Moment | Knowledge ID | Example Content |
|---------|---------|---------|
| First order | `perpetual_intro` | "Perpetual futures have no expiration date and can be held indefinitely. They anchor to spot price through the funding rate mechanism" |
| Selecting leverage | `leverage_explained` | "10x leverage means controlling a 1000U position with 100U margin. Leverage amplifies both gains and losses" |
| Going Long/Short | `long_short` | "Long = bullish, profit when price rises; Short = bearish, profit when price falls" |
| Holding a position | `unrealized_pnl` | "Unrealized PnL = (current price - entry price) x quantity x direction. It remains floating until the position is closed" |
| Setting TP/SL | `tp_sl_risk` | "Take Profit and Stop Loss are preset auto-close prices, a core risk management tool" |
| Funding rate settlement | `funding_rate` | "Funding rate settles every 8h to anchor contract price to spot. When rate > 0, longs pay shorts" |
| Approaching liquidation | `liquidation` | "When margin ratio falls below 0.5%, liquidation is triggered. Higher leverage means closer to liquidation" |
| After liquidation | `post_liquidation` | "Position was liquidated at {liq_price}, losing all margin of {margin}U" |
| After force take-profit | `force_tp_adl` | "Your position's profit triggered the counterparty LP's risk limit and was force-closed" |
| After closing position | `realized_pnl` | "This trade PnL = {pnl}U, fee = {fee}U, funding cost = {funding}U" |
| Market order | `market_order` | "Market orders execute immediately at current market price, guaranteeing execution but not price" |
| Limit order | `limit_order` | "Limit orders only trigger when price reaches the specified level, potentially getting a better execution price" |
| Trading fee | `trading_fee` | "Fee = position value x rate (0.04%), charged once each for opening and closing" |

### 4.3 API Response with Teaching Fields

```json
{
  "code": 0,
  "data": { ... },
  "tutorial": {
    "id": "leverage_explained",
    "title": "What is Leverage?",
    "content": "10x leverage means...",
    "formula": "Margin = Position Value / Leverage Multiplier",
    "example": "100U x 10x = Control a 1000U position",
    "show_once": true
  }
}
```

### 4.4 Learning Progress Tracking (13 Knowledge Points)

```
Perpetual Futures Learning Progress: 7/13 ██████░░░░░ 54%
✅ What are Perpetual Futures  ✅ Leverage & Margin
✅ Long/Short Mechanism        ✅ Market/Limit Orders
✅ Unrealized PnL              ✅ Fee Calculation
✅ Take Profit/Stop Loss       ⬜ Funding Rate
⬜ Liquidation                 ⬜ Force Take-Profit & ADL
⬜ Mark Price                  ⬜ Position Management Tips
⬜ Risk Control
```

---

## 5. Liquidation & Force Take-Profit Engine

### 5.1 Liquidation — Loss Direction

```
Liquidation Price (Long) = Entry Price x (1 - 1/Leverage + 0.005)
Liquidation Price (Short) = Entry Price x (1 + 1/Leverage - 0.005)
Margin Ratio = (Margin + Unrealized PnL) / (Quantity x Current Price)
When Margin Ratio <= 0.5% → Trigger Liquidation
```

### 5.2 Force Take-Profit (Forced Take-Profit / ADL) — Profit Direction

```
Force Take-Profit trigger: Unrealized ROI >= 500% (position value reaches 6x margin)
Force TP Price (Long) = Entry Price x (1 + 5/Leverage)
Force TP Price (Short) = Entry Price x (1 - 5/Leverage)
```

---

## 6. API Design

### Authentication
| Method | Path | Description |
|------|------|------|
| POST | `/api/v1/auth/wx-login` | WeChat login |
| POST | `/api/v1/auth/refresh` | Refresh token |

### Account
| Method | Path | Description |
|------|------|------|
| GET | `/api/v1/account/info` | Balance, total PnL, learning progress |
| POST | `/api/v1/account/reset` | Reset balance to 10000U |

### Trading
| Method | Path | Description |
|------|------|------|
| POST | `/api/v1/order/place` | Place order |
| POST | `/api/v1/order/cancel` | Cancel limit order |
| POST | `/api/v1/position/close` | Close position |
| POST | `/api/v1/position/update-tpsl` | Modify Take Profit / Stop Loss |
| GET | `/api/v1/position/list` | Current positions |
| GET | `/api/v1/order/pending` | Pending orders list |

### Market
| Method | Path | Description |
|------|------|------|
| GET | `/api/v1/market/price` | BTC/USDT current price |
| GET | `/api/v1/market/klines` | K-line data |
| GET | `/api/v1/market/funding-rate` | Current funding rate |

### History
| Method | Path | Description |
|------|------|------|
| GET | `/api/v1/history/orders` | Historical orders |
| GET | `/api/v1/history/trades` | Trade records |
| GET | `/api/v1/history/funding` | Funding rate settlement records |
| GET | `/api/v1/history/pnl` | PnL summary |

### Leaderboard
| Method | Path | Description |
|------|------|------|
| GET | `/api/v1/leaderboard/pnl` | Total PnL ranking |
| GET | `/api/v1/leaderboard/roi` | ROI ranking |

### Teaching
| Method | Path | Description |
|------|------|------|
| GET | `/api/v1/tutorial/progress` | Learning progress |
| POST | `/api/v1/tutorial/complete` | Mark knowledge point as learned |

### WebSocket
```
wss://domain/ws?token=<jwt>

Push message types:
- ticker: Real-time market data
- position_pnl: Position PnL update (on each price change)
- order_filled: Order filled (notify maker when limit order is taken)
- liquidated: Liquidation notification
- force_tp: Force Take-Profit notification
- adl: ADL (Auto-Deleveraging) notification
- funding_settled: Funding rate settlement
```

---

## 7. Database Design (PostgreSQL)

### Table Structure

| Table | Description | Key Fields |
|------|------|---------|
| `users` | Users | openid, nickname, avatar_url |
| `accounts` | Accounts | balance, frozen, total_pnl, reset_count |
| `positions` | Positions | side, leverage, entry_price, quantity, margin, liq_price, force_tp_price, status |
| `orders` | Orders | side, order_type, leverage, price, quantity, margin_cost, status |
| `trades` | Trades | price, quantity, fee, realized_pnl, is_close, close_reason |
| `funding_rates` | Funding rate history | rate, settle_time |
| `funding_settlements` | Funding settlement records | position_id, rate, position_value, payment |
| `klines` | K-lines | interval, open_time, OHLCV |
| `tutorial_progress` | Learning progress | user_id, topic_id |

### Key Indexes
- `idx_positions_user_status`: positions(user_id, status)
- `idx_orders_user_status`: orders(user_id, status)
- `idx_trades_user`: trades(user_id, created_at DESC)
- `idx_klines_query`: klines(interval, open_time DESC)

---

## 8. Matching Engine Detailed Design

### 8.1 Matching Model

This platform uses a **user vs system** counterparty model, where the system acts as a virtual liquidity provider (LP) with unlimited funds. Although true two-sided order book matching is not needed, it fully simulates a real exchange's matching, clearing, and settlement process.

```
                    ┌────────────────────────────┐
                    │      Matching Engine        │
                    │   (trading/engine.go)       │
                    └────────┬────────────────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
    ┌────────────────┐  ┌────────────┐  ┌────────────┐
    │ Market Order   │  │ Limit Order│  │ TP/SL      │
    │ Matching       │  │ Monitoring │  │ Trigger    │
    │ Execute at     │  │ Execute at │  │ Auto-close │
    │ latest price   │  │ limit when │  │ when price │
    │ immediately    │  │ price      │  │ reached    │
    │                │  │ crosses    │  │            │
    └─────┬──────────┘  └─────┬──────┘  └─────┬──────┘
          │                   │               │
          ▼                   ▼               ▼
    ┌──────────────────────────────────────────────┐
    │          Clearing & Settlement Engine         │
    │  1. Fund transfer (margin deduct/freeze/     │
    │     return)                                   │
    │  2. Position lifecycle management             │
    │  3. Fee calculation & deduction               │
    │  4. PnL settlement (realized/unrealized)      │
    │  5. Trade record generation                   │
    └──────────────────────────────────────────────┘
```

### 8.2 Order-First Architecture

One user action = one Order; matching produces N Trades (one per price level).
Orders are created at the caller side (EventCreateOrder); Trades are created in UpdatePosition (EventTrade).
Order IDs are allocated uniformly by book.NextOrderID(), shared between in-memory and DB.

```
Data flow:
  Caller → Create Order (ID=book.NextOrderID()) → EventCreateOrder → settler writes DB
  Caller → Order Book matching → N orderbook.Trade entries
  ProcessTrades → for each trade, call UpdatePosition(orderID)
    → handleOpen/Increase/Reduce → create model.Trade(OrderID=orderID) → EventTrade → settler writes DB

ID consistency:
  orderbook.Order.ID = model.Order.ID = book.NextOrderID()
  Initialized from DB MAX(id) at startup, ensuring no conflicts after restart

System users (UserID<=0, Market Maker/Liquidation Engine):
  In-memory positions maintained normally, but DB trade record writes are skipped
```

### 8.3 Market Order Matching Flow

```
1. [Validate] side in {1,-1}, leverage in [1,125], margin >= 1 USDT
2. [Get Price] currentPrice = PriceCache.GetPrice() (must be non-zero)
3. [Pre-check] balance >= margin + fee (optimistic check; actual deduction in UpdatePosition)
4. [Create Order] orderID = book.NextOrderID(), submitOrder → EventCreateOrder
5. [Match] PlaceMarket → N orderbook.Trade entries
6. [Position Update] ProcessTrades(trades, userID, side, leverage, orderID)
   - 1st trade → handleOpen: create position + trade(orderID)
   - 2nd~Nth → handleIncrease: weighted average merge + trade(orderID)
   - Counterparty (maker): trade references maker's orderbook order ID
7. [Return] Position info + teaching card, navigate to position page
```

### 8.4 Limit Order Matching Flow

```
Pending phase:
1. [Validate] Same as market order + limitPrice > 0 + price deviation <= 10%
2. [Freeze] memAccounts.Freeze(totalCost)
3. [Create Order] orderID = obOrder.ID, submitOrder(status=pending) → EventCreateOrder
4. [Place in Order Book] book.PlaceLimit
   - Price crosses → immediate execution (taker path)
   - No cross → resting in order book, added to orderCache

Execution phase (automatic matching by order book, not by Monitor polling):
  Market Maker refreshes quotes every 500ms → new order enters order book → price crosses → matching
  → onTrades callback → ProcessTrades → maker's trade references existing orderID
  → WS pushes order_filled notification to maker
```

### 8.5 Close Position Clearing & Settlement Flow

```
1. [Validate] Position exists && status=active && belongs to current user
2. [CAS] TrySetState(Closing) — prevent concurrent duplicate closes
3. [Create Order] closeOrderID = book.NextOrderID(), submitOrder → EventCreateOrder
4. [Match] PlaceMarket(reverse direction) → N trades
5. [Position Update] ProcessTrades → handleReduce
   - Calculate PnL = (closePrice - entryPrice) x qty x side - fee + fundingPnl
   - In-memory: ReturnMarginWithPnl (return margin + PnL)
   - Settler: EventTrade (trade + position status + balance return)
   - Liquidation: use TakeOver (bypasses order book), user loses all margin
6. [WS Push] Push different messages based on close_reason
```

### 8.6 Margin Account Model

```
┌──────────────────────────────────────┐
│         Account (per user)           │
│                                      │
│  balance ──→ Available balance       │
│              (for orders/withdrawal) │
│  frozen  ──→ Frozen amount           │
│              (limit order hold)      │
│  total_pnl → Cumulative realized PnL│
│                                      │
│  Available = balance - frozen        │
│                                      │
│  Fund flow:                          │
│  ┌─────────┐     ┌─────────┐        │
│  │ balance  │────→│ frozen  │Place   │
│  │         │←────│         │limit   │
│  └────┬────┘     └─────────┘order/  │
│       │                     cancel/ │
│       │                     fill    │
│       │ Market open: -margin-fee    │
│       │ Close: +margin+netPnl       │
│       │ Liquidation: 0 (margin      │
│       │   already deducted)         │
│       │ Funding: ±payment           │
│       ▼                             │
│  total_pnl: accumulate each         │
│    realized PnL                     │
└──────────────────────────────────────┘
```

### 8.6 Risk Control Engine (Monitor)

The Monitor performs checks based on position snapshots on each price update (priority from high to low).
Limit orders are matched automatically by the order book (triggered when Market Maker quote refresh causes price crossing), not by Monitor polling.
The execution layer ensures idempotency through CAS (TrySetState) to prevent concurrent duplicate operations.

```
OnPriceUpdate(lastPrice):
  1. Snapshot: allPositions = positionCache.GetAll()
  2. Iterate all active positions:
     a. Liquidation check (using markPrice): margin ratio <= 0.5% → TakeOver (bypasses order book)
     b. Force Take-Profit check: ROI >= 500% → ClosePositionInternal (market order via order book)
     c. Take Profit check: lastPrice crosses TP price → ClosePositionInternal
     d. Stop Loss check: lastPrice crosses SL price → ClosePositionInternal
     e. Push PnL: calculate and push real-time unrealized PnL / margin ratio / ROI / ADL indicator
  3. Liquidation Engine: OnTick attempts to dispose of taken-over positions (IOC limit orders)
```

### 8.7 PnL Calculation Formulas

```
Unrealized PnL = (Current Price - Entry Price) x Quantity x Direction
ROI = Unrealized PnL / Margin x 100%
Realized PnL = (Close Price - Entry Price) x Quantity x Direction - Close Fee
Margin Ratio = (Margin + Unrealized PnL) / (Quantity x Current Price)
Net PnL = Realized PnL + Cumulative Funding
```

### 8.8 Funding Rate Settlement

```
Settlement times: Every 8 hours (00:00/08:00/16:00 UTC)
Rate source: Real rates fetched via external REST API

Settlement flow:
1. Fetch latest rate → write to funding_rates table
2. Iterate all active positions:
   - positionValue = quantity x currentPrice
   - payment = positionValue x rate x (-side)
   - rate > 0: longs pay (payment is negative), shorts receive (payment is positive)
   - rate < 0: vice versa
3. Update positions.funding_pnl += payment
4. Update accounts.balance += payment
5. Write to funding_settlements table
6. WS push funding_settled
```

---

## 8A. Mark Price Engine

### Why is Mark Price Needed?

In real exchanges, if the latest trade price is used for liquidation calculations, an attacker could trigger mass liquidations by executing a single abnormally low-priced trade. Mark Price prevents this manipulation through multiple layers of protection.

### Dual Price Source Design (Mirroring Real Exchanges)

Even with only one price source (e.g., Binance), two different types of prices can be obtained to produce a real basis:

```
                    ┌─ Binance ─────────────────────┐
                    │                                │
    Spot API ───────┤  api.binance.com               │
    (REST, every 5s)│  /api/v3/ticker/price          │──→ Index Price
                    │  BTCUSDT spot latest price      │     = Spot price, hard to manipulate
                    │                                │       (no leverage)
                    │                                │
    Futures WS ────┤  fstream.binance.com            │
    (real-time,     │  /ws/btcusdt@trade             │──→ Last Price
     every tick)    │  BTCUSDT perpetual futures trade│     = Futures price, includes
                    │                                │       leverage premium
                    └────────────────────────────────┘

    Spot vs Futures naturally have a basis:
    Spot:    60000 (no leverage, stable)
    Futures: 60120 (leverage premium, 0.2% premium)
    Basis = (60120-60000)/60000 = +0.2%
```

### Calculation Formulas

```
Index Price = Spot price (fetched via REST API every 5s)
Last Price = Futures price (pushed via WS in real-time)
Basis = (Futures Price - Spot Price) / Spot Price
EMA Basis = α x Current Basis + (1-α) x Previous EMA Basis   (α=0.1)
Mark Price = Index Price x (1 + EMA Basis)
```

### Usage Scenarios

| Scenario | Price Used | Reason |
|------|---------|------|
| Opening/Closing execution | Futures Last Price | Reflects real supply & demand in futures market |
| Liquidation judgment | **Mark Price** | Based on spot, prevents futures price manipulation |
| Unrealized PnL display | Futures Last Price | Reflects real-time market conditions |
| Funding rate calculation | Mark Price | Prevents manipulation |

### Anti-Manipulation Effectiveness (Test Verification)

```
Scenario: Someone creates a flash crash in the futures market (54000)
Spot: Still 60000 (no leverage in spot, flash crash has no effect)
Mark Price: 59400 (follows spot, does not follow futures flash crash)
Result: Users with liquidation price 55000 are NOT falsely liquidated ✓
```

### Implementation

```
engine/markprice/engine.go
- UpdateLastPrice(price):  Called on every futures WS trade
- UpdateIndexPrice(price): Periodically updated via REST spot price
- GetMarkPrice():          Get mark price (used for liquidation judgment)
- Start():                 Start periodic spot price fetching (every 5s)
- GetBasis():              Get current basis (for teaching display)
```

---

## 8B. Insurance Fund

### Mechanism Description

The Insurance Fund is an exchange's risk buffer pool, used to absorb bankruptcy losses from liquidations.

```
Fund flow during liquidation:

Normal liquidation (with remaining balance):
  Liquidation price > Bankruptcy price → Remaining margin → Insurance Fund (surplus)

Bankruptcy liquidation (price gap):
  Liquidation price < Bankruptcy price → Loss exceeds margin → Insurance Fund covers (deficit)

Insurance Fund depleted:
  → Triggers ADL (Auto-Deleveraging)
```

### Key Formulas

```
Bankruptcy Price (Long) = Entry Price x (1 - 1/Leverage)     // Price where margin reaches zero
Liquidation Price (Long) = Entry Price x (1 - 1/Leverage + 0.5%)  // Early liquidation price

Surplus = (Liquidation Price - Bankruptcy Price) x Quantity x Direction
  > 0: Insurance Fund receives surplus
  < 0: Bankruptcy, Insurance Fund covers
```

### Implementation

```
engine/insurance/fund.go
- Contribute(): Deposit liquidation surplus into fund
- Cover(): Fund covers losses during bankruptcy
- Initial capital: 1,000,000 virtual USDT

Liquidation clearing handled by trading/liquidation_engine.go's TakeOver + OnTick
```

---

## 8C. ADL Auto-Deleveraging System

### Trigger Conditions

When the Insurance Fund cannot cover bankruptcy losses, the system must forcibly deleverage counterparty positions to compensate for the deficit.

### ADL Priority Ranking

```
Counterparty positions ranked by the following formula (highest to lowest):
ADL Score = ROI x Leverage Multiplier

Most profitable + highest leverage = first to be ADL'd
```

### ADL Indicator Lights

```
ADL risk indicator visible to users (1-5 lights):
  5 lights: ROI > 400% — Very high risk, very likely to be ADL'd
  4 lights: ROI > 300%
  3 lights: ROI > 200%
  2 lights: ROI > 100%
  1 light:  ROI <= 100% — Low risk
```

### Implementation

```
engine/adl/engine.go
- RankPositions(): Rank counterparty positions by ADL priority
- ExecuteADL(): Deleverage from highest priority until deficit is covered
- GetADLIndicator(): Return risk level from 1-5
```

---

## 8D. Maker/Taker Fee Structure

### Fee Tiers

| Tier | 30-Day Volume | Maker Fee | Taker Fee | Note |
|------|-----------|-----------|-----------|------|
| Regular | < 100K U | 0.02% | 0.04% | Default |
| VIP1 | >= 100K U | 0.018% | 0.035% | |
| VIP2 | >= 500K U | 0.015% | 0.03% | |
| VIP3 | >= 2M U | 0.01% | 0.025% | |
| VIP4 | >= 10M U | 0.005% | 0.02% | |
| Market Maker | >= 50M U | **-0.005%** | 0.015% | Maker rebate! |

### Maker vs Taker

- **Taker**: Market orders, "consume" order book liquidity → higher fee
- **Maker**: Limit orders, "provide" order book liquidity → lower fee (or even rebate)

### Implementation

```
engine/fee/calculator.go
- GetTier(): Determine fee tier based on 30-day trading volume
- CalcFee(): Calculate fee based on Maker/Taker
- Market Maker rebate: Negative Maker fee, placing orders earns money
```

---

## 8E. T+1 Reconciliation System

### Check Items

| Check | Description | Comparison Precision |
|------|------|---------|
| Balance reconciliation | In-memory balance vs DB balance | 8 decimal places |
| Position reconciliation | In-memory active positions vs DB active positions | Quantity + existence |
| Zero-sum verification | Sum of long quantity == Sum of short quantity | Exact |

### Execution Mechanism

- First run 1 minute after startup, then daily at UTC 04:00
- On discrepancy: log alert + email notification to admin
- Alert only, no automatic repair

### Implementation

```
engine/reconcile/reconcile.go
- checkBalances(): Compare in-memory vs DB balance per user
- checkPositions(): Compare in-memory vs DB quantity per position
- checkZeroSum(): Verify sum of longs == sum of shorts
- sendAlert(): Email notification on discrepancy
```

---

## 9. Price Feed Architecture

```
External WebSocket ──(btcusdt@trade)──> PriceFeedManager
                                         │
                                         ├─> In-memory (atomic latest price)
                                         ├─> K-line Aggregator (in-memory buckets → PG)
                                         ├─> WS Hub → Mini Program clients (200ms batch)
                                         ├─> Trading Engine (limit order/TP/SL trigger check)
                                         ├─> Liquidation Engine (margin ratio check)
                                         └─> Force Take-Profit Engine (ROI check)
```

**Compliance: Frontend does not display price source; API does not contain "binance" in any form.**

---

## 10. Project Directory Structure

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

## 11. K-line Support & Memory Footprint

### K-line Time Intervals
`1m`, `5m`, `15m`, `1h`, `4h`, `1d`

### In-Memory Cache

| Interval | Cached Count | Coverage Duration |
|----------|---------|---------|
| 1m | 1440 bars | 24 hours |
| 5m | 288 bars | 24 hours |
| 15m | 192 bars | 2 days |
| 1h | 168 bars | 7 days |
| 4h | 180 bars | 30 days |
| 1d | 365 bars | 1 year |

### Memory Footprint (Single Trading Pair)

| Data Item | Memory |
|-------|------|
| Latest price | ~100B |
| K-line cache (2633 bars) | ~210 KB |
| Active positions (1000 users x 3) | ~1.5 MB |
| Pending orders (5000 orders) | ~1.5 MB |
| WS connections (1000) | ~6 MB |
| Go runtime | ~20 MB |
| **Total (1000 concurrent)** | **~30 MB** |

---

## 12. Implementation Phases

| Phase | Content | Dependencies |
|-------|------|------|
| **Phase 1** | Infrastructure: project scaffolding, PG table creation, model layer, in-memory cache, price feed WS, HTTP health check | None |
| **Phase 2** | Core Trading: WeChat login + JWT, account management, market orders, position management + PnL, WS Hub | Phase 1 |
| **Phase 3** | Advanced Trading: limit orders, Take Profit / Stop Loss, Liquidation / Force Take-Profit engine, funding rate, K-line aggregation | Phase 2 |
| **Phase 4** | Teaching System: 13 knowledge points, trigger logic, learning progress | Phase 2 |
| **Phase 5** | Mini Program Frontend: all pages + teaching components | Phase 3+4 |
