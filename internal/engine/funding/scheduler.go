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
	s.fundingModel.CreateRate(fundingRate)

	// Settle all active positions
	allPositions := s.positionCache.GetAll()
	for _, pos := range allPositions {
		quantity, _ := decimal.NewFromString(pos.Quantity)
		payment := position.CalcFundingPayment(quantity, currentPrice, s.currentRate, pos.Side)

		// Record settlement
		posValue := quantity.Mul(currentPrice)
		settlement := &model.FundingSettlement{
			UserID:        pos.UserID,
			PositionID:    pos.ID,
			Rate:          s.currentRate,
			PositionValue: posValue,
			Payment:       payment,
			SettleTime:    now,
		}
		s.fundingModel.CreateSettlement(settlement)

		// Update position funding PnL — memory first, DB async
		s.positionCache.Update(pos.ID, func(cp *cache.CachedPosition) {
			oldFunding, _ := decimal.NewFromString(cp.FundingPnl)
			cp.FundingPnl = oldFunding.Add(payment).String()
		})
		s.positionModel.UpdateFundingPnl(pos.ID, payment)

		// Update account balance — memory first, DB async
		s.memAccounts.AddPnl(pos.UserID, payment)
		s.accountModel.AddPnl(pos.UserID, payment)

		// Notify user
		msg, _ := ws.NewMessage("funding_settled", map[string]interface{}{
			"rate":   s.currentRate.String(),
			"amount": payment.StringFixed(4),
		})
		s.hub.SendToUser(pos.UserID, msg)
	}

	log.Printf("[Funding] settled %d positions, rate=%s", len(allPositions), s.currentRate.String())
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
