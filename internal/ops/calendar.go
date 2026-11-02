package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/ical"
)

// calendarDoc names the egress event of a calendar export: ids only, and a
// feed has no single document.
const calendarDoc = "calendar"

// CalendarExportDefaultDays is the window of calendar_export when none is named.
const CalendarExportDefaultDays = 30

// CalendarExportInput is the input of calendar_export.
type CalendarExportInput struct {
	Days int    `json:"days,omitempty"` // from today on; default 30, at most 366
	To   string `json:"to,omitempty"`   // transport: stdout (default), file:<path>, clipboard, osc52, tmux, wormhole, qr, ssh:<host>:<path>
	Zone string `json:"zone,omitempty"` // the zone whose midnight starts the window; default this machine's
}

// CalendarExportOutput is the output of calendar_export.
type CalendarExportOutput struct {
	To            string `json:"to"`
	Destination   string `json:"destination"` // egress destination logged: export:<transport>
	Marking       string `json:"marking"`
	EgressDefault string `json:"egress_default"`
	EgressAllowed bool   `json:"egress_allowed"`
	Events        int    `json:"events"`
	Ext           string `json:"ext"`
	Text          string `json:"text"`
}

// Rendered returns the content of the output, for a surface that runs the
// transport.
func (o CalendarExportOutput) Rendered() export.Rendered {
	return export.Rendered{Format: export.Plain, Ext: o.Ext, Data: []byte(o.Text)}
}

// CalendarExport writes the scheduled meetings and the due tasks of the next
// days as an .ics feed and logs the egress for the transport. Like export, it
// moves nothing: the CLI runs the transport. Para never writes an external
// calendar; a calendar that wants the feed reads the file.
var CalendarExport = define("calendar_export", "Render scheduled meetings and due tasks of the next days as an .ics feed for a transport and log the egress; the CLI runs the transport", func(ctx context.Context, d Deps, in CalendarExportInput) (CalendarExportOutput, error) {
	days, err := agendaDays(in.Days, CalendarExportDefaultDays)
	if err != nil {
		return CalendarExportOutput{}, err
	}
	loc, err := agendaZone(in.Zone)
	if err != nil {
		return CalendarExportOutput{}, err
	}
	target, err := export.ParseTarget(in.To)
	if err != nil {
		return CalendarExportOutput{}, err
	}
	dest := target.Dest()
	dec, err := d.Store.Egress(ctx, calendarDoc, dest)
	if err != nil {
		return CalendarExportOutput{}, err
	}
	if !dec.Allowed {
		return CalendarExportOutput{}, fmt.Errorf("transport %s is not in this vault's egress.export", target.Kind)
	}
	w := newWindow(d.Store.Now(), loc, days)
	items, err := agendaItems(ctx, d, w, false)
	if err != nil {
		return CalendarExportOutput{}, err
	}
	fillNames(ctx, d.Store, &items)
	vault := d.Store.VaultID()
	cal := ical.Calendar{ProdID: "-//para//agenda//EN", Marking: dec.Marking, Stamp: d.Store.Now()}
	for _, it := range items {
		switch it.Kind {
		case AgendaMeeting:
			start, serr := time.Parse(time.RFC3339, it.At)
			end, eerr := time.Parse(time.RFC3339, it.Until)
			if serr != nil || eerr != nil {
				continue
			}
			ev := ical.Event{UID: it.ID + "@" + vault + ".para", Summary: it.Title, Start: start, End: end}
			if names := joinNonEmpty(it.WhoNames); names != "" {
				ev.Description = "With: " + names
			}
			cal.Events = append(cal.Events, ev)
		case AgendaDue:
			cal.Events = append(cal.Events, ical.Event{UID: it.ID + "-due@" + vault + ".para", Summary: "Due: " + it.Title, Day: it.Date})
		}
	}
	out := CalendarExportOutput{
		To: target.String(), Destination: dest, Marking: dec.Marking, EgressDefault: dec.Default,
		EgressAllowed: dec.Allowed, Events: len(cal.Events), Ext: "ics", Text: cal.Render(),
	}
	if err := target.Check(out.Rendered()); err != nil {
		return CalendarExportOutput{}, err
	}
	return out, nil
})

func joinNonEmpty(ss []string) string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}
