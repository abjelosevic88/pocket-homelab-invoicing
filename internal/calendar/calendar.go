// Package calendar counts working days: a configurable work week minus
// public holidays and personal days off. It feeds recurring invoices that
// bill "working days of the month" and the month-end wizard.
package calendar

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Holiday is a non-working day. Yearly holidays repeat on the same month/day
// every year (Date keeps the year it was entered with); one-off days such as
// vacation or movable feasts are stored with their exact date.
type Holiday struct {
	Date   string `json:"date"` // YYYY-MM-DD
	Name   string `json:"name"`
	Yearly bool   `json:"yearly"`
}

// DayOff is a holiday that fell inside a counted range.
type DayOff struct {
	Date    string `json:"date"`
	Name    string `json:"name"`
	Weekday string `json:"weekday"`
}

// Result of a working-day count.
type Result struct {
	From        string   `json:"from"`
	To          string   `json:"to"`
	WorkingDays int      `json:"working_days"` // work-week days minus holidays
	WeekDays    int      `json:"week_days"`    // work-week days before holidays
	Holidays    []DayOff `json:"holidays"`     // holidays that fell on a work-week day
}

// DefaultWorkWeek is Monday to Friday in ISO numbering (1 = Monday, 7 = Sunday).
const DefaultWorkWeek = "1,2,3,4,5"

// ParseWorkWeek turns "1,2,3,4,5" into a weekday set. Invalid or empty input
// yields Monday–Friday.
func ParseWorkWeek(spec string) map[time.Weekday]bool {
	out := map[time.Weekday]bool{}
	for _, p := range strings.Split(spec, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 || n > 7 {
			continue
		}
		out[time.Weekday(n%7)] = true // ISO 7 (Sunday) -> Go 0
	}
	if len(out) == 0 {
		return ParseWorkWeek(DefaultWorkWeek)
	}
	return out
}

// Count counts working days between from and to inclusive.
func Count(from, to time.Time, week map[time.Weekday]bool, holidays []Holiday) Result {
	res := Result{From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Holidays: []DayOff{}}
	if to.Before(from) {
		return res
	}
	byDay := map[string]string{}  // "MM-DD" -> name (yearly)
	byDate := map[string]string{} // "YYYY-MM-DD" -> name
	for _, h := range holidays {
		d := strings.TrimSpace(h.Date)
		if len(d) < 10 {
			continue
		}
		if h.Yearly {
			if _, ok := byDay[d[5:10]]; !ok {
				byDay[d[5:10]] = h.Name
			}
		} else {
			byDate[d[:10]] = h.Name
		}
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if !week[d.Weekday()] {
			continue
		}
		res.WeekDays++
		key := d.Format("2006-01-02")
		name, off := byDate[key]
		if !off {
			name, off = byDay[key[5:]]
		}
		if off {
			res.Holidays = append(res.Holidays, DayOff{Date: key, Name: name, Weekday: d.Weekday().String()})
			continue
		}
		res.WorkingDays++
	}
	return res
}

// CountRange is Count for ISO date strings.
func CountRange(from, to string, week map[time.Weekday]bool, holidays []Holiday) (Result, error) {
	f, err := time.Parse("2006-01-02", from)
	if err != nil {
		return Result{}, fmt.Errorf("invalid from date %q", from)
	}
	t, err := time.Parse("2006-01-02", to)
	if err != nil {
		return Result{}, fmt.Errorf("invalid to date %q", to)
	}
	return Count(f, t, week, holidays), nil
}

// MonthRange returns the first and last day of a YYYY-MM month.
func MonthRange(ym string) (time.Time, time.Time, error) {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid month %q (want YYYY-MM)", ym)
	}
	return t, t.AddDate(0, 1, -1), nil
}

// OrthodoxEaster returns Easter Sunday of the Julian calendar expressed in the
// Gregorian calendar (Meeus' Julian algorithm plus the 13-day offset valid 1900–2099).
func OrthodoxEaster(year int) time.Time {
	a := year % 4
	b := year % 7
	c := year % 19
	d := (19*c + 15) % 30
	e := (2*a + 4*b - d + 34) % 7
	month := (d + e + 114) / 31
	day := (d+e+114)%31 + 1
	julian := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return julian.AddDate(0, 0, 13)
}

// GregorianEaster returns Western Easter Sunday (Anonymous Gregorian algorithm).
func GregorianEaster(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// Preset is a named list of public holidays.
type Preset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Presets lists the built-in holiday sets.
func Presets() []Preset {
	return []Preset{
		{ID: "rs", Label: "Republika Srpska (BiH)"},
		{ID: "fbih", Label: "Federation of BiH"},
		{ID: "rs-orthodox", Label: "Republika Srpska – add Orthodox Easter (Good Friday, Easter Monday) for a year"},
		{ID: "weekend", Label: "Weekends only (no public holidays)"},
	}
}

// PresetHolidays returns the holidays of a preset. Fixed-date holidays are
// yearly; movable feasts are generated for the given year only.
func PresetHolidays(id string, year int) []Holiday {
	y := func(md, name string) Holiday {
		return Holiday{Date: fmt.Sprintf("%d-%s", year, md), Name: name, Yearly: true}
	}
	switch id {
	case "rs":
		return []Holiday{
			y("01-01", "Nova godina"), y("01-02", "Nova godina (drugi dan)"),
			y("01-07", "Božić (pravoslavni)"), y("01-09", "Dan Republike"),
			y("05-01", "Praznik rada"), y("05-02", "Praznik rada (drugi dan)"),
			y("05-09", "Dan pobjede nad fašizmom"),
			y("11-21", "Dan uspostavljanja Opšteg okvirnog sporazuma za mir (Dejton)"),
		}
	case "fbih":
		return []Holiday{
			y("01-01", "Nova godina"), y("01-02", "Nova godina (drugi dan)"),
			y("03-01", "Dan nezavisnosti"), y("05-01", "Praznik rada"), y("05-02", "Praznik rada (drugi dan)"),
			y("11-25", "Dan državnosti"),
		}
	case "rs-orthodox":
		easter := OrthodoxEaster(year)
		return []Holiday{
			{Date: easter.AddDate(0, 0, -2).Format("2006-01-02"), Name: "Veliki petak (pravoslavni)"},
			{Date: easter.AddDate(0, 0, 1).Format("2006-01-02"), Name: "Vaskršnji ponedjeljak (pravoslavni)"},
		}
	}
	return nil
}

// Merge adds holidays that are not already present (same date, or same
// month-day for yearly entries) and returns the sorted list.
func Merge(existing, add []Holiday) []Holiday {
	seen := map[string]bool{}
	for _, h := range existing {
		seen[keyOf(h)] = true
	}
	out := append([]Holiday{}, existing...)
	for _, h := range add {
		if k := keyOf(h); !seen[k] {
			seen[k] = true
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Yearly != out[j].Yearly {
			return out[i].Yearly
		}
		if out[i].Yearly {
			return out[i].Date[5:] < out[j].Date[5:]
		}
		return out[i].Date < out[j].Date
	})
	return out
}

func keyOf(h Holiday) string {
	if len(h.Date) < 10 {
		return h.Date
	}
	if h.Yearly {
		return "Y" + h.Date[5:10]
	}
	return h.Date[:10]
}

// Clean drops malformed entries and normalises dates.
func Clean(list []Holiday) []Holiday {
	out := make([]Holiday, 0, len(list))
	for _, h := range list {
		d := strings.TrimSpace(h.Date)
		if len(d) >= 10 {
			d = d[:10]
		}
		if _, err := time.Parse("2006-01-02", d); err != nil {
			continue
		}
		h.Date = d
		h.Name = strings.TrimSpace(h.Name)
		if h.Name == "" {
			h.Name = "Day off"
		}
		out = append(out, h)
	}
	return Merge(nil, out)
}

// NormalizeWorkWeek returns the canonical "1,2,3" form of a work-week spec.
func NormalizeWorkWeek(spec string) string {
	week := ParseWorkWeek(spec)
	var parts []string
	for iso := 1; iso <= 7; iso++ {
		if week[time.Weekday(iso%7)] {
			parts = append(parts, strconv.Itoa(iso))
		}
	}
	return strings.Join(parts, ",")
}
