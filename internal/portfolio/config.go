package portfolio

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// PortfolioConfig is the top-level config loaded from configs/portfolio.yaml.
type PortfolioConfig struct {
	Concentration Config
	Greeks        GreeksConfig
	IVStaleness   IVStalenessConfig
	SectorsFile   string
	Stress        StressConfig
}

type GreeksConfig struct {
	RiskFreeRate float64
}

type IVStalenessConfig struct {
	FreshMax      time.Duration
	AcceptableMax time.Duration
	StaleMax      time.Duration
}

type StressConfig struct {
	Scenarios []StressScenario
}

func DefaultPortfolioConfig() PortfolioConfig {
	return PortfolioConfig{
		Concentration: DefaultConfig(),
		Greeks:        GreeksConfig{RiskFreeRate: 0.043},
		IVStaleness: IVStalenessConfig{
			FreshMax:      4 * time.Hour,
			AcceptableMax: 24 * time.Hour,
			StaleMax:      168 * time.Hour,
		},
		SectorsFile: "./configs/sectors.json",
		Stress:      StressConfig{Scenarios: DefaultStressScenarios()},
	}
}

// LoadConfig parses the small portfolio.yaml schema. A missing file is not an
// error: commands keep working from embedded/default values in release bundles.
func LoadConfig(path string) (PortfolioConfig, error) {
	cfg := DefaultPortfolioConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read portfolio config: %w", err)
	}
	if err := parsePortfolioYAML(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parse portfolio config: %w", err)
	}
	return cfg, nil
}

func parsePortfolioYAML(s string, cfg *PortfolioConfig) error {
	section := ""
	inScenarios := false
	var current *StressScenario
	for _, raw := range strings.Split(s, "\n") {
		line := stripYAMLComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trim := strings.TrimSpace(line)
		if indent == 0 {
			if strings.HasPrefix(trim, "sectors_file:") {
				cfg.SectorsFile = parseYAMLString(valueAfterColon(trim))
				section = ""
				inScenarios = false
				current = nil
				continue
			}
			if strings.HasSuffix(trim, ":") {
				section = strings.TrimSuffix(trim, ":")
				switch section {
				case "concentration", "greeks", "iv_staleness", "stress":
				default:
					return fmt.Errorf("unknown top-level portfolio config key %q", section)
				}
				inScenarios = false
				current = nil
				continue
			}
			if strings.Contains(trim, ":") {
				key, _ := splitKV(trim)
				return fmt.Errorf("unknown top-level portfolio config key %q", key)
			}
		}
		if section == "stress" && indent == 2 && trim == "scenarios:" {
			cfg.Stress.Scenarios = nil
			inScenarios = true
			continue
		}
		if section == "stress" && inScenarios {
			if indent == 4 && strings.HasPrefix(trim, "- ") {
				sc := StressScenario{}
				cfg.Stress.Scenarios = append(cfg.Stress.Scenarios, sc)
				current = &cfg.Stress.Scenarios[len(cfg.Stress.Scenarios)-1]
				kv := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
				if kv != "" {
					if err := applyScenarioKV(current, kv); err != nil {
						return err
					}
				}
				continue
			}
			if current == nil {
				continue
			}
			if indent == 6 && trim == "shocks:" {
				continue
			}
			if indent == 6 && strings.Contains(trim, ":") {
				if err := applyScenarioKV(current, trim); err != nil {
					return err
				}
				continue
			}
			if indent == 8 && strings.HasPrefix(trim, "- ") {
				current.Shocks = append(current.Shocks, StressShock{})
				kv := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
				if kv != "" {
					if err := applyShockKV(&current.Shocks[len(current.Shocks)-1], kv); err != nil {
						return err
					}
				}
				continue
			}
			if indent == 10 && len(current.Shocks) > 0 && strings.Contains(trim, ":") {
				if err := applyShockKV(&current.Shocks[len(current.Shocks)-1], trim); err != nil {
					return err
				}
				continue
			}
		}
		if !strings.Contains(trim, ":") {
			continue
		}
		key, val := splitKV(trim)
		switch section {
		case "concentration":
			if err := applyConcentrationKV(&cfg.Concentration, key, val); err != nil {
				return err
			}
		case "greeks":
			if key == "risk_free_rate" {
				f, err := strconv.ParseFloat(val, 64)
				if err != nil {
					return err
				}
				cfg.Greeks.RiskFreeRate = f
			} else {
				return fmt.Errorf("unknown greeks key %q", key)
			}
		case "iv_staleness":
			d, err := time.ParseDuration(parseYAMLString(val))
			if err != nil {
				return err
			}
			switch key {
			case "fresh_max":
				cfg.IVStaleness.FreshMax = d
			case "acceptable_max":
				cfg.IVStaleness.AcceptableMax = d
			case "stale_max":
				cfg.IVStaleness.StaleMax = d
			default:
				return fmt.Errorf("unknown iv_staleness key %q", key)
			}
		case "stress":
			return fmt.Errorf("unknown stress key %q", key)
		}
	}
	if len(cfg.Stress.Scenarios) == 0 {
		cfg.Stress.Scenarios = DefaultStressScenarios()
	}
	return validatePortfolioConfig(*cfg)
}

func stripYAMLComment(s string) string {
	inQuote := false
	for i, r := range s {
		if r == '"' {
			inQuote = !inQuote
		}
		if r == '#' && !inQuote {
			return s[:i]
		}
	}
	return s
}

func valueAfterColon(s string) string { _, v := splitKV(s); return v }
func splitKV(s string) (string, string) {
	parts := strings.SplitN(s, ":", 2)
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
func parseYAMLString(s string) string { return strings.Trim(strings.TrimSpace(s), "\"") }

func applyConcentrationKV(c *Config, key, val string) error {
	switch key {
	case "top_n":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		c.TopN = n
	case "warn_pct", "red_pct", "top2_warn_pct", "top5_warn_pct", "hhi_diversified_max", "hhi_concentrated_min":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}
		switch key {
		case "warn_pct":
			c.WarnPct = f
		case "red_pct":
			c.RedPct = f
		case "top2_warn_pct":
			c.Top2WarnPct = f
		case "top5_warn_pct":
			c.Top5WarnPct = f
		case "hhi_diversified_max":
			c.HHIDiversifiedMax = f
		case "hhi_concentrated_min":
			c.HHIConcentratedMin = f
		}
	default:
		return fmt.Errorf("unknown concentration key %q", key)
	}
	return nil
}

func applyScenarioKV(sc *StressScenario, kv string) error {
	key, val := splitKV(kv)
	switch key {
	case "id":
		sc.ID = parseYAMLString(val)
	case "label":
		sc.Label = parseYAMLString(val)
	default:
		return fmt.Errorf("unknown stress scenario key %q", key)
	}
	return nil
}

func applyShockKV(sh *StressShock, kv string) error {
	key, val := splitKV(kv)
	switch key {
	case "axis":
		sh.Axis = parseYAMLString(val)
	case "magnitude":
		f, err := strconv.ParseFloat(strings.TrimPrefix(val, "+"), 64)
		if err != nil {
			return err
		}
		sh.Magnitude = f
	default:
		return fmt.Errorf("unknown stress shock key %q", key)
	}
	return nil
}

func validatePortfolioConfig(cfg PortfolioConfig) error {
	allowedAxes := map[string]bool{"spy_pct": true, "qqq_pct": true, "underlying_pct": true, "iv_points": true}
	for i, sc := range cfg.Stress.Scenarios {
		if sc.ID == "" {
			return fmt.Errorf("stress scenario %d missing id", i)
		}
		if sc.Label == "" {
			return fmt.Errorf("stress scenario %q missing label", sc.ID)
		}
		if len(sc.Shocks) == 0 {
			return fmt.Errorf("stress scenario %q has no shocks", sc.ID)
		}
		for j, sh := range sc.Shocks {
			if !allowedAxes[sh.Axis] {
				return fmt.Errorf("stress scenario %q shock %d has unsupported axis %q", sc.ID, j, sh.Axis)
			}
			if sh.Magnitude == 0 || math.IsNaN(sh.Magnitude) || math.IsInf(sh.Magnitude, 0) {
				return fmt.Errorf("stress scenario %q shock %d has invalid magnitude %v", sc.ID, j, sh.Magnitude)
			}
		}
	}
	return nil
}
