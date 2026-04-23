package cache

import (
	"sync"

	"github.com/shopspring/decimal"
)

// MemAccount holds in-memory account balance for fast matching decisions.
// This is the source of truth during runtime; DB is async backup.
type MemAccount struct {
	Balance decimal.Decimal
	Frozen  decimal.Decimal
}

// AccountCache provides thread-safe in-memory account management.
// The matching engine reads/writes this instead of hitting the DB.
type AccountCache struct {
	mu       sync.RWMutex
	accounts map[int64]*MemAccount // userID -> account
}

func NewAccountCache() *AccountCache {
	return &AccountCache{
		accounts: make(map[int64]*MemAccount),
	}
}

// Load initializes an account from DB values (called on startup).
func (c *AccountCache) Load(userID int64, balance, frozen decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accounts[userID] = &MemAccount{Balance: balance, Frozen: frozen}
}

// GetOrCreate returns the account, creating with zero balance if not exists.
func (c *AccountCache) GetOrCreate(userID int64) *MemAccount {
	c.mu.Lock()
	defer c.mu.Unlock()
	acc, ok := c.accounts[userID]
	if !ok {
		acc = &MemAccount{}
		c.accounts[userID] = acc
	}
	return acc
}

// GetBalance returns available balance (balance - frozen).
func (c *AccountCache) GetBalance(userID int64) decimal.Decimal {
	c.mu.RLock()
	defer c.mu.RUnlock()
	acc, ok := c.accounts[userID]
	if !ok {
		return decimal.Zero
	}
	return acc.Balance.Sub(acc.Frozen)
}

// Deduct subtracts amount from balance. Returns false if insufficient.
func (c *AccountCache) Deduct(userID int64, amount decimal.Decimal) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	acc, ok := c.accounts[userID]
	if !ok {
		return false
	}
	available := acc.Balance.Sub(acc.Frozen)
	if available.LessThan(amount) {
		return false
	}
	acc.Balance = acc.Balance.Sub(amount)
	return true
}

// Freeze moves amount from available to frozen. Returns false if insufficient.
func (c *AccountCache) Freeze(userID int64, amount decimal.Decimal) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	acc, ok := c.accounts[userID]
	if !ok {
		return false
	}
	available := acc.Balance.Sub(acc.Frozen)
	if available.LessThan(amount) {
		return false
	}
	acc.Frozen = acc.Frozen.Add(amount)
	return true
}

// Unfreeze moves amount from frozen back to available.
func (c *AccountCache) Unfreeze(userID int64, amount decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	acc, ok := c.accounts[userID]
	if !ok {
		return
	}
	acc.Frozen = acc.Frozen.Sub(amount)
	if acc.Frozen.IsNegative() {
		acc.Frozen = decimal.Zero
	}
}

// ReturnMarginWithPnl returns margin to balance and applies PnL.
func (c *AccountCache) ReturnMarginWithPnl(userID int64, margin, pnl decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	acc, ok := c.accounts[userID]
	if !ok {
		return
	}
	acc.Balance = acc.Balance.Add(margin).Add(pnl)
}

// AddPnl adds PnL to balance (can be negative).
func (c *AccountCache) AddPnl(userID int64, pnl decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	acc, ok := c.accounts[userID]
	if !ok {
		return
	}
	acc.Balance = acc.Balance.Add(pnl)
}

// Reset resets account to initial balance.
func (c *AccountCache) Reset(userID int64, balance decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accounts[userID] = &MemAccount{Balance: balance}
}
