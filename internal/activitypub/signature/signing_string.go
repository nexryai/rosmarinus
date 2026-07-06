package signature

import (
	"fmt"
	"net/url"
	"strings"
)

type Request struct {
	URL     string
	Method  string
	Headers map[string]string
}

func SigningString(req Request, includeHeaders []string) (string, error) {
	u, err := url.Parse(req.URL)
	if err != nil {
		return "", err
	}
	headers := lowerHeaders(req.Headers)
	lines := make([]string, 0, len(includeHeaders))
	for _, header := range includeHeaders {
		key := strings.ToLower(header)
		if key == "(request-target)" {
			lines = append(lines, fmt.Sprintf("(request-target): %s %s", strings.ToLower(req.Method), u.Path))
			continue
		}
		value, ok := headers[key]
		if !ok {
			return "", fmt.Errorf("missing signed header: %s", key)
		}
		lines = append(lines, fmt.Sprintf("%s: %s", key, value))
	}
	return strings.Join(lines, "\n"), nil
}

func lowerHeaders(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for key, value := range src {
		if key == "__proto__" {
			continue
		}
		dst[strings.ToLower(key)] = value
	}
	return dst
}
