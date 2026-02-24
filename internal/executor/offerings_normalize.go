package executor

import (
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/storage"
)

// WARNING:
// Offerings must be normalized BEFORE any AI or formatting.
// Never pass raw DB models forward.
// Violations leak internals and break human realism.

func normalizeOfferings(items []storage.Product) []domain.Offering {

	var offerings []domain.Offering

	for _, item := range items {

		o := domain.Offering{
			Title:       item.Name,
			Description: item.Description,
			PriceText:   formatPrice(int64(item.Price), item.Currency),
			Extras:      extractExtras(item),
		}

		if item.Price == 0 {
			o.PriceText = "Price available on request"
		}

		offerings = append(offerings, o)
	}

	return offerings
}
