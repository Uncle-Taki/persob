package domain

// TomanAmount uses integer Toman throughout comparisons. Rial remainders are
// truncated toward zero; original amounts/currency are retained for display/audit.
func TomanAmount(amount int64, currency Currency) int64 {
	if currency == CurrencyRial {
		return amount / 10
	}
	return amount
}
