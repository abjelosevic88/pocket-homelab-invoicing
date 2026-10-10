package server

import (
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/calendar"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// workingDays counts working days in [from, to] using the configured work week and holidays.
func workingDays(st store.Settings, from, to string) (calendar.Result, error) {
	return calendar.CountRange(from, to, calendar.ParseWorkWeek(st.WorkWeek), st.Holidays)
}

// handleWorkingDays: GET /calendar/working-days?month=YYYY-MM  or  ?from=&to=
func (s *Server) handleWorkingDays(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	if m := q.Get("month"); m != "" {
		a, b, err := calendar.MonthRange(m)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		from, to = a.Format("2006-01-02"), b.Format("2006-01-02")
	}
	if from == "" || to == "" {
		now := time.Now()
		a := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		from, to = a.Format("2006-01-02"), a.AddDate(0, 1, -1).Format("2006-01-02")
	}
	res, err := workingDays(st, from, to)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	f, _ := time.Parse("2006-01-02", res.From)
	t, _ := time.Parse("2006-01-02", res.To)
	allHol := calendar.Count(f, t, calendar.ParseWorkWeek("1,2,3,4,5,6,7"), st.Holidays).Holidays
	writeJSON(w, http.StatusOK, map[string]any{
		"from": res.From, "to": res.To, "working_days": res.WorkingDays, "week_days": res.WeekDays,
		"holidays": res.Holidays, "all_holidays": allHol, "hours_per_day": st.HoursPerDay, "working_hours": float64(res.WorkingDays) * st.HoursPerDay,
		"work_week": calendar.NormalizeWorkWeek(st.WorkWeek),
	})
}

// handleHolidayPresets: GET /calendar/presets            -> list of presets
//
//	GET /calendar/presets?set=rs&year=2026 -> holidays of that preset
func (s *Server) handleHolidayPresets(w http.ResponseWriter, r *http.Request) {
	set := r.URL.Query().Get("set")
	if set == "" {
		writeJSON(w, http.StatusOK, calendar.Presets())
		return
	}
	year := qInt(r, "year", time.Now().Year())
	list := calendar.PresetHolidays(set, year)
	if list == nil {
		list = []calendar.Holiday{}
	}
	writeJSON(w, http.StatusOK, list)
}
