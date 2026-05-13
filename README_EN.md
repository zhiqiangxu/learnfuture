# LearnFuture - Perpetual Futures Simulation Platform

A learning platform that simulates real exchange architecture for perpetual futures trading. Users trade with virtual funds to experience the complete workflow and learn how perpetual contracts work.

## Architecture

```
Trading Flow:
  Place Order → Risk Check → Orderbook Matching → Trade[] → Position Update (both sides) → Async DB Persist

Risk Monitor (triggered on each price tick):
  Monitor → TP/SL          Market order via orderbook
          → Force TP        Market order via orderbook
          → Liquidation     Bypass orderbook → Liquidation engine takeover
          → ADL             Bypass orderbook → Direct counterparty deleverage

Liquidation Flow:
  ① Mark price triggers liquidation → Position transferred to liquidation engine at bankruptcy price (∑longs ≡ ∑shorts maintained)
  ② Liquidation engine gradually closes via IOC limit orders (max 1% slippage)
  ③ Surplus/deficit goes to insurance fund
  ④ Insurance fund depleted → ADL (deleverage counterparty at bankruptcy price, settlement ≥ entry price)
```

### Core Design

**Order-First Architecture**: One user action (open/close/liquidation/ADL) = one Order, matching produces N Trades. Order is created at call site (`EventCreateOrder`), Trades are created in `UpdatePosition` (`EventTrade`). Order ID assigned by `book.NextOrderID()`, shared between memory and DB (initialized from DB MAX on startup).

**Unified Position Update**: All orders follow the same path: orderbook matching → `ProcessTrades` → `UpdatePosition` for both sides, automatically handling open/increase/reduce/close.

**Zero-Sum System**: Every trade updates both sides simultaneously. Market maker (UserID=0) and liquidation engine (UserID=-1) have real accounts with position tracking. User PnL + counterparty PnL + fees = 0, ∑longs ≡ ∑shorts.

**Liquidation Engine**: Inspired by Vega Protocol and MEXC. Liquidation bypasses the orderbook (avoids failure due to insufficient liquidity), transfers position to the system at bankruptcy price, then the engine gradually closes on the market.

**Price Monitor**: On each price tick, checks TP/SL, liquidation, and force TP based on position snapshot. TP/SL and force TP use market orders via orderbook; liquidation uses TakeOver. Execution layer uses CAS (TrySetState) for idempotency.

**Limit Order Auto-Matching**: After a limit order rests in the orderbook, when any new order enters (e.g., market maker refreshes quotes every 500ms), price crossing triggers automatic matching — no Monitor polling needed.

**ADL Safety**: Settlement price never falls below counterparty's entry price (no loss for counterparty). Only selects counterparties profitable at both market and bankruptcy price, ranked by ROI × leverage.

**T+1 Reconciliation**: Daily automated checks (balance/position/zero-sum), email alert on discrepancy.

### Core Modules

| Module | Description |
|--------|-------------|
| Order Book | Price-time priority matching, market/limit orders |
| Position Engine (UpdatePosition) | Unified open/increase/reduce, symmetric for both sides |
| Market Maker Bot | Tracks external price, auto-quotes for liquidity, callback on fill |
| Mark Price | Spot index + contract basis EMA smoothing, anti-manipulation |
| Liquidation Engine | Takeover at bankruptcy price, IOC disposal, surplus to insurance |
| Force TP | Force take-profit when ROI reaches cap, protects counterparty |
| TP/SL | User-set trigger prices, market order via orderbook, retry on next tick |
| Insurance Fund | Absorbs liquidation losses, funded by liquidation surplus |
| ADL | Auto-deleverage when insurance depleted, ranked by profitability |
| Maker/Taker Fees | 6-tier VIP system, maker rebates |
| Funding Rate | 8h settlement, longs/shorts pay each other, anchors to spot |
| K-line Aggregation | Hierarchical: 1s→5s→15s→1m→5m→15m→1h→4h→1d, continuous pricing |
| Reconciliation | T+1 balance/position/zero-sum check, email alert |
| Tutorial System | 14 topics, context-triggered during trading |

## Tech Stack

- **Backend**: Go + go-zero
- **Database**: PostgreSQL
- **Cache**: Go in-memory (no Redis)
- **Frontend**: WeChat Mini Program + H5 web (single-file)
- **Price Feed**: Gate.io WebSocket + REST

## Quick Start

### Local Development

```bash
# 1. Start PostgreSQL and create tables
createdb learn_future
psql -d learn_future -f scripts/init_db.sql

# 2. Start the server
make run

# 3. Verify
curl http://localhost:8888/health
curl http://localhost:8888/api/v1/market/price
curl http://localhost:8888/api/v1/market/depth?limit=5
```

### Docker Deployment

```bash
cd deploy
docker-compose up -d
```

### Run Tests

```bash
make test          # unit tests
make test-all      # all tests + coverage
```

## API

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/auth/wx-login | WeChat login |
| GET | /api/v1/account/info | Account info |
| POST | /api/v1/account/reset | Reset balance |
| POST | /api/v1/order/place | Place order |
| POST | /api/v1/order/cancel | Cancel pending order |
| POST | /api/v1/position/close | Close position |
| GET | /api/v1/position/list | Position list |
| GET | /api/v1/market/price | Real-time price |
| GET | /api/v1/market/depth | Order book depth |
| GET | /api/v1/market/mark-price | Mark price |
| GET | /api/v1/market/klines | K-line data |
| GET | /api/v1/market/funding-rate | Funding rate |
| GET | /api/v1/market/insurance-fund | Insurance fund |
| WS | /ws?token=xxx | Real-time push |

## Project Structure

```
internal/
  engine/
    orderbook/     Order book (price-time priority)
    trading/       Matching engine + unified position update + liquidation engine
    clearing/      Clearing + settlement
    markprice/     Mark price engine
    insurance/     Insurance fund
    adl/           Auto-deleveraging
    fee/           Maker/Taker fees
    funding/       Funding rate settlement
    pricefeed/     Price feed + K-line aggregation
    marketmaker/   Market maker bot
    position/      PnL / liquidation price calculation
    reconcile/     T+1 reconciliation + email alert
  handler/         HTTP handlers
  logic/           Business logic
  model/           Database models
  cache/           In-memory cache
  tutorial/        Tutorial system
  ws/              WebSocket
miniprogram/       WeChat Mini Program frontend
```

## Disclaimer

This is a simulation trading platform for educational purposes only. Uses virtual funds, no real trading involved.
