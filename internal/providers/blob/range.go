package blob

import (
	"strconv"
	"strings"
)

// parseRange parses a single HTTP Range header against a known size.
// Returns (start, end) inclusive. Returns ok=false if the header is empty,
// malformed, multi-part, or unsatisfiable.
//
// Supported forms:
//
//	bytes=0-99    first 100 bytes
//	bytes=100-    from byte 100 to end
//	bytes=-100    last 100 bytes
func parseRange(header string, size int64) (int64, int64, bool) {
	if header == "" {
		return 0, 0, false
	}
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false // multipart ranges: not supported
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	startStr, endStr := parts[0], parts[1]

	if startStr == "" {
		// suffix range: -N means last N bytes
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	if endStr == "" {
		return start, size - 1, true
	}
	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil || end < start {
		return 0, 0, false
	}
	if end >= size {
		end = size - 1
	}
	return start, end, true
}
