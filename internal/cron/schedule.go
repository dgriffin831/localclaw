package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var macroSchedules = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@hourly":   "0 * * * *",
}

type scheduleKind int

const (
	scheduleKindCron scheduleKind = iota
	scheduleKindReboot
)

type parsedSchedule struct {
	kind scheduleKind

	minute fieldMatcher
	hour   fieldMatcher
	dom    fieldMatcher
	month  fieldMatcher
	dow    fieldMatcher

	domWildcard bool
	dowWildcard bool
}

type fieldMatcher struct {
	any    bool
	values map[int]struct{}
}

func ValidateSchedule(schedule string) error {
	_, _, err := normalizeAndParseSchedule(schedule)
	return err
}

func NormalizeSchedule(schedule string) (string, error) {
	normalized, _, err := normalizeAndParseSchedule(schedule)
	if err != nil {
		return "", err
	}
	return normalized, nil
}

func normalizeAndParseSchedule(schedule string) (string, parsedSchedule, error) {
	value := strings.TrimSpace(schedule)
	if value == "" {
		return "", parsedSchedule{}, fmt.Errorf("schedule is required")
	}

	if strings.HasPrefix(value, "@") {
		macro := strings.ToLower(value)
		if macro == "@reboot" {
			return macro, parsedSchedule{kind: scheduleKindReboot}, nil
		}
		expanded, ok := macroSchedules[macro]
		if !ok {
			return "", parsedSchedule{}, fmt.Errorf("schedule %q is not a supported macro", value)
		}
		parsed, err := parseCronFields(strings.Fields(expanded))
		if err != nil {
			return "", parsedSchedule{}, err
		}
		return macro, parsed, nil
	}

	fields := strings.Fields(value)
	if len(fields) != 5 {
		return "", parsedSchedule{}, fmt.Errorf("schedule must use 5-field cron format")
	}
	parsed, err := parseCronFields(fields)
	if err != nil {
		return "", parsedSchedule{}, err
	}
	return strings.Join(fields, " "), parsed, nil
}

func parseCronFields(fields []string) (parsedSchedule, error) {
	if len(fields) != 5 {
		return parsedSchedule{}, fmt.Errorf("schedule must use 5-field cron format")
	}

	ranges := [][2]int{
		{0, 59}, // minute
		{0, 23}, // hour
		{1, 31}, // day of month
		{1, 12}, // month
		{0, 7},  // day of week
	}

	parsed := parsedSchedule{kind: scheduleKindCron}
	matchers := []*fieldMatcher{&parsed.minute, &parsed.hour, &parsed.dom, &parsed.month, &parsed.dow}

	for idx, field := range fields {
		matcher, err := parseCronField(field, ranges[idx][0], ranges[idx][1])
		if err != nil {
			return parsedSchedule{}, fmt.Errorf("invalid cron field %d: %w", idx+1, err)
		}
		*matchers[idx] = matcher
	}

	parsed.domWildcard = strings.TrimSpace(fields[2]) == "*"
	parsed.dowWildcard = strings.TrimSpace(fields[4]) == "*"
	return parsed, nil
}

func parseCronField(field string, min, max int) (fieldMatcher, error) {
	value := strings.TrimSpace(field)
	if value == "" {
		return fieldMatcher{}, fmt.Errorf("field cannot be blank")
	}
	if value == "*" {
		return fieldMatcher{any: true}, nil
	}
	matcher := fieldMatcher{values: map[int]struct{}{}}
	for _, rawToken := range strings.Split(value, ",") {
		token := strings.TrimSpace(rawToken)
		if token == "" {
			return fieldMatcher{}, fmt.Errorf("empty token")
		}
		if err := addTokenValues(&matcher, token, min, max); err != nil {
			return fieldMatcher{}, err
		}
	}
	return matcher, nil
}

func addTokenValues(matcher *fieldMatcher, token string, min, max int) error {
	base := token
	step := 1
	hasStep := false

	if strings.Contains(token, "/") {
		parts := strings.Split(token, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid step syntax %q", token)
		}
		base = strings.TrimSpace(parts[0])
		if base == "" {
			return fmt.Errorf("invalid step base in %q", token)
		}
		parsedStep, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || parsedStep <= 0 {
			return fmt.Errorf("step must be a positive integer in %q", token)
		}
		step = parsedStep
		hasStep = true
	}

	rangeStart := min
	rangeEnd := max
	if base == "*" {
		// keep min/max
	} else if strings.Contains(base, "-") {
		parts := strings.Split(base, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid range %q", token)
		}
		start, err := parseCronInt(strings.TrimSpace(parts[0]), min, max)
		if err != nil {
			return err
		}
		end, err := parseCronInt(strings.TrimSpace(parts[1]), min, max)
		if err != nil {
			return err
		}
		if start > end {
			return fmt.Errorf("range start %d exceeds end %d in %q", start, end, token)
		}
		rangeStart = start
		rangeEnd = end
	} else {
		value, err := parseCronInt(base, min, max)
		if err != nil {
			return err
		}
		if !hasStep {
			matcher.values[value] = struct{}{}
			return nil
		}
		rangeStart = value
		rangeEnd = max
	}

	for current := rangeStart; current <= rangeEnd; current += step {
		matcher.values[current] = struct{}{}
	}
	return nil
}

func parseCronInt(value string, min, max int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("value %q must be an integer", value)
	}
	if parsed < min || parsed > max {
		return 0, fmt.Errorf("value %d is out of range [%d,%d]", parsed, min, max)
	}
	return parsed, nil
}

func (m fieldMatcher) match(value int) bool {
	if m.any {
		return true
	}
	_, ok := m.values[value]
	return ok
}

func (s parsedSchedule) next(after time.Time) (time.Time, bool) {
	if s.kind == scheduleKindReboot {
		return time.Time{}, false
	}
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(5, 0, 0)
	for !candidate.After(limit) {
		if s.matches(candidate) {
			return candidate, true
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, false
}

func (s parsedSchedule) matches(ts time.Time) bool {
	if s.kind == scheduleKindReboot {
		return false
	}

	if !s.minute.match(ts.Minute()) {
		return false
	}
	if !s.hour.match(ts.Hour()) {
		return false
	}
	if !s.month.match(int(ts.Month())) {
		return false
	}

	domMatch := s.dom.match(ts.Day())
	dowValue := int(ts.Weekday())
	dowMatch := s.dow.match(dowValue) || (dowValue == 0 && s.dow.match(7))

	if s.domWildcard && s.dowWildcard {
		return true
	}
	if s.domWildcard {
		return dowMatch
	}
	if s.dowWildcard {
		return domMatch
	}
	return domMatch || dowMatch
}
