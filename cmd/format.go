package cmd

import (
	"fmt"
	"time"
)

// sessionLong is bound to the persistent -L/--long flag. When true,
// displaySessionID returns full UUIDs instead of an 8-char prefix so the
// output can be piped to `claude --resume`.
var sessionLong bool

// displaySessionID returns the session ID truncated to 8 chars (the default)
// or the full UUID when -L/--long is set.
func displaySessionID(id string) string {
	if sessionLong {
		return id
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// relativeTime formats a timestamp as a human-friendly relative string.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 48*time.Hour:
		return "yesterday"
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	default:
		return t.Format("2006-01-02")
	}
}

// formatTime returns relative or absolute time based on the flag.
func formatTime(t time.Time, absolute bool) string {
	if absolute {
		if t.IsZero() {
			return "unknown"
		}
		return t.Format(time.RFC3339)
	}
	return relativeTime(t)
}
