package clearing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"learn_future/internal/cache"
)

func tempWALPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "test.wal")
}

func TestWAL_WriteAndRead(t *testing.T) {
	path := tempWALPath(t)
	wal, err := NewWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	defer wal.Close()

	seq, err := wal.Write(&WALEntry{
		Type:   WALDeduct,
		UserID: 1,
		Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Errorf("expected seq=1, got %d", seq)
	}

	entries, err := wal.ReadUncommitted()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 uncommitted entry, got %d", len(entries))
	}
	if entries[0].UserID != 1 {
		t.Errorf("expected userID=1, got %d", entries[0].UserID)
	}
}

func TestWAL_CommittedEntriesFiltered(t *testing.T) {
	path := tempWALPath(t)
	wal, err := NewWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	defer wal.Close()

	// Write 3 entries
	wal.Write(&WALEntry{Type: WALDeduct, UserID: 1, Amount: decimal.NewFromInt(100)})
	wal.Write(&WALEntry{Type: WALDeduct, UserID: 2, Amount: decimal.NewFromInt(200)})
	seq3, _ := wal.Write(&WALEntry{Type: WALDeduct, UserID: 3, Amount: decimal.NewFromInt(300)})

	// Mark first 2 as committed (committed at seq=3 means entries with seq<=3 committed)
	// Actually MarkCommitted writes a new entry, so seq increments
	wal.MarkCommitted(seq3)

	// Write one more after commit
	wal.Write(&WALEntry{Type: WALDeduct, UserID: 4, Amount: decimal.NewFromInt(400)})

	entries, err := wal.ReadUncommitted()
	if err != nil {
		t.Fatal(err)
	}
	// Only entry 4 (seq=5) should be uncommitted (commit entry was seq=4)
	if len(entries) != 1 {
		t.Fatalf("expected 1 uncommitted entry, got %d", len(entries))
	}
	if entries[0].UserID != 4 {
		t.Errorf("expected userID=4, got %d", entries[0].UserID)
	}
}

func TestWAL_CrashRecovery(t *testing.T) {
	path := tempWALPath(t)

	// Simulate: write entries, then "crash" (close without commit)
	func() {
		wal, _ := NewWAL(path)
		wal.Write(&WALEntry{Type: WALDeduct, UserID: 1, Amount: decimal.NewFromInt(100)})
		wal.Write(&WALEntry{Type: WALFreeze, UserID: 1, Amount: decimal.NewFromInt(50)})
		wal.Close() // "crash" — entries not committed
	}()

	// Recovery: reopen WAL, read uncommitted, replay
	wal, err := NewWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	defer wal.Close()

	entries, err := wal.ReadUncommitted()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 uncommitted entries for replay, got %d", len(entries))
	}

	// Replay on fresh account cache
	accounts := cache.NewAccountCache()
	accounts.Load(1, decimal.NewFromInt(1000), decimal.Zero)

	for _, entry := range entries {
		ReplayEntry(entry, accounts)
	}

	// After replay: balance should be 1000-100=900, frozen should be 50
	bal := accounts.GetBalance(1) // available = balance - frozen
	// Deduct: balance=900, Freeze: frozen=50, available=850
	expected := decimal.NewFromInt(850)
	if !bal.Equal(expected) {
		t.Errorf("after replay expected available=850, got %s", bal)
	}
}

func TestWAL_WriteGroup(t *testing.T) {
	path := tempWALPath(t)
	wal, err := NewWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	defer wal.Close()

	groupID, err := wal.WriteGroup([]*WALEntry{
		{Type: WALDeduct, UserID: 1, Amount: decimal.NewFromInt(100)},
		{Type: WALOpenPos, UserID: 1, PosID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if groupID == 0 {
		t.Error("expected non-zero groupID")
	}

	entries, _ := wal.ReadUncommitted()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Both should have same group ID
	if entries[0].GroupID != entries[1].GroupID {
		t.Error("entries in same group should have same groupID")
	}
}

func TestWAL_PersistAcrossReopen(t *testing.T) {
	path := tempWALPath(t)

	// Write and close
	wal1, _ := NewWAL(path)
	wal1.Write(&WALEntry{Type: WALDeduct, UserID: 1, Amount: decimal.NewFromInt(100)})
	wal1.Close()

	// Reopen — should see the entry
	wal2, _ := NewWAL(path)
	defer wal2.Close()

	entries, _ := wal2.ReadUncommitted()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after reopen, got %d", len(entries))
	}

	// New writes should continue seq
	seq, _ := wal2.Write(&WALEntry{Type: WALDeduct, UserID: 2})
	if seq <= 1 {
		t.Errorf("seq should continue from previous, got %d", seq)
	}
}

func TestWAL_FileCreatedIfNotExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subdir", "test.wal")
	os.MkdirAll(filepath.Dir(path), 0755)

	wal, err := NewWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	wal.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("WAL file should be created")
	}
}

func TestReplayEntry_AllTypes(t *testing.T) {
	accounts := cache.NewAccountCache()
	accounts.Load(1, decimal.NewFromInt(1000), decimal.Zero)

	// Deduct 100
	ReplayEntry(&WALEntry{Type: WALDeduct, UserID: 1, Amount: decimal.NewFromInt(100)}, accounts)
	// Freeze 200
	ReplayEntry(&WALEntry{Type: WALFreeze, UserID: 1, Amount: decimal.NewFromInt(200)}, accounts)
	// available = 900 - 200 = 700

	bal := accounts.GetBalance(1)
	if !bal.Equal(decimal.NewFromInt(700)) {
		t.Errorf("expected 700, got %s", bal)
	}

	// Unfreeze 200
	ReplayEntry(&WALEntry{Type: WALUnfreeze, UserID: 1, Amount: decimal.NewFromInt(200)}, accounts)
	// available = 900 - 0 = 900

	bal = accounts.GetBalance(1)
	if !bal.Equal(decimal.NewFromInt(900)) {
		t.Errorf("expected 900, got %s", bal)
	}

	// Return margin 100 + pnl 50
	ReplayEntry(&WALEntry{Type: WALReturn, UserID: 1, Amount: decimal.NewFromInt(100), Amount2: decimal.NewFromInt(50)}, accounts)
	// balance = 900 + 100 + 50 = 1050

	bal = accounts.GetBalance(1)
	if !bal.Equal(decimal.NewFromInt(1050)) {
		t.Errorf("expected 1050, got %s", bal)
	}

	// AddPnl -30
	ReplayEntry(&WALEntry{Type: WALPnl, UserID: 1, Amount: decimal.NewFromInt(-30)}, accounts)
	bal = accounts.GetBalance(1)
	if !bal.Equal(decimal.NewFromInt(1020)) {
		t.Errorf("expected 1020, got %s", bal)
	}
}
