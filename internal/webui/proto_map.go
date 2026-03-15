package webui

// proto_map.go: functions that convert proto analysis types → clean JSON/template types.
// Keeps proto dependencies out of handlers and response_types.go.

import (
	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	marketdatav1 "github.com/IS908/optix/gen/go/optix/marketdata/v1"
	"github.com/IS908/optix/pkg/model"
	"time"
)

func protoToAnalyzeResponse(resp *analysisv1.AnalyzeStockResponse, symbol string, live bool) *AnalyzeResponse {
	ar := &AnalyzeResponse{
		GeneratedAt: time.Now().UTC(),
		FromCache:   !live,
		Symbol:      symbol,
	}

	if s := resp.Summary; s != nil {
		ar.Summary = SummaryData{
			Price:        s.Price,
			Change:       s.Change,
			ChangePct:    s.ChangePct,
			High52W:      s.High_52W,
			Low52W:       s.Low_52W,
			TodayVolume:  s.TodayVolume,
			AvgVolume20D: s.AvgVolume_20D,
		}
	}

	if t := resp.Technical; t != nil {
		td := TechnicalData{
			Trend:            t.Trend,
			TrendScore:       t.TrendScore,
			TrendDescription: t.TrendDescription,
			MA20:             t.Ma_20,
			MA50:             t.Ma_50,
			MA200:            t.Ma_200,
			RSI14:            t.Rsi_14,
			MACD:             t.Macd,
			MACDSignal:       t.MacdSignal,
			MACDHistogram:    t.MacdHistogram,
			BollingerUpper:   t.BollingerUpper,
			BollingerMid:     t.BollingerMid,
			BollingerLower:   t.BollingerLower,
		}
		for _, sl := range t.SupportLevels {
			td.SupportLevels = append(td.SupportLevels, PriceLevelData{sl.Price, sl.Source, sl.Strength})
		}
		for _, rl := range t.ResistanceLevels {
			td.ResistanceLevels = append(td.ResistanceLevels, PriceLevelData{rl.Price, rl.Source, rl.Strength})
		}
		ar.Technical = td
	}

	if o := resp.Options; o != nil {
		od := OptionsData{
			IVCurrent:            o.IvCurrent,
			IVRank:               o.IvRank,
			IVPercentile:         o.IvPercentile,
			IVEnvironment:        o.IvEnvironment,
			IVSkew:               o.IvSkew,
			MaxPain:              o.MaxPain,
			MaxPainExpiry:        o.MaxPainExpiry,
			PCRVolume:            o.PcrVolume,
			PCROi:                o.PcrOi,
			EarningsBeforeExpiry: o.EarningsBeforeExpiry,
			NextEarningsDate:     o.NextEarningsDate,
		}
		for _, cl := range o.OiClusters {
			ot := "CALL"
			if cl.OptionType == marketdatav1.OptionType_OPTION_TYPE_PUT {
				ot = "PUT"
			}
			od.OIClusters = append(od.OIClusters, OIClusterData{
				Strike:       cl.Strike,
				OptionType:   ot,
				OpenInterest: cl.OpenInterest,
				Significance: cl.Significance,
			})
		}
		ar.Options = od
	}

	if ol := resp.Outlook; ol != nil {
		ar.Outlook = OutlookData{
			Direction:    ol.Direction,
			Confidence:   ol.Confidence,
			Rationale:    ol.Rationale,
			RangeLow1S:   ol.RangeLow_1S,
			RangeHigh1S:  ol.RangeHigh_1S,
			RangeLow2S:   ol.RangeLow_2S,
			RangeHigh2S:  ol.RangeHigh_2S,
			ForecastDays: ol.ForecastDays,
			RiskEvents:   ol.RiskEvents,
		}
	}

	for _, st := range resp.Strategies {
		sd := StrategyData{
			StrategyName:        st.StrategyName,
			StrategyType:        st.StrategyType,
			Score:               st.Score,
			MaxProfit:           st.MaxProfit,
			MaxLoss:             st.MaxLoss,
			RiskRewardRatio:     st.RiskRewardRatio,
			MarginRequired:      st.MarginRequired,
			ProbabilityOfProfit: st.ProbabilityOfProfit,
			BreakevenPrice:      st.BreakevenPrice,
			NetCredit:           st.NetCredit,
			Rationale:           st.Rationale,
			RiskWarnings:        st.RiskWarnings,
		}
		for _, leg := range st.Legs {
			ot := "CALL"
			if leg.OptionType == marketdatav1.OptionType_OPTION_TYPE_PUT {
				ot = "PUT"
			}
			sd.Legs = append(sd.Legs, StrategyLegData{
				OptionType: ot,
				Strike:     leg.Strike,
				Expiration: leg.Expiration,
				Quantity:   leg.Quantity,
				Premium:    leg.Premium,
			})
		}
		ar.Strategies = append(ar.Strategies, sd)
	}

	return ar
}

func snapToSymbolSummary(s model.QuickSummary) SymbolSummary {
	return SymbolSummary{
		Symbol:           s.Symbol,
		Price:            s.Price,
		Trend:            s.Trend,
		RSI:              s.RSI,
		IVRank:           s.IVRank,
		MaxPain:          s.MaxPain,
		PCR:              s.PCR,
		RangeLow1S:       s.RangeLow1S,
		RangeHigh1S:      s.RangeHigh1S,
		Recommendation:   s.Recommendation,
		OpportunityScore: s.OpportunityScore,
		SnapshotDate:     s.SnapshotDate,
	}
}
