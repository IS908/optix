package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IS908/optix/internal/broker/yfinance"
	"github.com/IS908/optix/pkg/model"
)

// PCRatio 是单标的最近到期期权链的 Put/Call 聚合（情绪卡用）。
type PCRatio struct {
	Underlying string    `json:"underlying"`
	Expiration string    `json:"expiration"`
	PutOI      int64     `json:"put_oi"`
	CallOI     int64     `json:"call_oi"`
	PutVol     int64     `json:"put_vol"`
	CallVol    int64     `json:"call_vol"`
	PCOI       float64   `json:"pc_oi"`  // putOI/callOI；callOI=0 → 0
	PCVol      float64   `json:"pc_vol"` // putVol/callVol；callVol=0 → 0
	AsOf       time.Time `json:"as_of"`
}

type rawContract struct {
	Volume       int64 `json:"volume"`
	OpenInterest int64 `json:"openInterest"`
}
type rawExpiration struct {
	Expiration string        `json:"expiration"`
	Calls      []rawContract `json:"calls"`
	Puts       []rawContract `json:"puts"`
}
type rawOptionChain struct {
	Underlying  string          `json:"underlying"`
	Expirations []rawExpiration `json:"expirations"`
}

// PutCallRatio 取 underlying 最近到期期权链，聚合 P/C（主用 open interest）。
func PutCallRatio(ctx context.Context, pythonBin, underlying string) (PCRatio, error) {
	raw, err := yfinance.RunFetcher(ctx, pythonBin, "option_chain", underlying)
	if err != nil {
		return PCRatio{}, fmt.Errorf("option_chain %s: %w", underlying, err)
	}
	return parsePutCallRatio(raw)
}

// OptionChainWithOI returns the raw yfinance option chain through the marketdata
// layer so Market Intel views do not import broker/yfinance directly.
func OptionChainWithOI(ctx context.Context, pythonBin, underlying, expiry string) (*model.OptionChain, error) {
	b := yfinance.New(yfinance.Config{PythonBin: pythonBin})
	return b.GetOptionChainWithOI(ctx, underlying, expiry)
}

func parsePutCallRatio(raw []byte) (PCRatio, error) {
	var oc rawOptionChain
	if err := json.Unmarshal(raw, &oc); err != nil {
		return PCRatio{}, fmt.Errorf("parse option_chain: %w", err)
	}
	if len(oc.Expirations) == 0 {
		return PCRatio{}, fmt.Errorf("no option expirations for %s", oc.Underlying)
	}
	exp := oc.Expirations[0]
	pc := PCRatio{Underlying: oc.Underlying, Expiration: exp.Expiration, AsOf: time.Now().UTC()}
	for _, c := range exp.Calls {
		pc.CallOI += c.OpenInterest
		pc.CallVol += c.Volume
	}
	for _, p := range exp.Puts {
		pc.PutOI += p.OpenInterest
		pc.PutVol += p.Volume
	}
	if pc.CallOI > 0 {
		pc.PCOI = float64(pc.PutOI) / float64(pc.CallOI)
	}
	if pc.CallVol > 0 {
		pc.PCVol = float64(pc.PutVol) / float64(pc.CallVol)
	}
	return pc, nil
}
