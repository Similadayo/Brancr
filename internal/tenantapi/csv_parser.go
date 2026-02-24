package tenantapi

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
)

// parseCSVRows attempts to parse CSV/TSV content returning lines joined as "Name - Price" style
func parseCSVRows(r io.Reader) ([]string, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1
	var out []string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// heuristics: if last field parses as price, join as "name - price"
		if len(rec) == 0 {
			continue
		}
		last := strings.TrimSpace(rec[len(rec)-1])
		if v, _ := parsePrice(last); v != nil {
			name := strings.Join(rec[:len(rec)-1], " ")
			out = append(out, name+" - "+last)
			continue
		}
		// else join entire row as name
		out = append(out, strings.Join(rec, " "))
	}
	return out, nil
}

// ParseCSVFromBytes parses CSV content in bytes and returns joined lines (exported for job handlers)
func ParseCSVFromBytes(b []byte) ([]string, error) {
	return parseCSVRows(bytes.NewReader(b))
}
