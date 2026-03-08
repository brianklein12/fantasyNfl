// Package parser provides streaming CSV parsers for each NFL stats file type.
// Every parser reads one row at a time to keep memory usage flat regardless
// of file size, and emits parsed records through a callback.
package parser

import (
	"encoding/csv"
	"io"
	"strconv"
)

// colIndex maps column name → zero-based column index from a CSV header row.
func colIndex(headers []string) map[string]int {
	m := make(map[string]int, len(headers))
	for i, h := range headers {
		m[h] = i
	}
	return m
}

// field returns the value at column name col, or "" if the column is absent.
func field(row []string, idx map[string]int, col string) string {
	i, ok := idx[col]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}

// num parses a float64 from a CSV cell, returning 0 on blank or parse error.
func num(row []string, idx map[string]int, col string) float64 {
	v := field(row, idx, col)
	if v == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

// newCSVReader wraps an io.Reader in a csv.Reader with consistent settings.
func newCSVReader(r io.Reader) *csv.Reader {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate rows with differing column counts
	cr.LazyQuotes = true
	cr.ReuseRecord = true // re-use backing array for lower GC pressure
	return cr
}
