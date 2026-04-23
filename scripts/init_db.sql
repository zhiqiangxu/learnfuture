-- 永续合约模拟学习平台 - 数据库初始化

-- 用户
CREATE TABLE IF NOT EXISTS users (
    id          BIGSERIAL PRIMARY KEY,
    openid      VARCHAR(64) NOT NULL UNIQUE,
    nickname    VARCHAR(64) NOT NULL DEFAULT '',
    avatar_url  VARCHAR(512) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 账户
CREATE TABLE IF NOT EXISTS accounts (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL UNIQUE REFERENCES users(id),
    balance     DECIMAL(20,8) NOT NULL DEFAULT 10000,
    frozen      DECIMAL(20,8) NOT NULL DEFAULT 0,
    total_pnl   DECIMAL(20,8) NOT NULL DEFAULT 0,
    reset_count INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 持仓
CREATE TABLE IF NOT EXISTS positions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    symbol          VARCHAR(16) NOT NULL DEFAULT 'BTCUSDT',
    side            SMALLINT NOT NULL,           -- 1=多, -1=空
    margin_mode     SMALLINT NOT NULL DEFAULT 1, -- 1=逐仓, 2=全仓
    leverage        INT NOT NULL DEFAULT 10,
    entry_price     DECIMAL(20,8) NOT NULL,
    quantity        DECIMAL(20,8) NOT NULL,      -- BTC数量
    margin          DECIMAL(20,8) NOT NULL,
    liq_price       DECIMAL(20,8) NOT NULL,      -- 强平价
    force_tp_price  DECIMAL(20,8) NOT NULL,      -- 强盈价
    take_profit     DECIMAL(20,8),
    stop_loss       DECIMAL(20,8),
    unrealized_pnl  DECIMAL(20,8) NOT NULL DEFAULT 0,
    funding_pnl     DECIMAL(20,8) NOT NULL DEFAULT 0,
    status          SMALLINT NOT NULL DEFAULT 1, -- 1=持仓, 2=已平, 3=强平, 4=强盈
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_positions_user_status ON positions(user_id, status);

-- 订单
CREATE TABLE IF NOT EXISTS orders (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id),
    symbol       VARCHAR(16) NOT NULL DEFAULT 'BTCUSDT',
    side         SMALLINT NOT NULL,
    order_type   SMALLINT NOT NULL,              -- 1=市价, 2=限价
    leverage     INT NOT NULL DEFAULT 10,
    price        DECIMAL(20,8),                  -- 限价价格
    quantity     DECIMAL(20,8) NOT NULL,
    filled_price DECIMAL(20,8),
    margin_cost  DECIMAL(20,8) NOT NULL,
    take_profit  DECIMAL(20,8),
    stop_loss    DECIMAL(20,8),
    status       SMALLINT NOT NULL DEFAULT 1,    -- 1=挂单, 2=成交, 3=取消
    position_id  BIGINT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_orders_user_status ON orders(user_id, status);

-- 成交记录
CREATE TABLE IF NOT EXISTS trades (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id),
    order_id     BIGINT NOT NULL REFERENCES orders(id),
    position_id  BIGINT REFERENCES positions(id),
    symbol       VARCHAR(16) NOT NULL DEFAULT 'BTCUSDT',
    side         SMALLINT NOT NULL,
    price        DECIMAL(20,8) NOT NULL,
    quantity     DECIMAL(20,8) NOT NULL,
    fee          DECIMAL(20,8) NOT NULL DEFAULT 0,
    realized_pnl DECIMAL(20,8) NOT NULL DEFAULT 0,
    is_close     BOOLEAN NOT NULL DEFAULT FALSE,
    close_reason SMALLINT NOT NULL DEFAULT 0,    -- 0=手动, 1=止盈, 2=止损, 3=强平, 4=强盈
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_trades_user ON trades(user_id, created_at DESC);

-- 资金费率历史
CREATE TABLE IF NOT EXISTS funding_rates (
    id          BIGSERIAL PRIMARY KEY,
    symbol      VARCHAR(16) NOT NULL DEFAULT 'BTCUSDT',
    rate        DECIMAL(20,8) NOT NULL,
    settle_time TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 资金费结算记录
CREATE TABLE IF NOT EXISTS funding_settlements (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id),
    position_id    BIGINT NOT NULL REFERENCES positions(id),
    rate           DECIMAL(20,8) NOT NULL,
    position_value DECIMAL(20,8) NOT NULL,
    payment        DECIMAL(20,8) NOT NULL,
    settle_time    TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- K线 (仅BTC/USDT)
CREATE TABLE IF NOT EXISTS klines (
    id         BIGSERIAL PRIMARY KEY,
    interval   VARCHAR(4) NOT NULL,
    open_time  TIMESTAMPTZ NOT NULL,
    open       DECIMAL(20,8) NOT NULL,
    high       DECIMAL(20,8) NOT NULL,
    low        DECIMAL(20,8) NOT NULL,
    close      DECIMAL(20,8) NOT NULL,
    volume     DECIMAL(20,8) NOT NULL DEFAULT 0,
    close_time TIMESTAMPTZ NOT NULL,
    UNIQUE(interval, open_time)
);
CREATE INDEX IF NOT EXISTS idx_klines_query ON klines(interval, open_time DESC);

-- 学习进度
CREATE TABLE IF NOT EXISTS tutorial_progress (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id),
    topic_id     VARCHAR(32) NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, topic_id)
);
