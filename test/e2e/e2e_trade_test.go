//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// E2E tests require a running server.
// Start with: docker-compose up -d && sleep 5
// Run with: go test -tags=e2e -v ./test/e2e/...
// Set E2E_BASE_URL env var (default: http://localhost:8888)

var baseURL string

func init() {
	baseURL = os.Getenv("E2E_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8888"
	}
}

type apiResp struct {
	Code     int             `json:"code"`
	Message  string          `json:"message"`
	Data     json.RawMessage `json:"data"`
	Tutorial json.RawMessage `json:"tutorial"`
}

func get(t *testing.T, path string, token string) *apiResp {
	req, _ := http.NewRequest("GET", baseURL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doRequest(t, req)
}

func post(t *testing.T, path string, body interface{}, token string) *apiResp {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", baseURL+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doRequest(t, req)
}

func doRequest(t *testing.T, req *http.Request) *apiResp {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var r apiResp
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("parse response failed: %s", string(body))
	}
	return &r
}

func TestE2E_HealthCheck(t *testing.T) {
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Skipf("server not running: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health check failed: %d", resp.StatusCode)
	}
}

func TestE2E_MarketPrice(t *testing.T) {
	r := get(t, "/api/v1/market/price", "")
	if r.Code != 0 {
		t.Fatalf("expected code=0, got %d: %s", r.Code, r.Message)
	}

	var data map[string]string
	json.Unmarshal(r.Data, &data)
	if data["price"] == "" || data["price"] == "0" {
		t.Logf("price might not be available yet: %s", data["price"])
	}
}

func TestE2E_MarkPrice(t *testing.T) {
	r := get(t, "/api/v1/market/mark-price", "")
	if r.Code != 0 {
		t.Fatalf("expected code=0, got %d", r.Code)
	}
}

func TestE2E_InsuranceFund(t *testing.T) {
	r := get(t, "/api/v1/market/insurance-fund", "")
	if r.Code != 0 {
		t.Fatalf("expected code=0, got %d", r.Code)
	}
}

func TestE2E_FundingRate(t *testing.T) {
	r := get(t, "/api/v1/market/funding-rate", "")
	if r.Code != 0 {
		t.Fatalf("expected code=0, got %d", r.Code)
	}
}

func TestE2E_Klines(t *testing.T) {
	r := get(t, "/api/v1/market/klines?interval=1m&limit=10", "")
	if r.Code != 0 {
		t.Fatalf("expected code=0, got %d", r.Code)
	}
}

func TestE2E_UnauthorizedAccess(t *testing.T) {
	// Account info without token should fail
	req, _ := http.NewRequest("GET", baseURL+"/api/v1/account/info", nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// TestE2E_FullTradeCycle requires a valid wx login code which can't be automated.
// This test is a template for manual testing.
func TestE2E_FullTradeCycle_Template(t *testing.T) {
	t.Skip("requires valid wx login code - run manually")

	// 1. Login
	loginResp := post(t, "/api/v1/auth/wx-login", map[string]string{"code": "test_code"}, "")
	if loginResp.Code != 0 {
		t.Fatalf("login failed: %s", loginResp.Message)
	}

	var loginData struct {
		Token string `json:"token"`
	}
	json.Unmarshal(loginResp.Data, &loginData)
	token := loginData.Token

	// 2. Check account
	acctResp := get(t, "/api/v1/account/info", token)
	fmt.Printf("Account: %s\n", string(acctResp.Data))

	// 3. Place market long order
	orderResp := post(t, "/api/v1/order/place", map[string]interface{}{
		"side": 1, "order_type": 1, "leverage": 10, "margin": "100",
	}, token)
	fmt.Printf("Order: %s\n", string(orderResp.Data))
	if orderResp.Tutorial != nil {
		fmt.Printf("Tutorial: %s\n", string(orderResp.Tutorial))
	}

	// 4. Check positions
	posResp := get(t, "/api/v1/position/list", token)
	fmt.Printf("Positions: %s\n", string(posResp.Data))

	// 5. Close position
	var orderData struct {
		PositionID int64 `json:"position_id"`
	}
	json.Unmarshal(orderResp.Data, &orderData)
	closeResp := post(t, "/api/v1/position/close", map[string]int64{
		"position_id": orderData.PositionID,
	}, token)
	fmt.Printf("Close: %s\n", string(closeResp.Data))

	// 6. Check history
	histResp := get(t, "/api/v1/history/pnl", token)
	fmt.Printf("PnL: %s\n", string(histResp.Data))
}
