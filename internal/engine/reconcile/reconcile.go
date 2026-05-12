package reconcile

import (
	"database/sql"
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
)

// Reconciler runs daily T+1 checks comparing memory state against DB.
// Discrepancies are logged as warnings and emailed to admin.
type Reconciler struct {
	db            *sql.DB
	memAccounts   *cache.AccountCache
	positionCache *cache.PositionCache
	done          chan struct{}
}

func New(db *sql.DB, memAccounts *cache.AccountCache, positionCache *cache.PositionCache) *Reconciler {
	return &Reconciler{
		db:            db,
		memAccounts:   memAccounts,
		positionCache: positionCache,
		done:          make(chan struct{}),
	}
}

func (r *Reconciler) Start() {
	go r.run()
}

func (r *Reconciler) Stop() {
	close(r.done)
}

func (r *Reconciler) run() {
	// Run first check 1 minute after startup, then daily at 04:00 UTC
	timer := time.NewTimer(1 * time.Minute)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			r.RunAll()
			// Schedule next run at 04:00 UTC tomorrow
			now := time.Now().UTC()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 4, 0, 0, 0, time.UTC)
			timer.Reset(next.Sub(now))
		case <-r.done:
			return
		}
	}
}

// RunAll executes all reconciliation checks.
func (r *Reconciler) RunAll() {
	log.Println("[Reconcile] starting daily reconciliation...")
	var details []string
	details = append(details, r.checkBalances()...)
	details = append(details, r.checkPositions()...)
	details = append(details, r.checkZeroSum()...)

	if len(details) == 0 {
		log.Println("[Reconcile] all checks passed")
	} else {
		log.Printf("[Reconcile] WARNING: %d discrepancies found", len(details))
		for _, d := range details {
			log.Printf("[Reconcile] %s", d)
		}
		r.sendAlert(details)
	}
}

// checkBalances compares memory balances against DB for all real users (userID > 0).
func (r *Reconciler) checkBalances() []string {
	rows, err := r.db.Query(`SELECT user_id, balance FROM accounts WHERE user_id > 0`)
	if err != nil {
		return []string{fmt.Sprintf("balance check query failed: %v", err)}
	}
	defer rows.Close()

	var issues []string
	for rows.Next() {
		var userID int64
		var dbBalance decimal.Decimal
		if err := rows.Scan(&userID, &dbBalance); err != nil {
			issues = append(issues, fmt.Sprintf("balance scan error: %v", err))
			continue
		}
		memBalance := r.memAccounts.GetBalance(userID)
		// Compare at DB precision (8 decimal places) to avoid false positives
		if !memBalance.Round(8).Equal(dbBalance.Round(8)) {
			issues = append(issues, fmt.Sprintf("BALANCE MISMATCH user=%d mem=%s db=%s diff=%s",
				userID, memBalance.StringFixed(8), dbBalance.StringFixed(8),
				memBalance.Sub(dbBalance).StringFixed(8)))
		}
	}
	return issues
}

// checkPositions compares memory active positions against DB.
func (r *Reconciler) checkPositions() []string {
	var dbCount int
	err := r.db.QueryRow(`SELECT count(*) FROM positions WHERE status = 1 AND user_id > 0`).Scan(&dbCount)
	if err != nil {
		return []string{fmt.Sprintf("position count query failed: %v", err)}
	}

	allPositions := r.positionCache.GetAll()
	memCount := 0
	for _, p := range allPositions {
		if p.UserID > 0 {
			memCount++
		}
	}

	var issues []string
	if memCount != dbCount {
		issues = append(issues, fmt.Sprintf("POSITION COUNT MISMATCH mem=%d db=%d", memCount, dbCount))
	}

	rows, err := r.db.Query(`SELECT id, user_id, quantity FROM positions WHERE status = 1 AND user_id > 0`)
	if err != nil {
		return append(issues, fmt.Sprintf("position detail query failed: %v", err))
	}
	defer rows.Close()

	for rows.Next() {
		var posID, userID int64
		var dbQty decimal.Decimal
		if err := rows.Scan(&posID, &userID, &dbQty); err != nil {
			issues = append(issues, fmt.Sprintf("position scan error: %v", err))
			continue
		}
		cp, ok := r.positionCache.Get(posID)
		if !ok {
			issues = append(issues, fmt.Sprintf("POSITION MISSING in memory: id=%d user=%d", posID, userID))
			continue
		}
		memQty, _ := decimal.NewFromString(cp.Quantity)
		if !memQty.Round(8).Equal(dbQty.Round(8)) {
			issues = append(issues, fmt.Sprintf("POSITION QTY MISMATCH id=%d mem=%s db=%s", posID, memQty, dbQty))
		}
	}
	return issues
}

// checkZeroSum verifies ∑longs ≡ ∑shorts across all positions in memory.
func (r *Reconciler) checkZeroSum() []string {
	allPositions := r.positionCache.GetAll()
	sumLong := decimal.Zero
	sumShort := decimal.Zero
	for _, p := range allPositions {
		qty, _ := decimal.NewFromString(p.Quantity)
		if p.Side == 1 {
			sumLong = sumLong.Add(qty)
		} else {
			sumShort = sumShort.Add(qty)
		}
	}

	if !sumLong.Equal(sumShort) {
		return []string{fmt.Sprintf("ZERO-SUM VIOLATED longs=%s shorts=%s diff=%s",
			sumLong, sumShort, sumLong.Sub(sumShort))}
	}
	return nil
}

// sendAlert sends email notification for reconciliation issues.
func (r *Reconciler) sendAlert(issues []string) {
	const (
		smtpHost = "smtp.gmail.com"
		smtpPort = "587"
		smtpUser = "shore.cloud@gmail.com"
		smtpPass = "bwcn ljgx lqsq uomj"
		alertTo  = "652732310@qq.com"
	)

	subject := fmt.Sprintf("LearnFuture Reconcile ALERT: %d issues", len(issues))
	body := fmt.Sprintf("Daily reconciliation found %d discrepancies:\n\n%s\n\nTime: %s",
		len(issues), strings.Join(issues, "\n"), time.Now().Format(time.RFC3339))

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		smtpUser, alertTo, subject, body)

	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, smtpUser, []string{alertTo}, []byte(msg))
	if err != nil {
		log.Printf("[Reconcile] failed to send alert email: %v", err)
	} else {
		log.Printf("[Reconcile] alert email sent to %s", alertTo)
	}
}
