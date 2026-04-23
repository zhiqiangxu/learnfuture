package tutorial

import (
	"testing"
)

func TestShouldShow_NotCompleted(t *testing.T) {
	completed := map[string]bool{}
	card := ShouldShow(TopicLeverage, completed)
	if card == nil {
		t.Error("expected tutorial card for uncompleted topic")
	}
	if card.ID != TopicLeverage {
		t.Errorf("expected ID=%s, got %s", TopicLeverage, card.ID)
	}
}

func TestShouldShow_AlreadyCompleted(t *testing.T) {
	completed := map[string]bool{TopicLeverage: true}
	card := ShouldShow(TopicLeverage, completed)
	if card != nil {
		t.Error("expected nil for completed show_once topic")
	}
}

func TestShouldShow_ShowOnceFalse(t *testing.T) {
	// PostLiquidation has show_once=false
	completed := map[string]bool{TopicPostLiquidation: true}
	card := ShouldShow(TopicPostLiquidation, completed)
	if card == nil {
		t.Error("expected card for show_once=false topic even if completed")
	}
}

func TestShouldShow_InvalidTopic(t *testing.T) {
	card := ShouldShow("nonexistent_topic", map[string]bool{})
	if card != nil {
		t.Error("expected nil for invalid topic")
	}
}

func TestForPostLiquidation(t *testing.T) {
	card := ForPostLiquidation(TriggerContext{
		LiqPrice: "54300",
		Margin:   "100",
	})
	if card == nil {
		t.Fatal("expected card")
	}
	if card.ID != TopicPostLiquidation {
		t.Errorf("expected ID=%s, got %s", TopicPostLiquidation, card.ID)
	}
}

func TestForForceTakeProfit(t *testing.T) {
	card := ForForceTakeProfit(TriggerContext{
		Price:  "90000",
		Profit: "500",
	})
	if card == nil {
		t.Fatal("expected card")
	}
	if card.ID != TopicForceTpADL {
		t.Errorf("expected ID=%s, got %s", TopicForceTpADL, card.ID)
	}
}

func TestForRealizedPnl(t *testing.T) {
	card := ForRealizedPnl(TriggerContext{
		Pnl:     "49.6",
		Fee:     "0.8",
		Funding: "-0.06",
		NetPnl:  "48.74",
	})
	if card == nil {
		t.Fatal("expected card")
	}
	if card.ID != TopicRealizedPnl {
		t.Errorf("expected ID=%s, got %s", TopicRealizedPnl, card.ID)
	}
}
