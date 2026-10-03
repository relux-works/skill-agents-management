package codex

import "unicode/utf8"

// exactCatalogStringsValid checks spelling, not decoded values. It scans all
// string tokens (keys and values, including unknown fields) before Token can
// normalize invalid UTF-8 or lone surrogates. The existing token reader still
// owns JSON structure and the only semantic parse. This linear scan shares
// the catalog's 4 MiB input bound and allocates no decoded strings.
func exactCatalogStringsValid(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		var ok bool
		i, ok = exactCatalogStringEnd(data, i+1)
		if !ok {
			return false
		}
	}
	return true
}

// exactCatalogStringEnd returns the closing quote's offset. A high surrogate
// must be immediately followed by a low surrogate \u escape; lone lows and
// reversed pairs are invalid. A literal U+FFFD and valid surrogate pairs are
// preserved, as are every ordinary JSON escape and raw Unicode scalar.
func exactCatalogStringEnd(data []byte, start int) (int, bool) {
	for i := start; i < len(data); i++ {
		switch c := data[i]; {
		case c == '"':
			return i, true
		case c < 0x20:
			return 0, false
		case c == '\\':
			i++
			if i == len(data) {
				return 0, false
			}
			switch data[i] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				word, ok := exactCatalogHexWord(data, i+1)
				if !ok {
					return 0, false
				}
				i += 4
				if word >= 0xd800 && word <= 0xdbff {
					if i+2 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
						return 0, false
					}
					low, ok := exactCatalogHexWord(data, i+3)
					if !ok || low < 0xdc00 || low > 0xdfff {
						return 0, false
					}
					i += 6
				} else if word >= 0xdc00 && word <= 0xdfff {
					return 0, false
				}
			default:
				return 0, false
			}
		}
	}
	return 0, false
}

func exactCatalogHexWord(data []byte, start int) (uint16, bool) {
	if len(data)-start < 4 {
		return 0, false
	}
	var word uint16
	for _, c := range data[start : start+4] {
		var digit byte
		switch {
		case c >= '0' && c <= '9':
			digit = c - '0'
		case c >= 'a' && c <= 'f':
			digit = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			digit = c - 'A' + 10
		default:
			return 0, false
		}
		word = word<<4 | uint16(digit)
	}
	return word, true
}
