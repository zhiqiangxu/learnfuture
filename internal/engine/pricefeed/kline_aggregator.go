package pricefeed

import (
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/model"
)

// Hierarchical K-line aggregation (层级聚合)
//
// Architecture:
//   tick → 1s → 5s → 15s → 1m → 5m → 15m → 1h → 4h → 1d
//
// Only the base level (1s) processes raw ticks.
// Each higher level merges closed bars from its child level.
// Additionally, ticks also update all parent current bars in real-time
// so that GetCurrentBar always reflects the latest price.

type IntervalConfig struct {
	Name     string
	Duration time.Duration
	MaxCache int
	Persist  bool   // persist closed bars to DB
	Parent   string // parent interval name (empty = base level)
}

var Intervals = []IntervalConfig{
	// 秒级: 只缓存不落盘
	{"1s", time.Second, 3600, false, "5s"},
	{"5s", 5 * time.Second, 720, false, "15s"},
	{"15s", 15 * time.Second, 240, false, "1m"},
	// 分钟及以上: 缓存 + 落盘PG
	{"1m", time.Minute, 1440, true, "5m"},
	{"5m", 5 * time.Minute, 288, true, "15m"},
	{"15m", 15 * time.Minute, 192, true, "1h"},
	{"1h", time.Hour, 168, true, "4h"},
	{"4h", 4 * time.Hour, 180, true, "1d"},
	{"1d", 24 * time.Hour, 365, true, ""},
}

type KlineBar struct {
	Interval  string
	OpenTime  time.Time
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
	CloseTime time.Time
}

type KlineCloseCallback func(bar *KlineBar)

type KlineAggregator struct {
	mu      sync.Mutex
	configs map[string]*IntervalConfig
	current map[string]*KlineBar
	cache   map[string][]*KlineBar
	onClose KlineCloseCallback
}

func NewKlineAggregator(onClose KlineCloseCallback) *KlineAggregator {
	configs := make(map[string]*IntervalConfig, len(Intervals))
	for i := range Intervals {
		configs[Intervals[i].Name] = &Intervals[i]
	}
	return &KlineAggregator{
		configs: configs,
		current: make(map[string]*KlineBar),
		cache:   make(map[string][]*KlineBar),
		onClose: onClose,
	}
}

// OnTrade processes a raw tick.
// 1. Updates the base (1s) bar — may trigger close & cascade.
// 2. Also updates all ancestor current bars with the latest price in real-time.
func (a *KlineAggregator) OnTrade(price decimal.Decimal, ts time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Step 1: Update base bar (1s), cascading closes upward
	a.updateBase(price, ts)

	// Step 2: Update all ancestor current bars with latest tick
	// so GetCurrentBar always shows the latest close price
	a.updateAncestors(price, ts)
}

// updateBase handles the base interval (1s). When a 1s bar closes,
// it cascades upward: the closed 1s bar is merged into 5s, etc.
// If multiple periods were skipped (no ticks), fill gaps with flat candles.
func (a *KlineAggregator) updateBase(price decimal.Decimal, ts time.Time) {
	cfg := a.configs["1s"]
	openTime := truncateToInterval(ts, cfg.Duration)
	closeTime := openTime.Add(cfg.Duration)
	bar := a.current["1s"]

	if bar == nil || !bar.OpenTime.Equal(openTime) {
		// Use previous bar's close as the new bar's open for continuity
		openPrice := price
		if bar != nil {
			openPrice = bar.Close
			a.closeAndCascade(bar, cfg)

			// Fill gaps: generate flat candles for skipped periods
			lastClose := bar.Close
			gapStart := bar.OpenTime.Add(cfg.Duration)
			maxFill := 3
			for filled := 0; gapStart.Before(openTime) && filled < maxFill; filled++ {
				gapEnd := gapStart.Add(cfg.Duration)
				flatBar := &KlineBar{
					Interval: "1s", OpenTime: gapStart, CloseTime: gapEnd,
					Open: lastClose, High: lastClose, Low: lastClose, Close: lastClose,
				}
				a.closeAndCascade(flatBar, cfg)
				gapStart = gapEnd
			}
		}
		// Open = previous close, High/Low include both open and current price
		highPrice := decimal.Max(openPrice, price)
		lowPrice := decimal.Min(openPrice, price)
		a.current["1s"] = &KlineBar{
			Interval: "1s", OpenTime: openTime, CloseTime: closeTime,
			Open: openPrice, High: highPrice, Low: lowPrice, Close: price,
		}
	} else {
		if price.GreaterThan(bar.High) {
			bar.High = price
		}
		if price.LessThan(bar.Low) {
			bar.Low = price
		}
		bar.Close = price
	}
}

// updateAncestors ensures every ancestor's current bar reflects the latest price.
// This is needed so that e.g. the 5m current bar shows updated close/high/low
// even before its child (1m) closes.
func (a *KlineAggregator) updateAncestors(price decimal.Decimal, ts time.Time) {
	parentName := a.configs["1s"].Parent
	for parentName != "" {
		cfg := a.configs[parentName]
		if cfg == nil {
			break
		}

		openTime := truncateToInterval(ts, cfg.Duration)
		closeTime := openTime.Add(cfg.Duration)
		bar := a.current[parentName]

		if bar == nil {
			a.current[parentName] = &KlineBar{
				Interval: parentName, OpenTime: openTime, CloseTime: closeTime,
				Open: price, High: price, Low: price, Close: price,
			}
		} else if !bar.OpenTime.Equal(openTime) {
			// Period rolled over — use previous close as new open for continuity
			openPrice := bar.Close
			a.closeBarOnly(bar, cfg)
			highPrice := decimal.Max(openPrice, price)
			lowPrice := decimal.Min(openPrice, price)
			a.current[parentName] = &KlineBar{
				Interval: parentName, OpenTime: openTime, CloseTime: closeTime,
				Open: openPrice, High: highPrice, Low: lowPrice, Close: price,
			}
		} else {
			// Same period — update high/low/close
			if price.GreaterThan(bar.High) {
				bar.High = price
			}
			if price.LessThan(bar.Low) {
				bar.Low = price
			}
			bar.Close = price
		}

		parentName = cfg.Parent
	}
}

// closeAndCascade closes a bar, caches it, and merges into its parent.
// The parent then checks if it should also close, cascading further up.
func (a *KlineAggregator) closeAndCascade(bar *KlineBar, cfg *IntervalConfig) {
	a.cacheBar(bar, cfg)

	// Merge into parent's current bar
	if cfg.Parent == "" {
		return
	}
	parentCfg := a.configs[cfg.Parent]
	if parentCfg == nil {
		return
	}

	parentBar := a.current[cfg.Parent]
	openTime := truncateToInterval(bar.OpenTime, parentCfg.Duration)

	if parentBar != nil && !parentBar.OpenTime.Equal(openTime) {
		// Parent period changed — close parent first, then start new
		a.closeAndCascade(parentBar, parentCfg)
		parentBar = nil
	}

	if parentBar == nil {
		closeTime := openTime.Add(parentCfg.Duration)
		a.current[cfg.Parent] = &KlineBar{
			Interval: cfg.Parent, OpenTime: openTime, CloseTime: closeTime,
			Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close,
			Volume: bar.Volume,
		}
	} else {
		// Merge child into parent
		if bar.High.GreaterThan(parentBar.High) {
			parentBar.High = bar.High
		}
		if bar.Low.LessThan(parentBar.Low) {
			parentBar.Low = bar.Low
		}
		parentBar.Close = bar.Close
		parentBar.Volume = parentBar.Volume.Add(bar.Volume)
	}
}

// closeBarOnly caches a bar without cascading (used by updateAncestors).
func (a *KlineAggregator) closeBarOnly(bar *KlineBar, cfg *IntervalConfig) {
	a.cacheBar(bar, cfg)
}

// cacheBar adds a closed bar to the cache and optionally persists to DB.
func (a *KlineAggregator) cacheBar(bar *KlineBar, cfg *IntervalConfig) {
	bars := a.cache[cfg.Name]
	bars = append([]*KlineBar{bar}, bars...)
	if len(bars) > cfg.MaxCache {
		bars = bars[:cfg.MaxCache]
	}
	a.cache[cfg.Name] = bars

	if cfg.Persist && a.onClose != nil {
		a.onClose(bar)
	}
}

// GetCachedKlines returns cached klines for the given interval.
func (a *KlineAggregator) GetCachedKlines(interval string, limit int) []*KlineBar {
	a.mu.Lock()
	defer a.mu.Unlock()

	bars := a.cache[interval]
	current := a.current[interval]
	if current != nil {
		result := make([]*KlineBar, 0, len(bars)+1)
		result = append(result, current)
		result = append(result, bars...)
		if limit > 0 && limit < len(result) {
			return result[:limit]
		}
		return result
	}
	if limit > 0 && limit < len(bars) {
		return bars[:limit]
	}
	return bars
}

// GetCurrentBar returns the current unclosed bar for an interval.
func (a *KlineAggregator) GetCurrentBar(interval string) *KlineBar {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current[interval]
}

func truncateToInterval(t time.Time, d time.Duration) time.Time {
	if d >= 24*time.Hour {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
	return t.Truncate(d)
}

func (b *KlineBar) ToModelKline() *model.Kline {
	return &model.Kline{
		Interval:  b.Interval,
		OpenTime:  b.OpenTime,
		Open:      b.Open,
		High:      b.High,
		Low:       b.Low,
		Close:     b.Close,
		Volume:    b.Volume,
		CloseTime: b.CloseTime,
	}
}
