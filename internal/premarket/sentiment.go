package premarket

// vixTermPremium = VIX3M / VIX。>1 contango(远月升水,平稳)、<1 backwardation(近月升水,承压)。
// 降级口径(指数族比值,非真实期货曲线)。VIX<=0 → 0。
func vixTermPremium(vix, vix3m float64) float64 {
	if vix <= 0 {
		return 0
	}
	return vix3m / vix
}

// regimeLabel 粗 regime(固定经验阈值,降级):高 P/C + backwardation=防御,低 P/C + contango=偏多。
func regimeLabel(pcOI, termPremium float64, pcAvailable bool) string {
	if !pcAvailable && termPremium <= 0 {
		return "不可用"
	}
	defensive := 0
	if pcAvailable && pcOI >= 1.15 { // 看跌持仓偏多 → 避险
		defensive++
	} else if pcAvailable && pcOI <= 0.85 {
		defensive--
	}
	if termPremium > 0 && termPremium < 1.0 { // backwardation → 承压
		defensive++
	} else if termPremium >= 1.05 { // contango → 平稳
		defensive--
	}
	switch {
	case defensive >= 1:
		return "防御"
	case defensive <= -1:
		return "偏多"
	default:
		return "中性"
	}
}
