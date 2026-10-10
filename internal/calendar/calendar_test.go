package calendar

import (
	"testing"
	"time"
)

func TestCountSeptember2026(t *testing.T) {
	from, to, _ := MonthRange("2026-09")
	r := Count(from, to, ParseWorkWeek(DefaultWorkWeek), nil)
	if r.WorkingDays != 22 || r.WeekDays != 22 {
		t.Fatalf("Sep 2026 should have 22 weekdays, got %+v", r)
	}
}

func TestHolidaysYearlyAndOneOff(t *testing.T) {
	from, to, _ := MonthRange("2026-05")
	hol := []Holiday{
		{Date: "2020-05-01", Name: "Praznik rada", Yearly: true}, // Fri 1 May 2026
		{Date: "2026-05-02", Name: "weekend holiday"},            // Saturday: must not count
		{Date: "2026-05-11", Name: "vacation"},                   // Monday
	}
	r := Count(from, to, ParseWorkWeek("1,2,3,4,5"), hol)
	if r.WeekDays != 21 {
		t.Fatalf("May 2026 weekdays = %d", r.WeekDays)
	}
	if r.WorkingDays != 19 || len(r.Holidays) != 2 {
		t.Fatalf("expected 19 working days and 2 holidays, got %+v", r)
	}
}

func TestWorkWeekParsing(t *testing.T) {
	w := ParseWorkWeek("1,2,3,4,5,6")
	if !w[time.Saturday] || w[time.Sunday] {
		t.Fatalf("unexpected week %v", w)
	}
	if w := ParseWorkWeek("garbage"); !w[time.Monday] || w[time.Saturday] {
		t.Fatalf("fallback week wrong: %v", w)
	}
	if w := ParseWorkWeek("7"); !w[time.Sunday] {
		t.Fatalf("ISO 7 should be Sunday")
	}
}

func TestOrthodoxEaster(t *testing.T) {
	cases := map[int]string{2024: "2024-05-05", 2025: "2025-04-20", 2026: "2026-04-12", 2027: "2027-05-02"}
	for y, want := range cases {
		if got := OrthodoxEaster(y).Format("2006-01-02"); got != want {
			t.Errorf("Orthodox Easter %d = %s, want %s", y, got, want)
		}
	}
	if got := GregorianEaster(2026).Format("2006-01-02"); got != "2026-04-05" {
		t.Errorf("Gregorian Easter 2026 = %s", got)
	}
}

func TestMergeAndClean(t *testing.T) {
	a := PresetHolidays("rs", 2026)
	b := PresetHolidays("rs", 2027) // same yearly dates, different year -> no duplicates
	m := Merge(a, b)
	if len(m) != len(a) {
		t.Fatalf("yearly duplicates not merged: %d", len(m))
	}
	m = Merge(m, PresetHolidays("rs-orthodox", 2026))
	if len(m) != len(a)+2 {
		t.Fatalf("movable feasts missing: %d", len(m))
	}
	c := Clean([]Holiday{{Date: "bad"}, {Date: "2026-01-05T00:00", Name: ""}})
	if len(c) != 1 || c[0].Date != "2026-01-05" || c[0].Name != "Day off" {
		t.Fatalf("clean: %+v", c)
	}
}
