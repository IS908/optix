package premarket

import "testing"

func TestMarketAdapterSatisfiesInterface(t *testing.T) {
	var _ MarketSource = NewMarketAdapter("python3")
}
