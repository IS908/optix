package intel

import "math"

// Score 纯函数对账打分：比对登记价与到期价，按方向+阈值判 hit/miss/void。
// thresholdPct 默认 0；up 用 >=th（故 delta=0、th=0 算 hit）。flat 是 |delta|<=th 容差带。
// 价缺失/非正 → void（不计命中率）。M3 只产 hit/miss/void；push 保留给将来手动场景。
func Score(direction string, thresholdPct, registeredPrice, expiryPrice float64) (outcome string, deltaPct float64) {
	if registeredPrice <= 0 || expiryPrice <= 0 {
		return "void", 0
	}
	deltaPct = (expiryPrice - registeredPrice) / registeredPrice * 100
	switch direction {
	case "up":
		if deltaPct >= thresholdPct {
			return "hit", deltaPct
		}
		return "miss", deltaPct
	case "down":
		if deltaPct <= -thresholdPct {
			return "hit", deltaPct
		}
		return "miss", deltaPct
	case "flat":
		if math.Abs(deltaPct) <= thresholdPct {
			return "hit", deltaPct
		}
		return "miss", deltaPct
	}
	return "void", deltaPct
}
