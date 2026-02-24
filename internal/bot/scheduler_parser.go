package bot

import (
	"regexp"
	"strings"
	"time"
)

var (
	reSchedule = regexp.MustCompile(`(?i)schedule\s+(\S+)\s+for\s+(.+)`)
	reName     = regexp.MustCompile(`(?i)name\s+(?:this\s+)?carousel\s+(.+)`)
)

func parseScheduleIntent(msg string) (assetName string, when time.Time, ok bool) {
	m := reSchedule.FindStringSubmatch(msg)
	if len(m) < 3 {
		return "", time.Time{}, false
	}
	assetName = m[1]
	parsed, err := time.ParseInLocation("Jan 2 3:04pm 2006", m[2], time.Now().Location())
	if err != nil {
		// fallback: “tomorrow 10am”
		when = time.Now().Add(24 * time.Hour).Truncate(time.Hour).Add(10 * time.Hour)
	} else {
		when = parsed
	}
	return assetName, when, true
}

func parseNameIntent(msg string) (string, bool) {
	m := reName.FindStringSubmatch(strings.ToLower(msg))
	if len(m) < 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}
