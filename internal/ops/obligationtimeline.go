package ops

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// The obligation timeline is the schedule of the owner's promises: each day
// of a window lists its fixed points (meetings at their time, promises at
// their due date), each with its open prep. A point is at risk when its open
// prep takes more working time than is left before it, or when any of it is
// blocked. Working time is order.work_hours_per_day (default 6) on a weekday
// from 9:00 local, and none at the weekend.

// WorkHoursKey is the vault setting of the working hours in a weekday.
const WorkHoursKey = "order.work_hours_per_day"

// DefaultWorkHours is the working hours in a weekday when the vault sets none.
const DefaultWorkHours = 6

// workStartHour is the hour a working day starts, local time.
const workStartHour = 9

// scoreDays is the window of the promise numbers.
const scoreDays = 30

// ObligationTimelineInput is the input of obligation_timeline.
type ObligationTimelineInput struct {
	Days int `json:"days,omitempty" query:"days"` // the window from today; default 14, at most 60
}

// TimelinePoint is one fixed point: a meeting at its time, or a promise on
// its due date, with its open prep.
type TimelinePoint struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Kind    model.TaskKind `json:"kind"`
	Date    string         `json:"date"`
	Start   string         `json:"start,omitempty"`
	End     string         `json:"end,omitempty"`
	Overdue bool           `json:"overdue,omitempty"`
	// Promise is true for an obligation to people (para keeps its score).
	Promise bool `json:"promise"`
	// Parties are the names of the people it is owed to or with.
	Parties []string `json:"parties"`
	// Prep is its open prep; PrepHours the hours it takes (xs 0.25, s 1, m
	// 4, l 8; unset effort is the value order's default); HoursLeft the
	// working hours before it.
	Prep      []ObligationPart `json:"prep"`
	PrepHours float64          `json:"prep_hours"`
	HoursLeft float64          `json:"hours_left"`
	AtRisk    bool             `json:"at_risk"`
	Risk      string           `json:"risk,omitempty"`
}

// TimelineDay is one day of the timeline.
type TimelineDay struct {
	Date    string          `json:"date"`
	Weekday string          `json:"weekday"`
	Hours   float64         `json:"hours"` // working hours that day
	Points  []TimelinePoint `json:"points"`
}

// ObligationScore counts how the promises of the last Days days went: kept,
// renegotiated, and broken. No score is kept per person.
type ObligationScore struct {
	Days         int `json:"days"`
	Kept         int `json:"kept"`
	Renegotiated int `json:"renegotiated"`
	Broken       int `json:"broken"`
}

// ObligationTimelineOutput is the timeline.
type ObligationTimelineOutput struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	Days    []TimelineDay   `json:"days"`
	Overdue []TimelinePoint `json:"overdue"`
	Score   ObligationScore `json:"score"`
}

// ObligationTimeline is the schedule of the owner's obligations.
var ObligationTimeline = defineRead("obligation_timeline", "Show the owner's obligations as a schedule for days (default 14): each day lists its fixed points (meetings at their time, promises at their due date) with their open prep (title, state, effort, backward date). A point is at_risk when its open prep (xs 0.25h, s 1h, m 4h, l 8h) takes more than the working hours left before it (order.work_hours_per_day, default 6, weekdays only), or any prep is blocked. overdue lists open ones past their date. score counts the promises of the last 30 days: kept, renegotiated, broken", func(ctx context.Context, d Deps, in ObligationTimelineInput) (ObligationTimelineOutput, error) {
	days := in.Days
	switch {
	case days == 0:
		days = 14
	case days < 0 || days > 60:
		return ObligationTimelineOutput{}, fmt.Errorf("days %d: want 1 to 60", days)
	}
	hours, err := configInt(d.Store, WorkHoursKey, DefaultWorkHours)
	if err != nil {
		return ObligationTimelineOutput{}, err
	}
	docs, err := allDocs(ctx, d.Store)
	if err != nil {
		return ObligationTimelineOutput{}, fmt.Errorf("ops: obligation_timeline: %w", err)
	}
	return timelineOf(docs, d.Store.Now().Local(), days, hours), nil
})

// timelineOf builds the timeline of docs at now.
func timelineOf(docs map[string]model.Document, now time.Time, days, hours int) ObligationTimelineOutput {
	today := now.Format(dateLayout)
	first, _ := time.ParseInLocation(dateLayout, today, now.Location())
	end := first.AddDate(0, 0, days-1).Format(dateLayout)
	out := ObligationTimelineOutput{From: today, To: end, Days: []TimelineDay{}, Overdue: []TimelinePoint{}, Score: ObligationScore{Days: scoreDays}}
	byDay := map[string][]TimelinePoint{}
	get := func(id string) (model.Document, error) {
		if x, ok := docs[id]; ok {
			return x, nil
		}
		return model.Document{}, model.ErrNotFound
	}
	since := first.AddDate(0, 0, -scoreDays).Format(dateLayout)
	ids := make([]string, 0, len(docs))
	for id := range docs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		doc := docs[id]
		o, ok := doc.Meta.(*model.Obligation)
		if !ok || doc.Folder == model.FolderArchive || doc.Meta.Header().DuplicateOf != "" {
			continue
		}
		date := obligationDate(o)
		if promised(o) {
			at := date
			if !o.Open() {
				at = string(o.Closed)
			}
			if at != "" && at >= since && at <= today {
				switch promiseNow(get, o, today) {
				case model.PromiseKept:
					out.Score.Kept++
				case model.PromiseRenegotiated:
					out.Score.Renegotiated++
				case model.PromiseBroken:
					out.Score.Broken++
				}
			}
		}
		if !o.Open() || date == "" || len(o.OwedBy) > 0 || o.Produce != nil || attendQuiet(o.Kind, o.Optional, o.Attend) || date > end {
			continue
		}
		p := pointOf(docs, doc, o, date, now, hours)
		if date < today {
			p.Overdue = true
			out.Overdue = append(out.Overdue, p)
			continue
		}
		byDay[date] = append(byDay[date], p)
	}
	for i := range days {
		day := first.AddDate(0, 0, i)
		key := day.Format(dateLayout)
		pts := byDay[key]
		slices.SortStableFunc(pts, func(a, b TimelinePoint) int {
			return cmp.Or(cmp.Compare(startKey(a.Start), startKey(b.Start)), cmp.Compare(a.ID, b.ID))
		})
		if pts == nil {
			pts = []TimelinePoint{}
		}
		out.Days = append(out.Days, TimelineDay{Date: key, Weekday: day.Weekday().String()[:3], Hours: dayHours(day, hours), Points: pts})
	}
	slices.SortStableFunc(out.Overdue, func(a, b TimelinePoint) int { return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.ID, b.ID)) })
	return out
}

// startKey sorts timed points by their start, then the ones with none.
func startKey(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return "~"
}

// pointOf is an open obligation as a point of the timeline.
func pointOf(docs map[string]model.Document, doc model.Document, o *model.Obligation, date string, now time.Time, hours int) TimelinePoint {
	p := TimelinePoint{ID: doc.ID, Title: doc.Title, Kind: o.Kind, Date: date, Promise: promised(o), Parties: []string{}, Prep: []ObligationPart{}}
	if o.Kind == model.KindMeeting {
		p.Start, p.End = o.Scheduled, o.End
	}
	for _, pt := range partiesOf(o) {
		p.Parties = append(p.Parties, partyView(docs, pt).Name)
	}
	before, _ := obligationPrep(docs, doc.ID, o)
	var blocked string
	for _, x := range before {
		if x.Done {
			continue
		}
		p.Prep = append(p.Prep, x)
		p.PrepHours += effortHours[x.Effort]
		if x.Blocked && blocked == "" {
			blocked = x.Title
		}
	}
	due := endOfWork(date, now.Location(), hours)
	if t, err := time.Parse(time.RFC3339, p.Start); err == nil {
		due = t.In(now.Location())
	}
	p.HoursLeft = math.Floor(workHoursBetween(now, due, hours)*4) / 4 // to the quarter hour below
	switch {
	case blocked != "":
		p.AtRisk, p.Risk = true, "prep blocked: "+blocked
	case p.PrepHours > p.HoursLeft:
		p.AtRisk, p.Risk = true, fmt.Sprintf("prep takes %s, %s of work time left", hoursWords(p.PrepHours), hoursWords(p.HoursLeft))
	}
	return p
}

// endOfWork is the end of the working day of date.
func endOfWork(date string, loc *time.Location, hours int) time.Time {
	t, _ := time.ParseInLocation(dateLayout, date, loc)
	return t.Add(time.Duration(workStartHour)*time.Hour + time.Duration(hours)*time.Hour)
}

// dayHours is the working hours of a day: none at the weekend.
func dayHours(day time.Time, hours int) float64 {
	if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return 0
	}
	return float64(hours)
}

// workHoursBetween is the working time from a to b, in hours: each weekday
// from 9:00 for hours hours, none at the weekend.
func workHoursBetween(a, b time.Time, hours int) float64 {
	if !b.After(a) {
		return 0
	}
	total := 0.0
	day := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, a.Location())
	for !day.After(b) {
		if dayHours(day, hours) > 0 {
			from := day.Add(time.Duration(workStartHour) * time.Hour)
			to := from.Add(time.Duration(hours) * time.Hour)
			if a.After(from) {
				from = a
			}
			if b.Before(to) {
				to = b
			}
			if to.After(from) {
				total += to.Sub(from).Hours()
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return total
}

// hoursWords says hours as "1.5h".
func hoursWords(h float64) string {
	return strconv.FormatFloat(h, 'f', -1, 64) + "h"
}
