package market

import (
	"time"
	_ "time/tzdata" // embed tz data so America/New_York resolves in minimal containers and on Windows
)

// NewYork is the US equity market time zone.
var NewYork = mustLoad("America/New_York")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Regular US session, local New York time.
const (
	openMinute  = 9*60 + 30
	closeMinute = 16 * 60
)

// IsRegularHours reports whether t falls inside the regular NYSE/Nasdaq session
// (Mon-Fri 09:30-16:00 ET). Exchange holidays are not modelled; real providers
// report holidays through their market-status endpoint.
func IsRegularHours(t time.Time) bool {
	ny := t.In(NewYork)
	if ny.Weekday() == time.Saturday || ny.Weekday() == time.Sunday {
		return false
	}
	m := ny.Hour()*60 + ny.Minute()
	return m >= openMinute && m < closeMinute
}

// NextOpen returns the next regular-session open strictly after t (ignoring holidays).
func NextOpen(t time.Time) time.Time {
	ny := t.In(NewYork)
	day := time.Date(ny.Year(), ny.Month(), ny.Day(), 9, 30, 0, 0, NewYork)
	if !day.After(ny) {
		day = day.AddDate(0, 0, 1)
	}
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

// LastClose returns the most recent regular-session close at or before t.
func LastClose(t time.Time) time.Time {
	ny := t.In(NewYork)
	day := time.Date(ny.Year(), ny.Month(), ny.Day(), 16, 0, 0, 0, NewYork)
	if day.After(ny) {
		day = day.AddDate(0, 0, -1)
	}
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}
