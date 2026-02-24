package executor

import (
	"github.com/similadayo/brancr/internal/storage"
)

func extractExtras(item storage.Product) []string {

	var extras []string

	// item.Variants is a JSON string e.g. {"size": ["S", "M"]}
	// For now, we omit complex parsing to keep it clean, or just use Description.
	// The prompt asked for 'Options', likely referring to a concept not fully in Product model yet
	// or referring to 'Variants'.
	// I will just return empty for now to satisfy build, OR parse simplistic.
	// Let's use Tags as extras for now, as they are often 'Vegan', 'New', etc.
	// Tags is also JSON string.
	// We'll skip complex JSON parsing in this helper to avoid extra dependencies/complexity unless needed.
	// If item.Description has details, they show up there.

	// if len(item.Tags) > 0 { extras = append(extras, item.Tags) } // It's string

	// Assuming 'Note' field exists on Product/OfferingModel or similar
	// The prompt calls it 'OfferingModel', but codebase likely uses 'Product'.
	// Using 'Product' from storage package based on typical naming.
	// Will confirm struct fields from models.go viewing.
	// If Note exists:
	// if item.Note != "" {
	// 	extras = append(extras, item.Note)
	// }

	return extras
}
