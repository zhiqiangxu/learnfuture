package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/types"
	"learn_future/testutil"
)

func TestPriceHandler(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.PriceCache.SetPrice(decimal.NewFromFloat(67890.12))

	req := httptest.NewRequest("GET", "/api/v1/market/price", nil)
	rr := httptest.NewRecorder()
	PriceHandler(svcCtx)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Errorf("expected code=0, got %d", resp.Code)
	}
}

func TestMarkPriceHandler(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()
	svcCtx.MarkPriceEngine.UpdateLastPrice(decimal.NewFromInt(60000))

	req := httptest.NewRequest("GET", "/api/v1/market/mark-price", nil)
	rr := httptest.NewRecorder()
	MarkPriceHandler(svcCtx)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Errorf("expected code=0, got %d", resp.Code)
	}
}

func TestInsuranceFundHandler(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	req := httptest.NewRequest("GET", "/api/v1/market/insurance-fund", nil)
	rr := httptest.NewRecorder()
	InsuranceFundHandler(svcCtx)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp types.Response
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Errorf("expected code=0, got %d", resp.Code)
	}
}

func TestFundingRateHandler(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	req := httptest.NewRequest("GET", "/api/v1/market/funding-rate", nil)
	rr := httptest.NewRecorder()
	FundingRateHandler(svcCtx)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestKlinesHandler_FromCache(t *testing.T) {
	svcCtx := testutil.NewTestServiceContext()

	// Add some kline data to cache via aggregator
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	svcCtx.KlineAggregator.OnTrade(decimal.NewFromInt(60000), ts)
	svcCtx.KlineAggregator.OnTrade(decimal.NewFromInt(60100), ts.Add(2*time.Second))

	req := httptest.NewRequest("GET", "/api/v1/market/klines?interval=1s&limit=10", nil)
	rr := httptest.NewRecorder()
	KlinesHandler(svcCtx)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}
