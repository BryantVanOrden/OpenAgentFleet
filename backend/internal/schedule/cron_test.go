package schedule

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string) *Schedule {
	t.Helper()
	s, err := Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	return s
}

// at builds a UTC instant; the scheduler always matches in UTC.
func at(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func TestParseRejectsMalformed(t *testing.T) {
	bad := []string{
		"",
		"* * * *",      // four fields
		"* * * * * *",  // six fields
		"60 * * * *",   // minute out of range
		"* 24 * * *",   // hour out of range
		"* * 0 * *",    // day-of-month is 1-based
		"* * * 13 *",   // month out of range
		"* * * * 8",    // day-of-week is 0-7
		"*/0 * * * *",  // zero step never advances
		"*/-1 * * * *", // negative step
		"5-1 * * * *",  // backwards range
		"a * * * *",    // not a number
		"1,,2 * * * *", // empty list entry
		"* * * * mon",  // names are not supported, and must not parse as 0
		"@daily",       // shorthands are not supported
	}
	for _, expr := range bad {
		if s, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q) = %v, want an error", expr, s)
		}
	}
}

// Every-fifteen-minutes is the single most common non-trivial expression, and
// the one a naive parser gets wrong by firing every minute.
func TestStepMinutes(t *testing.T) {
	s := mustParse(t, "*/15 * * * *")
	for m := 0; m < 60; m++ {
		want := m%15 == 0
		if got := s.Matches(at(2026, time.March, 4, 9, m)); got != want {
			t.Errorf("*/15 at minute %d = %v, want %v", m, got, want)
		}
	}
}

// A step with a lower bound is open-ended to the top of the field: 10/15 is
// minutes 10, 25, 40 and 55 -- not just 10 and 25.
func TestStepFromOffset(t *testing.T) {
	s := mustParse(t, "10/15 * * * *")
	for _, m := range []int{10, 25, 40, 55} {
		if !s.Matches(at(2026, time.March, 4, 9, m)) {
			t.Errorf("10/15 should match minute %d", m)
		}
	}
	for _, m := range []int{0, 9, 11, 24, 59} {
		if s.Matches(at(2026, time.March, 4, 9, m)) {
			t.Errorf("10/15 should not match minute %d", m)
		}
	}
}

// A bounded range with a step stops at the range, unlike the open form above.
func TestBoundedRangeWithStep(t *testing.T) {
	s := mustParse(t, "0-30/10 * * * *")
	for _, m := range []int{0, 10, 20, 30} {
		if !s.Matches(at(2026, time.March, 4, 9, m)) {
			t.Errorf("0-30/10 should match minute %d", m)
		}
	}
	for _, m := range []int{40, 50} {
		if s.Matches(at(2026, time.March, 4, 9, m)) {
			t.Errorf("0-30/10 should not match minute %d past the range", m)
		}
	}
}

func TestCommaLists(t *testing.T) {
	s := mustParse(t, "0,30 9,17 * * *")
	hits := []struct {
		h, m int
		want bool
	}{
		{9, 0, true}, {9, 30, true}, {17, 0, true}, {17, 30, true},
		{9, 15, false}, {10, 0, false}, {18, 30, false},
	}
	for _, c := range hits {
		if got := s.Matches(at(2026, time.March, 4, c.h, c.m)); got != c.want {
			t.Errorf("0,30 9,17 at %02d:%02d = %v, want %v", c.h, c.m, got, c.want)
		}
	}

	// Lists mixing single values, ranges and steps in one field.
	mixed := mustParse(t, "1,5-7,*/20 * * * *")
	for _, m := range []int{0, 1, 5, 6, 7, 20, 40} {
		if !mixed.Matches(at(2026, time.March, 4, 0, m)) {
			t.Errorf("mixed list should match minute %d", m)
		}
	}
	for _, m := range []int{2, 8, 19, 41} {
		if mixed.Matches(at(2026, time.March, 4, 0, m)) {
			t.Errorf("mixed list should not match minute %d", m)
		}
	}
}

func TestDayOfWeek(t *testing.T) {
	// 2026-03-02 is a Monday.
	monday := at(2026, time.March, 2, 3, 0)
	s := mustParse(t, "0 3 * * 1")
	if !s.Matches(monday) {
		t.Error("dow=1 should match Monday")
	}
	for d := 3; d <= 8; d++ { // Tue..Sun
		if s.Matches(at(2026, time.March, d, 3, 0)) {
			t.Errorf("dow=1 should not match March %d", d)
		}
	}

	// Sunday is reachable as both 0 and 7.
	sunday := at(2026, time.March, 1, 3, 0)
	if !mustParse(t, "0 3 * * 0").Matches(sunday) {
		t.Error("dow=0 should match Sunday")
	}
	if !mustParse(t, "0 3 * * 7").Matches(sunday) {
		t.Error("dow=7 should match Sunday too")
	}

	// Weekday range.
	weekdays := mustParse(t, "0 9 * * 1-5")
	if !weekdays.Matches(at(2026, time.March, 6, 9, 0)) { // Friday
		t.Error("1-5 should match Friday")
	}
	if weekdays.Matches(at(2026, time.March, 7, 9, 0)) { // Saturday
		t.Error("1-5 should not match Saturday")
	}
}

// When both day fields are narrowed, cron ORs them. Getting this wrong makes a
// trigger fire far less often than the operator asked for, silently.
func TestDayOfMonthAndDayOfWeekAreOred(t *testing.T) {
	s := mustParse(t, "0 0 1 * 1")

	if !s.Matches(at(2026, time.April, 1, 0, 0)) { // a Wednesday, but the 1st
		t.Error("should fire on the 1st even though it is not Monday")
	}
	if !s.Matches(at(2026, time.April, 6, 0, 0)) { // a Monday, not the 1st
		t.Error("should fire on Monday even though it is not the 1st")
	}
	if s.Matches(at(2026, time.April, 7, 0, 0)) { // Tuesday the 7th
		t.Error("should not fire on a day that is neither")
	}

	// With only one of the two narrowed, the other must not veto.
	domOnly := mustParse(t, "0 0 15 * *")
	if !domOnly.Matches(at(2026, time.April, 15, 0, 0)) {
		t.Error("dom-only schedule should fire on the 15th whatever weekday it is")
	}
	if domOnly.Matches(at(2026, time.April, 16, 0, 0)) {
		t.Error("dom-only schedule should not fire on the 16th")
	}
}

func TestNightlyExpression(t *testing.T) {
	s := mustParse(t, "0 2 * * *")
	if !s.Matches(at(2026, time.March, 4, 2, 0)) {
		t.Error("nightly should fire at 02:00")
	}
	for _, c := range [][2]int{{2, 1}, {1, 0}, {3, 0}, {14, 0}} {
		if s.Matches(at(2026, time.March, 4, c[0], c[1])) {
			t.Errorf("nightly should not fire at %02d:%02d", c[0], c[1])
		}
	}
}

func TestMonthField(t *testing.T) {
	s := mustParse(t, "0 0 1 1,7 *")
	if !s.Matches(at(2026, time.January, 1, 0, 0)) {
		t.Error("should fire on 1 January")
	}
	if !s.Matches(at(2026, time.July, 1, 0, 0)) {
		t.Error("should fire on 1 July")
	}
	if s.Matches(at(2026, time.June, 1, 0, 0)) {
		t.Error("should not fire on 1 June")
	}
}

// Seconds are not part of cron's resolution: the scheduler ticks on the minute
// and must still recognise a trigger if the tick lands late.
func TestSecondsAreIgnored(t *testing.T) {
	s := mustParse(t, "30 4 * * *")
	late := time.Date(2026, time.March, 4, 4, 30, 47, 0, time.UTC)
	if !s.Matches(late) {
		t.Error("a tick 47 seconds into the matching minute should still match")
	}
}

func TestExprRoundTrips(t *testing.T) {
	s := mustParse(t, "  */15   2 * * 1-5 ")
	if s.Expr() != "*/15 2 * * 1-5" {
		t.Errorf("Expr() = %q, want the normalised expression", s.Expr())
	}
}

func TestNilScheduleNeverMatches(t *testing.T) {
	var s *Schedule
	if s.Matches(time.Now()) {
		t.Error("a nil schedule must not fire")
	}
}
