package diffview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func pad(v any, width int) string {
	var s string
	switch n := v.(type) {
	case int:
		s = strconv.Itoa(n)
	default:
		s = fmt.Sprintf("%v", v)
	}
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}

func isEven(n int) bool {
	return n%2 == 0
}

func isOdd(n int) bool {
	return !isEven(n)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func ternary[T any](cond bool, t, f T) T {
	if cond {
		return t
	}
	return f
}
