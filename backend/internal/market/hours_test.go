package market

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func ny(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, NewYork)
	if err != nil {
		panic(err)
	}
	return t
}

func TestIsRegularHours(t *testing.T) {
	tests := []struct {
		at   string
		want bool
	}{
		{"2026-10-02 09:29", false}, // Friday pre-open
		{"2026-10-02 09:30", true},
		{"2026-10-02 15:59", true},
		{"2026-10-02 16:00", false},
		{"2026-10-03 12:00", false}, // Saturday
		{"2026-10-04 12:00", false}, // Sunday
	}
	for _, tt := range tests {
		t.Run(tt.at, func(t *testing.T) {
			assert.Equal(t, tt.want, IsRegularHours(ny(tt.at)))
		})
	}
}

func TestNextOpenAndLastClose(t *testing.T) {
	tests := []struct {
		at        string
		nextOpen  string
		lastClose string
	}{
		{"2026-10-03 12:00", "2026-10-05 09:30", "2026-10-02 16:00"}, // Saturday
		{"2026-10-05 08:00", "2026-10-05 09:30", "2026-10-02 16:00"}, // Monday pre-open
		{"2026-10-05 10:00", "2026-10-06 09:30", "2026-10-02 16:00"}, // Monday in session
		{"2026-10-05 16:00", "2026-10-06 09:30", "2026-10-05 16:00"}, // Monday at close
	}
	for _, tt := range tests {
		t.Run(tt.at, func(t *testing.T) {
			assert.True(t, ny(tt.nextOpen).Equal(NextOpen(ny(tt.at))), "next open")
			assert.True(t, ny(tt.lastClose).Equal(LastClose(ny(tt.at))), "last close")
		})
	}
}
