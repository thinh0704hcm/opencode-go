package config

// stripJSONC removes // line comments, /* */ block comments, and trailing
// commas from JSONC source, preserving any such characters that appear inside
// string literals (architecture §3.3). The result is plain JSON suitable for
// encoding/json.
func stripJSONC(src []byte) []byte {
	var out []byte
	inString := false
	escaped := false
	n := len(src)
	for i := 0; i < n; i++ {
		c := src[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		// Not inside a string literal.
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < n && src[i+1] == '/' {
			// Line comment: skip to end of line.
			i += 2
			for i < n && src[i] != '\n' {
				i++
			}
			if i < n {
				out = append(out, src[i]) // keep the newline
			}
			continue
		}
		if c == '/' && i+1 < n && src[i+1] == '*' {
			// Block comment: skip to closing */.
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++ // loop's i++ skips the final '/'
			continue
		}
		out = append(out, c)
	}
	return removeTrailingCommas(out)
}

// removeTrailingCommas drops any comma that is followed (ignoring whitespace)
// by a closing } or ], outside of string literals.
func removeTrailingCommas(src []byte) []byte {
	var out []byte
	inString := false
	escaped := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(src) {
				switch src[j] {
				case ' ', '\t', '\n', '\r':
					j++
					continue
				}
				break
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue // drop the trailing comma
			}
		}
		out = append(out, c)
	}
	return out
}
