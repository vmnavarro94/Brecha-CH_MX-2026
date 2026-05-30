package types

import (
	"encoding/json"
	"testing"
)

// TestStrategyFieldExists verifies that Opportunity and Trade each have a Strategy
// string field that serializes as "strategy" in JSON with omitempty semantics.
func TestStrategyFieldExists(t *testing.T) {
	opp := Opportunity{}
	opp.Strategy = "spatial"

	b, err := json.Marshal(opp)
	if err != nil {
		t.Fatalf("marshal Opportunity: %v", err)
	}
	if got := string(b); len(got) == 0 {
		t.Fatal("empty JSON for Opportunity")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["strategy"] != "spatial" {
		t.Errorf("Opportunity JSON strategy: got %v, want \"spatial\"", m["strategy"])
	}

	trade := Trade{}
	trade.Strategy = "spatial"

	b2, err := json.Marshal(trade)
	if err != nil {
		t.Fatalf("marshal Trade: %v", err)
	}
	var m2 map[string]interface{}
	if err := json.Unmarshal(b2, &m2); err != nil {
		t.Fatalf("unmarshal trade: %v", err)
	}
	if m2["strategy"] != "spatial" {
		t.Errorf("Trade JSON strategy: got %v, want \"spatial\"", m2["strategy"])
	}
}
