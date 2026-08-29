// Package schedule parses the five-field cron expressions used by the
// orchestrator's trigger engine.
//
// This is deliberately a few hundred lines rather than a dependency: the
// scheduler only ever asks "does this expression match the minute that just
// started", which needs matching, not the general next-fire-time search a full
// cron library provides. The supported syntax is the portable subset every
// operator already knows -- `*`, a number, `a-b`, `*/n`, `a-b/n` and comma
// lists -- and anything outside it is rejected at parse time so a typo in the
// admin panel fails there rather than silently never firing.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed cron expression. The five fields are held as bitsets
// because matching happens once a minute for every trigger, and a set lookup
// keeps that O(1) regardless of how wide the expression is.
type Schedule struct {
	minute uint64 // bits 0-59
	hour   uint64 // bits 0-23
	dom    uint64 // bits 1-31
	month  uint64 // bits 1-12
	dow    uint64 // bits 0-6, Sunday = 0

	// domRestricted/dowRestricted record whether the day-of-month and
	// day-of-week fields were written as anything other than "*". Cron's day
	// rule depends on it: when both are restricted the two are OR'd, so
	// "0 0 1 * 1" fires on the first of the month AND on every Monday. When
	// only one is restricted the other must not veto it.
	domRestricted bool
	dowRestricted bool

	expr string
}

// Expr returns the expression this schedule was parsed from.
func (s *Schedule) Expr() string { return s.expr }

func (s *Schedule) String() string { return s.expr }

type fieldSpec struct {
	name     string
	min, max int
}

var cronFields = [5]fieldSpec{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day-of-month", 1, 31},
	{"month", 1, 12},
	{"day-of-week", 0, 7}, // 7 is accepted as an alias for Sunday and folded to 0
}

// Parse turns "m h dom mon dow" into a Schedule.
func Parse(expr string) (*Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression %q has %d fields, want 5 (m h dom mon dow)", expr, len(fields))
	}

	sets := [5]uint64{}
	for i, raw := range fields {
		set, err := parseField(raw, cronFields[i])
		if err != nil {
			return nil, err
		}
		sets[i] = set
	}

	return &Schedule{
		minute:        sets[0],
		hour:          sets[1],
		dom:           sets[2],
		month:         sets[3],
		dow:           sets[4],
		domRestricted: strings.TrimSpace(fields[2]) != "*",
		dowRestricted: strings.TrimSpace(fields[4]) != "*",
		expr:          strings.Join(fields, " "),
	}, nil
}

// Matches reports whether the expression fires during t's minute. Seconds and
// sub-second precision are ignored: cron's resolution is the minute, and the
// scheduler ticks on it.
func (s *Schedule) Matches(t time.Time) bool {
	if s == nil {
		return false
	}
	if !has(s.minute, t.Minute()) || !has(s.hour, t.Hour()) || !has(s.month, int(t.Month())) {
		return false
	}

	domHit := has(s.dom, t.Day())
	dowHit := has(s.dow, int(t.Weekday()))
	switch {
	case s.domRestricted && s.dowRestricted:
		// Both narrowed: classic cron OR. Either one firing is enough.
		return domHit || dowHit
	case s.domRestricted:
		return domHit
	case s.dowRestricted:
		return dowHit
	default:
		return true
	}
}

func parseField(raw string, spec fieldSpec) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s field is empty", spec.name)
	}

	var set uint64
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return 0, fmt.Errorf("%s field %q has an empty list entry", spec.name, raw)
		}

		body, step := item, 1
		if slash := strings.Index(item, "/"); slash >= 0 {
			body = strings.TrimSpace(item[:slash])
			n, err := strconv.Atoi(strings.TrimSpace(item[slash+1:]))
			if err != nil || n <= 0 {
				return 0, fmt.Errorf("%s field %q has an invalid step", spec.name, item)
			}
			step = n
		}

		lo, hi, err := parseRange(body, spec)
		if err != nil {
			return 0, err
		}
		// "5/15" means "from 5 to the end of the field, every 15" -- an open
		// upper bound, unlike "5-20/15". This is what vixie cron does and what
		// an operator writing "*/15" is really writing a special case of.
		if step > 1 && !strings.Contains(body, "-") && body != "*" {
			hi = spec.max
		}
		for v := lo; v <= hi; v += step {
			set |= 1 << uint(normalize(v, spec))
		}
	}
	if set == 0 {
		return 0, fmt.Errorf("%s field %q matches nothing", spec.name, raw)
	}
	return set, nil
}

// parseRange resolves the value part of an item ("*", "7", or "3-9") to an
// inclusive bound pair.
func parseRange(body string, spec fieldSpec) (int, int, error) {
	if body == "*" {
		return spec.min, spec.max, nil
	}
	if dash := strings.Index(body, "-"); dash >= 0 {
		lo, err1 := parseValue(strings.TrimSpace(body[:dash]), spec)
		hi, err2 := parseValue(strings.TrimSpace(body[dash+1:]), spec)
		if err1 != nil {
			return 0, 0, err1
		}
		if err2 != nil {
			return 0, 0, err2
		}
		if lo > hi {
			return 0, 0, fmt.Errorf("%s range %q runs backwards", spec.name, body)
		}
		return lo, hi, nil
	}
	v, err := parseValue(body, spec)
	if err != nil {
		return 0, 0, err
	}
	return v, v, nil
}

func parseValue(s string, spec fieldSpec) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s field has a non-numeric value %q", spec.name, s)
	}
	if v < spec.min || v > spec.max {
		return 0, fmt.Errorf("%s value %d is outside %d-%d", spec.name, v, spec.min, spec.max)
	}
	return v, nil
}

// normalize folds day-of-week 7 onto 0 so "sunday" written either way lands on
// the same bit as time.Weekday reports.
func normalize(v int, spec fieldSpec) int {
	if spec.name == "day-of-week" && v == 7 {
		return 0
	}
	return v
}

func has(set uint64, v int) bool {
	if v < 0 || v > 63 {
		return false
	}
	return set&(1<<uint(v)) != 0
}
