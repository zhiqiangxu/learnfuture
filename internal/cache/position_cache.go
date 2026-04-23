package cache

import (
	"sync"
	"sync/atomic"
)

// Position status in cache
const (
	PosStateActive      = 0 // 正常
	PosStateLiquidating = 1 // 强平中: 已接管，等待订单簿成交
	PosStateForceTPing  = 2 // 强盈中: 已接管，等待订单簿成交
	PosStateClosing     = 3 // 平仓中: 用户发起平仓，等待订单簿成交
)

type CachedPosition struct {
	ID            int64
	UserID        int64
	Side          int
	MarginMode    int // 1=逐仓(isolated), 2=全仓(cross)
	Leverage      int
	EntryPrice    string
	Quantity      string
	Margin        string
	LiqPrice      string
	ForceTpPrice  string
	TakeProfit    string
	StopLoss      string
	FundingPnl    string // 累计资金费盈亏，funding结算时更新
	State         atomic.Int32
}

type PositionCache struct {
	positions sync.Map // key: positionID (int64), value: *CachedPosition
	userIndex sync.Map // key: userID (int64), value: *sync.Map[positionID]struct{}
}

func NewPositionCache() *PositionCache {
	return &PositionCache{}
}

func (c *PositionCache) Add(pos *CachedPosition) {
	c.positions.Store(pos.ID, pos)

	actual, _ := c.userIndex.LoadOrStore(pos.UserID, &sync.Map{})
	posMap := actual.(*sync.Map)
	posMap.Store(pos.ID, struct{}{})
}

func (c *PositionCache) Remove(positionID int64) {
	val, ok := c.positions.LoadAndDelete(positionID)
	if !ok {
		return
	}
	pos := val.(*CachedPosition)

	if userPosMap, ok := c.userIndex.Load(pos.UserID); ok {
		posMap := userPosMap.(*sync.Map)
		posMap.Delete(positionID)
	}
}

func (c *PositionCache) Get(positionID int64) (*CachedPosition, bool) {
	val, ok := c.positions.Load(positionID)
	if !ok {
		return nil, false
	}
	return val.(*CachedPosition), true
}

func (c *PositionCache) GetByUser(userID int64) []*CachedPosition {
	var result []*CachedPosition
	userPosMap, ok := c.userIndex.Load(userID)
	if !ok {
		return result
	}
	posMap := userPosMap.(*sync.Map)
	posMap.Range(func(key, _ interface{}) bool {
		posID := key.(int64)
		if val, ok := c.positions.Load(posID); ok {
			result = append(result, val.(*CachedPosition))
		}
		return true
	})
	return result
}

// FindByUserSideSymbol finds an existing active position for the same user+side.
// Used for position merging (#2): same direction orders merge into one position.
func (c *PositionCache) FindByUserSide(userID int64, side int) *CachedPosition {
	positions := c.GetByUser(userID)
	for _, p := range positions {
		if p.Side == side && p.State.Load() == PosStateActive {
			return p
		}
	}
	return nil
}

func (c *PositionCache) GetAll() []*CachedPosition {
	var result []*CachedPosition
	c.positions.Range(func(_, val interface{}) bool {
		p := val.(*CachedPosition)
		if p.State.Load() == PosStateActive {
			result = append(result, p)
		}
		return true
	})
	return result
}

func (c *PositionCache) Update(positionID int64, fn func(pos *CachedPosition)) {
	val, ok := c.positions.Load(positionID)
	if !ok {
		return
	}
	fn(val.(*CachedPosition))
}

// TrySetState atomically transitions a position to a new state.
// Only succeeds if the position is currently Active.
// Returns true if successfully marked (first caller wins).
// Prevents double-close race condition.
func (c *PositionCache) TrySetState(positionID int64, newState int32) bool {
	val, ok := c.positions.Load(positionID)
	if !ok {
		return false
	}
	pos := val.(*CachedPosition)
	return pos.State.CompareAndSwap(PosStateActive, newState)
}
