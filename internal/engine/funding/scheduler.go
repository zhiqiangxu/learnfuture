package funding

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
	"learn_future/internal/engine/position"
	"learn_future/internal/model"
	"learn_future/internal/ws"
)

type Scheduler struct {
	settleHours   []int
	fetchURL      string
	priceCache    *cache.PriceCache
	positionCache *cache.PositionCache
	memAccounts   *cache.AccountCache
	positionModel *model.PositionModel
	accountModel  *model.AccountModel
	fundingModel  *model.FundingModel
	hub           *ws.Hub
	done          chan struct{}
	currentRate   decimal.Decimal
	maintRate     decimal.Decimal
}

func NewScheduler(
	settleHours []int,
	fetchURL string,
	priceCache *cache.PriceCache,
	positionCache *cache.PositionCache,
	memAccounts *cache.AccountCache,
	positionModel *model.PositionModel,
	accountModel *model.AccountModel,
	fundingModel *model.FundingModel,
	hub *ws.Hub,
	maintRate decimal.Decimal,
) *Scheduler {
	return &Scheduler{
		settleHours:   settleHours,
		fetchURL:      fetchURL,
		priceCache:    priceCache,
		positionCache: positionCache,
		memAccounts:   memAccounts,
		positionModel: positionModel,
		accountModel:  accountModel,
		fundingModel:  fundingModel,
		hub:           hub,
		done:          make(chan struct{}),
		maintRate:     maintRate,
	}
}

func (s *Scheduler) Start() {
	// Fetch initial rate
	s.fetchRate()

	go s.run()
}

func (s *Scheduler) Stop() {
	close(s.done)
}

func (s *Scheduler) GetCurrentRate() decimal.Decimal {
	return s.currentRate
}

func (s *Scheduler) GetNextSettleTime() time.Time {
	now := time.Now().UTC()
	for _, h := range s.settleHours {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC)
		if t.After(now) {
			return t
		}
	}
	// Next day first settle hour
	t := time.Date(now.Year(), now.Month(), now.Day()+1, s.settleHours[0], 0, 0, 0, time.UTC)
	return t
}

func (s *Scheduler) run() {
	for {
		nextSettle := s.GetNextSettleTime()
		waitDuration := time.Until(nextSettle)

		select {
		case <-time.After(waitDuration):
			s.settle()
		case <-s.done:
			return
		}
	}
}

func (s *Scheduler) settle() {
	log.Printf("[Funding] starting settlement")

	// Fetch latest rate
	s.fetchRate()

	if s.currentRate.IsZero() {
		log.Printf("[Funding] rate is zero, skipping settlement")
		return
	}

	now := time.Now().UTC()
	currentPrice := s.priceCache.GetPrice()
	if currentPrice.IsZero() {
		log.Printf("[Funding] no price available, skipping settlement")
		return
	}

	// Save rate to DB
	fundingRate := &model.FundingRate{
		Symbol:     "BTCUSDT",
		Rate:       s.currentRate,
		SettleTime: now,
	}
	if err := s.fundingModel.CreateRate(fundingRate); err != nil {
		log.Printf("[Funding] create rate error: %v, aborting settlement", err)
		return
	}

	// Settle all active positions
	allPositions := s.positionCache.GetAll()
	for _, pos := range allPositions {
		for retries := 0; ; retries++ {
			err := s.settlePosition(pos, currentPrice, now)
			if err == nil {
				break
			}
			log.Printf("[Funding] settle position %d error (attempt %d): %v", pos.ID, retries+1, err)
			time.Sleep(time.Duration(min(retries+1, 5)) * time.Second) // backoff: 1s, 2s, 3s, 4s, 5s, 5s, ...
		}
	}

	log.Printf("[Funding] settled %d positions, rate=%s", len(allPositions), s.currentRate.String())
}

func (s *Scheduler) settlePosition(pos *cache.CachedPosition, currentPrice decimal.Decimal, now time.Time) error {
	quantity, _ := decimal.NewFromString(pos.Quantity)
	payment := position.CalcFundingPayment(quantity, currentPrice, s.currentRate, pos.Side)

	// Step 1: Write all DB changes first (before touching memory)
	posValue := quantity.Mul(currentPrice)
	settlement := &model.FundingSettlement{
		UserID: pos.UserID, PositionID: pos.ID,
		Rate: s.currentRate, PositionValue: posValue,
		Payment: payment, SettleTime: now,
	}
	if err := s.fundingModel.CreateSettlement(settlement); err != nil {
		return fmt.Errorf("create settlement: %w", err)
	}
	if err := s.positionModel.UpdateFundingPnl(pos.ID, payment); err != nil {
		return fmt.Errorf("update funding pnl: %w", err)
	}

	// Step 2: Handle balance — check if balance is sufficient
	if payment.IsNegative() {
		balance := s.memAccounts.GetBalance(pos.UserID)
		if balance.Add(payment).IsNegative() {
			return s.settleInsufficientBalance(pos, payment, balance, quantity)
		}
	}

	if err := s.accountModel.AddPnl(pos.UserID, payment); err != nil {
		return fmt.Errorf("add pnl: %w", err)
	}

	// Step 3: DB succeeded — now update memory
	s.positionCache.Update(pos.ID, func(cp *cache.CachedPosition) {
		oldFunding, _ := decimal.NewFromString(cp.FundingPnl)
		cp.FundingPnl = oldFunding.Add(payment).String()
	})
	s.memAccounts.AddPnl(pos.UserID, payment)

	// Notify user
	msg, _ := ws.NewMessage("funding_settled", map[string]interface{}{
		"rate":   s.currentRate.String(),
		"amount": payment.StringFixed(4),
	})
	s.hub.SendToUser(pos.UserID, msg)
	return nil
}

func (s *Scheduler) settleInsufficientBalance(pos *cache.CachedPosition, payment, balance decimal.Decimal, quantity decimal.Decimal) error {
	fromBalance := balance
	fromMargin := payment.Add(fromBalance).Neg()

	// Step 1: DB first
	if fromBalance.IsPositive() {
		if err := s.accountModel.AddPnl(pos.UserID, fromBalance.Neg()); err != nil {
			return fmt.Errorf("deduct balance: %w", err)
		}
	}

	margin, _ := decimal.NewFromString(pos.Margin)
	entryPrice, _ := decimal.NewFromString(pos.EntryPrice)
	newMargin := margin.Sub(fromMargin)
	if newMargin.IsNegative() {
		newMargin = decimal.Zero
	}

	newFundingPnl := func() decimal.Decimal {
		old, _ := decimal.NewFromString(pos.FundingPnl)
		return old.Add(payment)
	}()
	newLiqPrice := position.CalcLiquidationPriceWithFunding(
		entryPrice, pos.Leverage, pos.Side, s.maintRate, newFundingPnl, quantity)

	if err := s.positionModel.UpdateMargin(pos.ID, newMargin, newLiqPrice); err != nil {
		return fmt.Errorf("update margin: %w", err)
	}

	// Step 2: DB succeeded — update memory
	if fromBalance.IsPositive() {
		s.memAccounts.AddPnl(pos.UserID, fromBalance.Neg())
	}
	s.positionCache.Update(pos.ID, func(cp *cache.CachedPosition) {
		oldFunding, _ := decimal.NewFromString(cp.FundingPnl)
		cp.FundingPnl = oldFunding.Add(payment).String()
		cp.Margin = newMargin.String()
		cp.LiqPrice = newLiqPrice.String()
	})

	log.Printf("[Funding] user %d insufficient balance, deducted %s from margin, new liq=%s",
		pos.UserID, fromMargin, newLiqPrice.StringFixed(2))
	return nil
}

type premiumIndexResp struct {
	LastFundingRate string `json:"lastFundingRate"`
}

func (s *Scheduler) fetchRate() {
	if s.fetchURL == "" {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(s.fetchURL)
	if err != nil {
		log.Printf("[Funding] fetch rate error: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Funding] read response error: %v", err)
		return
	}

	// Try Binance format: {"lastFundingRate":"0.0001"}
	var binance struct {
		LastFundingRate string `json:"lastFundingRate"`
	}
	if err := json.Unmarshal(body, &binance); err == nil && binance.LastFundingRate != "" {
		if rate, err := decimal.NewFromString(binance.LastFundingRate); err == nil {
			s.currentRate = rate
			log.Printf("[Funding] fetched rate: %s", rate.String())
			return
		}
	}

	// Try Gate.io format: [{"funding_rate_indicative":"-0.000191",...}]
	var gateArr []struct {
		FundingRate string `json:"funding_rate_indicative"`
	}
	if err := json.Unmarshal(body, &gateArr); err == nil && len(gateArr) > 0 {
		if rate, err := decimal.NewFromString(gateArr[0].FundingRate); err == nil {
			s.currentRate = rate
			log.Printf("[Funding] fetched rate (Gate.io): %s", rate.String())
			return
		}
	}

	log.Printf("[Funding] could not parse rate from response")
}

// SettleNow forces an immediate settlement (for testing).
func (s *Scheduler) SettleNow() {
	s.settle()
}

// SetRate sets the funding rate manually (for testing).
func (s *Scheduler) SetRate(rate decimal.Decimal) {
	s.currentRate = rate
}

// FormatPayment formats the funding payment for display.
func FormatPayment(payment decimal.Decimal) string {
	if payment.IsPositive() {
		return fmt.Sprintf("+%s", payment.StringFixed(4))
	}
	return payment.StringFixed(4)
}
