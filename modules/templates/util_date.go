// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package templates

import (
	"fmt"
	"html"
	"html/template"
	"strings"
	"time"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/translation"
)

type DateUtils struct{}

// timeNow is the clock of the date helpers; tests replace it.
var timeNow = time.Now

func NewDateUtils() *DateUtils {
	return (*DateUtils)(nil) // the util is stateless, and we do not need to create an instance
}

// AbsoluteShort renders in "Jan 01, 2006" format
func (du *DateUtils) AbsoluteShort(time any) template.HTML {
	return dateTimeFormat("short", time)
}

// AbsoluteShort renders in "Sun, 01 Jan, 2006" format
func (du *DateUtils) AbsoluteShortWithDay(time any) template.HTML {
	return dateTimeFormat("short-with-day", time)
}

// AbsoluteLong renders in "January 01, 2006" format
func (du *DateUtils) AbsoluteLong(time any) template.HTML {
	return dateTimeFormat("long", time)
}

// FullTime renders in "Jan 01, 2006 20:33:44" format
func (du *DateUtils) FullTime(time any) template.HTML {
	return dateTimeFormat("full", time)
}

// DayTime renders in the design's "27 Oct 02:00" format, in UTC, as plain text (day first, no year; the full time is the tooltip).
func (du *DateUtils) DayTime(when any) template.HTML {
	t, isZero := anyToTime(when)
	if isZero {
		return "-"
	}
	t = t.UTC()
	return template.HTML(fmt.Sprintf(`<time datetime="%s" title="%s">%s</time>`, html.EscapeString(t.Format(time.RFC3339)), html.EscapeString(t.Format("2006-01-02 15:04:05 UTC")), html.EscapeString(t.Format("2 Jan 15:04"))))
}

// Day renders in the design's "3 Feb 2026" format (day first), in the interface's time zone; the full time is the tooltip.
func (du *DateUtils) Day(when any) template.HTML {
	t, isZero := anyToTime(when)
	if isZero {
		return "-"
	}
	t = t.In(setting.DefaultUILocation)
	return template.HTML(fmt.Sprintf(`<time datetime="%s" title="%s">%s</time>`, html.EscapeString(t.Format(time.RFC3339)), html.EscapeString(t.Format("2006-01-02 15:04:05 -07:00")), html.EscapeString(t.Format("2 Jan 2006"))))
}

// dayDistance is the number of calendar days from t to now, in the interface's time zone.
func dayDistance(t time.Time) int {
	loc := setting.DefaultUILocation
	day := func(x time.Time) time.Time {
		y, m, d := x.In(loc).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(day(timeNow()).Sub(day(t)).Hours() / 24)
}

func timeTag(t time.Time, text string) template.HTML {
	return template.HTML(fmt.Sprintf(`<time datetime="%s" title="%s">%s</time>`, html.EscapeString(t.Format(time.RFC3339)), html.EscapeString(t.Format("2006-01-02 15:04:05 -07:00")), html.EscapeString(text)))
}

// TimeOrDay renders the time of today as "10:14:02", "Yesterday", "2 days ago" for the days before, and "27 Oct 10:14" for a week or more (the repository audit log).
func (du *DateUtils) TimeOrDay(when any) template.HTML {
	t, isZero := anyToTime(when)
	if isZero {
		return "-"
	}
	t = t.In(setting.DefaultUILocation)
	switch n := dayDistance(t); {
	case n <= 0:
		return timeTag(t, t.Format("15:04:05"))
	case n == 1:
		return timeTag(t, "Yesterday")
	case n < 7:
		return timeTag(t, fmt.Sprintf("%d days ago", n))
	}
	return timeTag(t, t.Format("2 Jan 15:04"))
}

// TodayTime renders "Today 09:41", "Yesterday 09:41" or "27 Oct 09:41" (the instance audit log).
func (du *DateUtils) TodayTime(when any) template.HTML {
	t, isZero := anyToTime(when)
	if isZero {
		return "-"
	}
	t = t.In(setting.DefaultUILocation)
	switch dayDistance(t) {
	case 0:
		return timeTag(t, "Today "+t.Format("15:04"))
	case 1:
		return timeTag(t, "Yesterday "+t.Format("15:04"))
	}
	return timeTag(t, t.Format("2 Jan 15:04"))
}

func (du *DateUtils) TimeSince(time any) template.HTML {
	return TimeSince(time)
}

// ParseLegacy parses the datetime in legacy format, eg: "2016-01-02" in server's timezone.
// It shouldn't be used in new code. New code should use Time or TimeStamp as much as possible.
func (du *DateUtils) ParseLegacy(datetime string) time.Time {
	return parseLegacy(datetime)
}

func parseLegacy(datetime string) time.Time {
	t, err := time.Parse(time.RFC3339, datetime)
	if err != nil {
		t, _ = time.ParseInLocation(time.DateOnly, datetime, setting.DefaultUILocation)
	}
	return t
}

func dateTimeLegacy(format string, datetime any, _ ...string) template.HTML {
	if !setting.IsProd || setting.IsInTesting {
		panic("dateTimeLegacy is for backward compatibility only, do not use it in new code")
	}
	if s, ok := datetime.(string); ok {
		datetime = parseLegacy(s)
	}
	return dateTimeFormat(format, datetime)
}

func timeSinceLegacy(time any, _ translation.Locale) template.HTML {
	if !setting.IsProd || setting.IsInTesting {
		panic("timeSinceLegacy is for backward compatibility only, do not use it in new code")
	}
	return TimeSince(time)
}

func anyToTime(any any) (t time.Time, isZero bool) {
	switch v := any.(type) {
	case nil:
		// it is zero
		break
	case *time.Time:
		if v != nil {
			t = *v
		}
	case time.Time:
		t = v
	case timeutil.TimeStamp:
		t = v.AsTime()
	case timeutil.TimeStampNano:
		t = v.AsTime()
	case int:
		t = timeutil.TimeStamp(v).AsTime()
	case int64:
		t = timeutil.TimeStamp(v).AsTime()
	default:
		panic(fmt.Sprintf("Unsupported time type %T", any))
	}
	return t, t.IsZero() || t.Unix() == 0
}

func dateTimeFormat(format string, datetime any) template.HTML {
	t, isZero := anyToTime(datetime)
	if isZero {
		return "-"
	}
	var textEscaped string
	datetimeEscaped := html.EscapeString(t.Format(time.RFC3339))
	if format == "full" {
		textEscaped = html.EscapeString(t.Format("2006-01-02 15:04:05 -07:00"))
	} else {
		textEscaped = html.EscapeString(t.Format("2006-01-02"))
	}

	attrs := []string{`weekday=""`, `year="numeric"`}
	switch format {
	case "short", "long": // date only
		attrs = append(attrs, `month="`+format+`"`, `day="numeric"`)
		return template.HTML(fmt.Sprintf(`<absolute-date %s date="%s">%s</absolute-date>`, strings.Join(attrs, " "), datetimeEscaped, textEscaped))
	case "short-with-day":
		attrs = append(attrs, `month="short"`, `day="numeric"`)
		attrs[0] = `weekday="short"`
		return template.HTML(fmt.Sprintf(`<absolute-date %s date="%s">%s</absolute-date>`, strings.Join(attrs, " "), datetimeEscaped, textEscaped))
	case "full": // full date including time
		attrs = append(attrs, `format="datetime"`, `month="short"`, `day="numeric"`, `hour="numeric"`, `minute="numeric"`, `second="numeric"`, `data-tooltip-content`, `data-tooltip-interactive="true"`)
		return template.HTML(fmt.Sprintf(`<relative-time %s datetime="%s">%s</relative-time>`, strings.Join(attrs, " "), datetimeEscaped, textEscaped))
	default:
		panic(fmt.Sprintf("Unsupported format %s", format))
	}
}

func timeSinceTo(then any, now time.Time) template.HTML {
	thenTime, isZero := anyToTime(then)
	if isZero {
		return "-"
	}

	friendlyText := thenTime.Format("2006-01-02 15:04:05 -07:00")

	// document: https://github.com/github/relative-time-element
	attrs := `tense="past"`
	isFuture := now.Before(thenTime)
	if isFuture {
		attrs = `tense="future"`
	}

	// declare data-tooltip-content attribute to switch from "title" tooltip to "tippy" tooltip
	htm := fmt.Sprintf(`<relative-time prefix="" %s datetime="%s" data-tooltip-content data-tooltip-interactive="true">%s</relative-time>`,
		attrs, thenTime.Format(time.RFC3339), friendlyText)
	return template.HTML(htm)
}

// TimeSince renders relative time HTML given a time
func TimeSince(then any) template.HTML {
	return timeSinceTo(then, time.Now())
}
