// Package docx fills Word (.docx) templates with invoice data and converts them to PDF.
//
// Templates use a small Mustache-like syntax that survives Word's run splitting:
//
//	{{number}}  {{client.name}}  {{custom.pfr}}
//	{{#items}} … {{/items}}      repeat (table rows are repeated when the markers sit in a row)
//	{{#paid}} … {{/paid}}        show when truthy;  {{^paid}} … {{/paid}} show when falsy
package docx

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	sectionRe = regexp.MustCompile(`(?s)\{\{([#^])\s*([\w.]+)\s*\}\}(.*?)\{\{/\s*([\w.]+)\s*\}\}`)
	varRe     = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)
)

// Escaper converts a value to its textual representation in the output (e.g. XML escaping).
type Escaper func(string) string

// Render expands the template against the data stack. ctx[len-1] is the innermost scope.
func Render(tpl string, data map[string]any, esc Escaper) string {
	return render(tpl, []any{data}, esc)
}

func render(tpl string, stack []any, esc Escaper) string {
	// Sections first (innermost-first is achieved by recursion on the body).
	out := sectionRe.ReplaceAllStringFunc(tpl, func(m string) string {
		sub := sectionRe.FindStringSubmatch(m)
		kind, name, body, closer := sub[1], sub[2], sub[3], sub[4]
		if name != closer {
			return m // mismatched; leave as is
		}
		val, _ := lookup(stack, name)
		if kind == "^" {
			if !truthy(val) {
				return render(body, stack, esc)
			}
			return ""
		}
		switch v := val.(type) {
		case []map[string]any:
			var b strings.Builder
			for _, item := range v {
				b.WriteString(render(body, append(stack, item), esc))
			}
			return b.String()
		case []any:
			var b strings.Builder
			for _, item := range v {
				b.WriteString(render(body, append(stack, item), esc))
			}
			return b.String()
		case map[string]any:
			return render(body, append(stack, v), esc)
		default:
			if truthy(val) {
				return render(body, stack, esc)
			}
			return ""
		}
	})
	return varRe.ReplaceAllStringFunc(out, func(m string) string {
		name := varRe.FindStringSubmatch(m)[1]
		val, ok := lookup(stack, name)
		if !ok {
			return ""
		}
		return esc(str(val))
	})
}

func lookup(stack []any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	for i := len(stack) - 1; i >= 0; i-- {
		cur := stack[i]
		found := true
		for _, p := range parts {
			m, ok := cur.(map[string]any)
			if !ok {
				found = false
				break
			}
			v, ok := m[p]
			if !ok {
				found = false
				break
			}
			cur = v
		}
		if found {
			return cur, true
		}
	}
	return nil, false
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return strings.TrimSpace(x) != ""
	case []map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case int, int64, float64:
		return fmt.Sprint(x) != "0"
	}
	return true
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "yes"
		}
		return ""
	default:
		return fmt.Sprint(x)
	}
}
