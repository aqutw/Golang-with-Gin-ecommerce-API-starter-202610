package catalog

// PriceMinor is always an integer in the currency's smallest unit.
type Product struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceMinor int64  `json:"price_minor"`
	Currency   string `json:"currency"`
	Active     bool   `json:"active"`
}
