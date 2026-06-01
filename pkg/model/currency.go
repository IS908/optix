package model

import "strings"

// NormalizeCurrency returns an uppercase currency code and treats missing
// broker currency as USD for legacy journal rows.
func NormalizeCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return "USD"
	}
	return currency
}
