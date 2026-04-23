package clearing

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// WAL (Write-Ahead Log) ensures no operations are lost on crash.
//
// Real exchange flow:
//   1. Matching engine makes decision
//   2. Write operation to WAL (fsync to disk) ← guarantees durability
//   3. Apply to in-memory state
//   4. Return to user
//   5. Settler asynchronously writes to DB
//   6. After DB confirms, mark WAL entry as committed
//
// On crash recovery:
//   1. Load DB state (last confirmed snapshot)
//   2. Replay uncommitted WAL entries on top of DB state
//   3. Memory state is now consistent
//
// WAL entries are append-only, sequentially written, and fsync'd.
// This is the same pattern used by PostgreSQL, MySQL InnoDB, and
// real exchange matching engines like LMAX.

type WALEntryType string

const (
	WALDeduct    WALEntryType = "DEDUCT"
	WALFreeze    WALEntryType = "FREEZE"
	WALUnfreeze  WALEntryType = "UNFREEZE"
	WALReturn    WALEntryType = "RETURN"
	WALPnl       WALEntryType = "PNL"
	WALOpenPos   WALEntryType = "OPEN_POS"
	WALClosePos  WALEntryType = "CLOSE_POS"
	WALCommitted WALEntryType = "COMMITTED" // marks a group of entries as DB-persisted
)

type WALEntry struct {
	Seq       uint64          `json:"seq"`
	Type      WALEntryType    `json:"type"`
	Timestamp int64           `json:"ts"`
	UserID    int64           `json:"uid,omitempty"`
	Amount    decimal.Decimal `json:"amt,omitempty"`
	Amount2   decimal.Decimal `json:"amt2,omitempty"` // secondary amount (e.g., PnL)
	PosID     int64           `json:"pid,omitempty"`
	Side      int             `json:"side,omitempty"`
	Leverage  int             `json:"lev,omitempty"`
	Price     decimal.Decimal `json:"price,omitempty"`
	Quantity  decimal.Decimal `json:"qty,omitempty"`
	Margin    decimal.Decimal `json:"margin,omitempty"`
	Reason    int             `json:"reason,omitempty"`
	GroupID   uint64          `json:"gid,omitempty"` // groups entries belonging to one operation
}

type WAL struct {
	mu       sync.Mutex
	file     *os.File
	writer   *bufio.Writer
	seq      uint64
	filePath string
	lastCommitted uint64 // last seq that was confirmed persisted to DB
}

func NewWAL(filePath string) (*WAL, error) {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open WAL file: %w", err)
	}

	wal := &WAL{
		file:     file,
		writer:   bufio.NewWriter(file),
		filePath: filePath,
	}

	// Find the highest sequence number in existing WAL
	wal.seq = wal.findMaxSeq()

	return wal, nil
}

// Write appends an entry to the WAL and fsyncs.
// Returns the sequence number assigned to this entry.
func (w *WAL) Write(entry *WALEntry) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.seq++
	entry.Seq = w.seq
	entry.Timestamp = time.Now().UnixNano()

	data, err := json.Marshal(entry)
	if err != nil {
		return 0, err
	}

	// Write entry + newline
	if _, err := w.writer.Write(data); err != nil {
		return 0, err
	}
	if err := w.writer.WriteByte('\n'); err != nil {
		return 0, err
	}

	// Flush buffer and fsync to guarantee durability
	if err := w.writer.Flush(); err != nil {
		return 0, err
	}
	if err := w.file.Sync(); err != nil {
		return 0, err
	}

	return w.seq, nil
}

// WriteGroup writes multiple entries atomically with the same group ID.
func (w *WAL) WriteGroup(entries []*WALEntry) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.seq++
	groupID := w.seq

	for _, entry := range entries {
		w.seq++
		entry.Seq = w.seq
		entry.GroupID = groupID
		entry.Timestamp = time.Now().UnixNano()

		data, err := json.Marshal(entry)
		if err != nil {
			return 0, err
		}
		w.writer.Write(data)
		w.writer.WriteByte('\n')
	}

	if err := w.writer.Flush(); err != nil {
		return 0, err
	}
	if err := w.file.Sync(); err != nil {
		return 0, err
	}

	return groupID, nil
}

// MarkCommitted records that entries up to this seq have been persisted to DB.
func (w *WAL) MarkCommitted(upToSeq uint64) error {
	_, err := w.Write(&WALEntry{
		Type: WALCommitted,
	})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.lastCommitted = upToSeq
	w.mu.Unlock()
	return nil
}

// ReadUncommitted returns all WAL entries that haven't been committed to DB.
// Used during crash recovery.
func (w *WAL) ReadUncommitted() ([]*WALEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.readUncommittedLocked()
}

// readUncommittedLocked is the internal version of ReadUncommitted.
// Caller must hold w.mu.
func (w *WAL) readUncommittedLocked() ([]*WALEntry, error) {
	// Re-read from beginning
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(w.file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line

	var allEntries []*WALEntry
	var lastCommittedSeq uint64

	for scanner.Scan() {
		var entry WALEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // skip corrupted entries
		}
		if entry.Type == WALCommitted {
			lastCommittedSeq = entry.Seq
		} else {
			allEntries = append(allEntries, &entry)
		}
	}

	// Filter out entries before the last committed point
	var uncommitted []*WALEntry
	for _, e := range allEntries {
		if e.Seq > lastCommittedSeq {
			uncommitted = append(uncommitted, e)
		}
	}

	return uncommitted, scanner.Err()
}

// Truncate removes committed entries from the WAL file to prevent unbounded growth.
// Called periodically (e.g., every hour).
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	uncommitted, err := w.readUncommittedLocked()
	if err != nil {
		return err
	}

	// Close current file
	w.writer.Flush()
	w.file.Close()

	// Rewrite with only uncommitted entries
	file, err := os.Create(w.filePath)
	if err != nil {
		return err
	}
	w.file = file
	w.writer = bufio.NewWriter(file)

	for _, entry := range uncommitted {
		data, _ := json.Marshal(entry)
		w.writer.Write(data)
		w.writer.WriteByte('\n')
	}
	w.writer.Flush()
	w.file.Sync()

	return nil
}

// Close flushes and closes the WAL file.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writer.Flush()
	return w.file.Close()
}

func (w *WAL) findMaxSeq() uint64 {
	if _, err := w.file.Seek(0, 0); err != nil {
		return 0
	}
	scanner := bufio.NewScanner(w.file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var maxSeq uint64
	for scanner.Scan() {
		var entry WALEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Seq > maxSeq {
			maxSeq = entry.Seq
		}
	}
	return maxSeq
}

// ReplayEntry applies a WAL entry to the in-memory account cache.
// Used during crash recovery to rebuild state from DB + uncommitted WAL.
func ReplayEntry(entry *WALEntry, accounts interface {
	Deduct(userID int64, amount decimal.Decimal) bool
	Freeze(userID int64, amount decimal.Decimal) bool
	Unfreeze(userID int64, amount decimal.Decimal)
	ReturnMarginWithPnl(userID int64, margin, pnl decimal.Decimal)
	AddPnl(userID int64, pnl decimal.Decimal)
}) {
	switch entry.Type {
	case WALDeduct:
		accounts.Deduct(entry.UserID, entry.Amount)
	case WALFreeze:
		accounts.Freeze(entry.UserID, entry.Amount)
	case WALUnfreeze:
		accounts.Unfreeze(entry.UserID, entry.Amount)
	case WALReturn:
		accounts.ReturnMarginWithPnl(entry.UserID, entry.Amount, entry.Amount2)
	case WALPnl:
		accounts.AddPnl(entry.UserID, entry.Amount)
	default:
		log.Printf("[WAL] unknown entry type for replay: %s", entry.Type)
	}
}
