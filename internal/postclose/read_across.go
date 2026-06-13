package postclose

import (
	"fmt"
	"sort"

	"github.com/IS908/optix/internal/portfolio"
)

func BuildReadAcrossEdges(movers []Mover, sm *portfolio.SectorMap) []ReadAcrossEdge {
	bySector := map[string][]Mover{}
	for _, m := range movers {
		sector := sm.Sector(m.Symbol)
		bySector[sector] = append(bySector[sector], m)
	}
	drivers := append([]Mover(nil), movers...)
	sort.Slice(drivers, func(i, j int) bool { return abs(drivers[i].AfterHoursPct) > abs(drivers[j].AfterHoursPct) })
	edges := []ReadAcrossEdge{}
	for _, d := range drivers {
		if abs(d.AfterHoursPct) < 2 {
			continue
		}
		sector := sm.Sector(d.Symbol)
		peers := append([]Mover(nil), bySector[sector]...)
		sort.Slice(peers, func(i, j int) bool { return peers[i].Symbol < peers[j].Symbol })
		count := 0
		for _, p := range peers {
			if p.Symbol == d.Symbol {
				continue
			}
			dir := "positive"
			if d.AfterHoursPct < 0 {
				dir = "negative"
			}
			conf := 0.35 + abs(d.AfterHoursPct)/10
			if conf > 0.95 {
				conf = 0.95
			}
			edges = append(edges, ReadAcrossEdge{
				Driver:              d.Symbol,
				Peer:                p.Symbol,
				SectorID:            sector,
				SectorLabel:         sm.Label(sector),
				Direction:           dir,
				Confidence:          conf,
				Lag:                 "T+1 open",
				DriverAfterHoursPct: d.AfterHoursPct,
				Note:                fmt.Sprintf("%s 盘后 %.2f%%，同板块观察 %s", d.Symbol, d.AfterHoursPct, p.Symbol),
			})
			count++
			if count == 3 {
				break
			}
		}
	}
	return edges
}
