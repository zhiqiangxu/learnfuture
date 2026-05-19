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

// reconcileTolerance is the max acceptable drift between memory and DB.
// Set above the 8th-decimal rounding noise from PG numeric(20,8) truncation
// vs. full-precision decimal math in memory; anything larger is a real bug.
var reconcileTolerance = decimal.New(1, -6) // 1e-6

// AlertConfig holds SMTP credentials for reconcile alert email.
// Leaving any field empty disables email delivery (issues are still logged).
type AlertConfig struct {
	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
	To       string
}

// Reconciler runs daily T+1 checks comparing memory state against DB.
// Discrepancies are logged as warnings and emailed to admin.
type Reconciler struct {
	db            *sql.DB
	memAccounts   *cache.AccountCache
	positionCache *cache.PositionCache
	alert         AlertConfig
	done          chan struct{}
}

func New(db *sql.DB, memAccounts *cache.AccountCache, positionCache *cache.PositionCache, alert AlertConfig) *Reconciler {
	return &Reconciler{
		db:            db,
		memAccounts:   memAccounts,
		positionCache: positionCache,
		alert:         alert,
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
		diff := memBalance.Sub(dbBalance)
		if diff.Abs().GreaterThan(reconcileTolerance) {
			issues = append(issues, fmt.Sprintf("BALANCE MISMATCH user=%d mem=%s db=%s diff=%s",
				userID, memBalance.StringFixed(8), dbBalance.StringFixed(8),
				diff.StringFixed(8)))
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
		if memQty.Sub(dbQty).Abs().GreaterThan(reconcileTolerance) {
			issues = append(issues, fmt.Sprintf("POSITION QTY MISMATCH id=%d mem=%s db=%s", posID, memQty, dbQty))
		}
	}
	return issues
}

// sendAlert sends email notification for reconciliation issues.
func (r *Reconciler) sendAlert(issues []string) {
	a := r.alert
	if a.SMTPHost == "" || a.SMTPUser == "" || a.SMTPPass == "" || a.To == "" {
		log.Printf("[Reconcile] alert email not configured; skipping (%d issues logged above)", len(issues))
		return
	}
	port := a.SMTPPort
	if port == "" {
		port = "587"
	}

	subject := fmt.Sprintf("LearnFuture Reconcile ALERT: %d issues", len(issues))
	body := fmt.Sprintf("Daily reconciliation found %d discrepancies:\n\n%s\n\nTime: %s",
		len(issues), strings.Join(issues, "\n"), time.Now().Format(time.RFC3339))

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		a.SMTPUser, a.To, subject, body)

	auth := smtp.PlainAuth("", a.SMTPUser, a.SMTPPass, a.SMTPHost)
	err := smtp.SendMail(a.SMTPHost+":"+port, auth, a.SMTPUser, []string{a.To}, []byte(msg))
	if err != nil {
		log.Printf("[Reconcile] failed to send alert email: %v", err)
	} else {
		log.Printf("[Reconcile] alert email sent to %s", a.To)
	}
}
