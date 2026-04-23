package tutorial

import (
	"testing"
)

func TestAllTopicsDefined(t *testing.T) {
	if len(AllTopicIDs) != TotalTopics {
		t.Errorf("expected %d topics, got %d", TotalTopics, len(AllTopicIDs))
	}

	for _, id := range AllTopicIDs {
		card, ok := Topics[id]
		if !ok {
			t.Errorf("topic %s not found in Topics map", id)
			continue
		}
		if card.ID != id {
			t.Errorf("topic ID mismatch: expected %s, got %s", id, card.ID)
		}
		if card.Title == "" {
			t.Errorf("topic %s has empty title", id)
		}
		if card.Content == "" {
			t.Errorf("topic %s has empty content", id)
		}
	}
}

func TestTopicsWithFormula(t *testing.T) {
	topicsWithFormula := []string{
		TopicLeverage,
		TopicUnrealizedPnl,
		TopicFundingRate,
		TopicLiquidation,
		TopicForceTpADL,
		TopicRealizedPnl,
		TopicTradingFee,
	}

	for _, id := range topicsWithFormula {
		card := Topics[id]
		if card.Formula == "" {
			t.Errorf("topic %s expected to have formula", id)
		}
	}
}
