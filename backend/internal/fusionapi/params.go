package fusionapi

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

// TimeRange is a closed interval of wall-clock time.
type TimeRange struct{ From, To time.Time }

// Range limits.
const (
	DefaultWindow = time.Hour
	MaxWindow     = 31 * 24 * time.Hour
)

// The longest range one query to a store takes under the limits the chart leaves at the stores' defaults: Tempo's
// query_frontend.search.max_duration is 168h and Loki's limits_config.max_query_length 721h. A longer range is refused here
// with a message that says so, rather than as the store's own.
const (
	MaxSearchWindow = 7 * 24 * time.Hour
	MaxLogWindow    = 30 * 24 * time.Hour
)

// within refuses a range longer than a store reads at once.
func (tr TimeRange) within(store string, max time.Duration) error {
	if tr.To.Sub(tr.From) > max {
		return badRequest("%s reads at most %d days of time range in one query; narrow from and to", store, int(max.Hours()/24))
	}
	return nil
}

// newest is the last max of the range: what a sample of the newest items reads from.
func (tr TimeRange) newest(max time.Duration) TimeRange {
	if tr.To.Sub(tr.From) > max {
		tr.From = tr.To.Add(-max)
	}
	return tr
}

// ParseRange reads the from/to parameters: each is an RFC 3339 time, unix seconds, or "now" / "now-<duration>"
// (for example now-15m). Missing "to" is now; missing "from" is DefaultWindow before "to".
func ParseRange(from, to string, now time.Time) (TimeRange, error) {
	t2 := now
	if to != "" {
		t, err := parseTime(to, now)
		if err != nil {
			return TimeRange{}, badRequest("to: %v", err)
		}
		t2 = t
	}
	t1 := t2.Add(-DefaultWindow)
	if from != "" {
		t, err := parseTime(from, now)
		if err != nil {
			return TimeRange{}, badRequest("from: %v", err)
		}
		t1 = t
	}
	if !t1.Before(t2) {
		return TimeRange{}, badRequest("from must be before to")
	}
	if t2.Sub(t1) > MaxWindow {
		return TimeRange{}, badRequest("the time range is longer than %d days", int(MaxWindow.Hours()/24))
	}
	return TimeRange{From: t1.UTC(), To: t2.UTC()}, nil
}

// parseDuration is time.ParseDuration that also takes days and weeks (7d, 2w, 1w3d12h), which the 31-day range makes the
// natural unit; "d" is 24 hours and "w" is 7 days, as they are on a calendar without daylight saving.
func parseDuration(s string) (time.Duration, error) {
	var days time.Duration
	for _, unit := range []struct {
		suffix string
		per    time.Duration
	}{{"w", 7 * 24 * time.Hour}, {"d", 24 * time.Hour}} {
		if i := strings.Index(s, unit.suffix); i > 0 {
			n, err := strconv.ParseUint(s[:i], 10, 12) // (at most 4095, so the sum cannot overflow)
			if err != nil {
				return 0, err
			}
			days += time.Duration(n) * unit.per
			s = s[i+1:]
		}
	}
	if s == "" && days > 0 {
		return days, nil
	}
	d, err := time.ParseDuration(s)
	return days + d, err
}

// ParseTime reads a time in the forms from and to take: RFC 3339, unix seconds, now, or now-<duration>.
func ParseTime(s string, now time.Time) (time.Time, error) { return parseTime(s, now) }

func parseTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "now":
		return now, nil
	case strings.HasPrefix(s, "now-"):
		d, err := parseDuration(strings.TrimPrefix(s, "now-"))
		if err != nil || d < 0 {
			return time.Time{}, errors.New("not a time (use RFC 3339, unix seconds, or now-<duration> such as now-15m or now-7d)")
		}
		return now.Add(-d), nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		switch {
		case math.IsInf(f, 0) || math.IsNaN(f) || f >= 1e12:
			return time.Time{}, errors.New("not a time in unix seconds (a value this large looks like milliseconds; divide by 1000)")
		}
		return time.Unix(0, int64(f*1e9)), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Time{}, errors.New("not a time (use RFC 3339, unix seconds, or now-<duration> such as now-15m or now-7d)")
}

// Limit parses a result-count parameter: def when absent, never above max.
func Limit(s string, def, max int) (int, error) {
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, badRequest("limit must be a whole number from 1 to %d", max)
	}
	if n > max {
		n = max
	}
	return n, nil
}

func unixSec(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

func unixNano(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }

func unixFloat(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', 3, 64)
}

// DurationParam reads a duration parameter written as Go ("30s", "2m") or as plain seconds.
// maxDurationSeconds keeps a duration given as plain seconds far below what time.Duration can hold (about 292 years), where
// the conversion would wrap around to a negative one.
const maxDurationSeconds = 366 * 86400

func DurationParam(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if f < 0 || math.IsNaN(f) {
			return 0, badRequest("a duration cannot be negative")
		}
		if f > maxDurationSeconds {
			return 0, badRequest("a duration cannot be longer than %d days", int(maxDurationSeconds/86400))
		}
		return time.Duration(f * float64(time.Second)), nil
	}
	d, err := parseDuration(s)
	if err != nil || d < 0 {
		return 0, badRequest("%q is not a duration (30s, 2m, 1d, or seconds)", s)
	}
	return d, nil
}
