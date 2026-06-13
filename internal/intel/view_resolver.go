package intel

import (
	"context"
	"fmt"
	"time"

	"github.com/IS908/optix/internal/marketdata"
)

const viewOverrideTimeout = 1500 * time.Millisecond

type viewOverride struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type viewResolution struct {
	BaseView marketdata.View
	View     marketdata.View
	Override *viewOverride
}

func (h *Handlers) resolveAutoView(ctx context.Context, now time.Time, phase Phase) viewResolution {
	base := ViewFor(phase)
	out := viewResolution{BaseView: base, View: base}

	if override, ok := h.resolveShockOverride(ctx); ok {
		out.View = marketdata.ViewShock
		out.Override = override
		return out
	}
	if override, ok := h.resolveEventOverride(ctx, now); ok {
		out.View = marketdata.ViewEvent
		out.Override = override
	}
	return out
}

func (h *Handlers) resolveShockOverride(ctx context.Context) (*viewOverride, bool) {
	if h.Shock == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, viewOverrideTimeout)
	defer cancel()
	regime, err := h.Shock.Regime(ctx)
	if err != nil || regime.TriggeredView != string(marketdata.ViewShock) {
		return nil, false
	}
	reason := fmt.Sprintf("shock regime: %s, score %.0f", regime.State, regime.Score)
	return &viewOverride{Source: "shock_regime", Reason: reason}, true
}

func (h *Handlers) resolveEventOverride(ctx context.Context, now time.Time) (*viewOverride, bool) {
	if h.Event == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, viewOverrideTimeout)
	defer cancel()
	events, _ := h.Event.EventDates(ctx)
	if len(events) == 0 {
		return nil, false
	}
	y, m, d := now.In(nyLoc).Date()
	for _, event := range events {
		ey, em, ed := event.Date.UTC().Date()
		if y == ey && m == em && d == ed {
			reason := event.Label
			if reason == "" {
				reason = event.Kind
			}
			if reason == "" {
				reason = "scheduled event"
			}
			return &viewOverride{Source: "event_calendar", Reason: reason}, true
		}
	}
	return nil, false
}
