package campaign

import (
	"context"
	"encoding/json"
	"github.com/rjrddy/throttle/internal/gateway"
	"testing"
	"time"
)

func TestSeededPlanAndRealHTTPReconciliation(t *testing.T) {
	a, err := Plan("mixed", time.Second, 52017)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Plan("mixed", time.Second, 52017)
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	if string(left) != string(right) {
		t.Fatal("non-deterministic offers")
	}
	result, err := Isolated(context.Background(), "adaptive", gateway.TokenBucket, "mixed", 300*time.Millisecond, 52017)
	if err != nil {
		t.Fatal(err)
	}
	offered := 0
	for _, c := range result.Classes {
		if c.Success+c.Limited+c.Capacity+c.Other != c.Offered {
			t.Fatal("lost receipt")
		}
		offered += c.Offered
	}
	if offered != len(result.Receipts) || result.OriginRequests == 0 || result.PeakOriginInflight > 4 {
		t.Fatal("invalid live campaign", result)
	}
	for _, receipt := range result.Receipts {
		if receipt.Outcome == "transport-error" || receipt.Outcome == "load-client-saturated" {
			t.Fatal("load client invalidated trial", receipt)
		}
	}
}
