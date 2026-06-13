package premarket

import "fmt"

// chainConsistency 统计隔夜传导链的方向接力一致性：与首环同号的环数 / 总环数 + 描述注。
// 描述性，不算 correlation（避免冒充因果）。
func chainConsistency(pcts []float64) (sameDir, total int, note string) {
	total = len(pcts)
	if total == 0 {
		return 0, 0, ""
	}
	firstUp := pcts[0] >= 0
	for _, p := range pcts {
		if (p >= 0) == firstUp {
			sameDir++
		}
	}
	dir := "↑"
	if !firstUp {
		dir = "↓"
	}
	if sameDir == total {
		note = fmt.Sprintf("%d 环方向一致 %s", total, dir)
	} else {
		note = fmt.Sprintf("%d/%d 环与首环同向（链条有断点）", sameDir, total)
	}
	return sameDir, total, note
}
