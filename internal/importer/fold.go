package importer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// noteLine is a dated text line of an item (a note or a risk).
type noteLine struct {
	TS   time.Time
	Text string
}

// contact is one person asked about an item, keyed by who. The latest event
// for a field wins.
type contact struct {
	Who, Via, State, Ref, Direction string
	ReplyBy                         string
	FirstTS, SentTS                 time.Time
}

// item is the folded state of one id in events.jsonl.
type item struct {
	ID, Type, Text, Project, Due, Who, Ref, Source, Via, On, Direction, Opinion string
	Created                                                                     time.Time
	Status, StatusNote                                                          string // latest valid status event
	Contacts                                                                    []*contact
	Depends                                                                     []string
	Check                                                                       string
	Notes, Risks                                                                []noteLine
	Answer, AnswerActor                                                         string
	AnswerTS                                                                    time.Time
	raw                                                                         [][]byte
}

var (
	allStatuses   = map[string]bool{"todo": true, "inflight": true, "waiting": true, "blocked": true, "done": true, "dropped": true, "open": true, "answered": true}
	contactStates = map[string]bool{"drafted": true, "sent": true, "replied": true, "no-reply": true}
	idLetterRe    = regexp.MustCompile(`^([A-Za-z])-`)
	typeWords     = map[string]string{"task": "T", "question": "Q", "deadline": "D", "note": "N", "decision": "X", "interaction": "I"}
)

// foldResult is the outcome of folding the events.
type foldResult struct {
	items  map[string]*item
	order  []string
	report *FoldReport
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprint(v)
	case bool:
		return fmt.Sprint(v)
	}
	return ""
}

func strList(m map[string]any, k string) []string {
	switch v := m[k].(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

func parseTS(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// itemType returns T Q D N X or I.
func itemType(declared, id string) string {
	d := strings.TrimSpace(declared)
	if l, ok := typeWords[strings.ToLower(d)]; ok {
		return l
	}
	if len(d) == 1 && strings.Contains("TQDNXI", strings.ToUpper(d)) {
		return strings.ToUpper(d)
	}
	if m := idLetterRe.FindStringSubmatch(id); m != nil && strings.Contains("TQDNXI", strings.ToUpper(m[1])) {
		return strings.ToUpper(m[1])
	}
	return ""
}

// foldEvents reads events.jsonl and folds it. Rules: the latest valid status
// wins; a contact is keyed by who and the latest value of each of its fields
// wins; depends edges accumulate; the latest check wins. A line that cannot
// be used is reported with its number, never dropped silently.
func foldEvents(p string) (*foldResult, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	type line struct {
		n   int
		raw []byte
		m   map[string]any
		ts  time.Time
	}
	var lines []line
	rep := &FoldReport{ItemsByType: map[string]int{}, TasksByStatus: map[string]int{}, DeadlinesByStatus: map[string]int{},
		QuestionsByStatus: map[string]int{}, ContactsByState: map[string]int{}}
	bad := func(n int, why string) {
		rep.BadLines = append(rep.BadLines, Issue{fmt.Sprintf("events.jsonl:%d", n), why})
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var last time.Time
	for n := 1; sc.Scan(); n++ {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		rep.Lines++
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			bad(n, "not a JSON object")
			continue
		}
		ts, ok := parseTS(str(m, "ts"))
		if !ok {
			bad(n, "ts is missing or not a time; ordered by position")
			ts = last
		}
		last = ts
		lines = append(lines, line{n, append([]byte(nil), raw...), m, ts})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].ts.Before(lines[j].ts) })

	res := &foldResult{items: map[string]*item{}, report: rep}
	for _, ln := range lines {
		m := ln.m
		id, action := str(m, "id"), strings.ToLower(str(m, "action"))
		if id == "" || action == "" {
			bad(ln.n, "no id or no action")
			continue
		}
		it := res.items[id]
		if action == "add" {
			if it == nil {
				it = &item{ID: id, Created: ln.ts}
				res.items[id] = it
				res.order = append(res.order, id)
			}
		} else if it == nil {
			bad(ln.n, "action "+action+" for an id with no add")
			continue
		}
		it.raw = append(it.raw, ln.raw)
		set := func(dst *string, k string) {
			if v := str(m, k); v != "" {
				*dst = v
			}
		}
		note := str(m, "text")
		if note == "" {
			note = str(m, "note")
		}
		switch action {
		case "add":
			set(&it.Text, "text")
			set(&it.Project, "project")
			set(&it.Due, "due")
			set(&it.Who, "who")
			set(&it.Ref, "ref")
			set(&it.Source, "source")
			set(&it.Via, "via")
			set(&it.On, "on")
			set(&it.Direction, "direction")
			set(&it.Opinion, "opinion")
			if t := itemType(str(m, "type"), id); t != "" {
				it.Type = t
			} else if it.Type == "" {
				bad(ln.n, "item type unknown (type field and id prefix)")
			}
			if s := strings.ToLower(str(m, "status")); s != "" {
				if allStatuses[s] {
					it.Status = s
				} else {
					bad(ln.n, "unknown status "+safe(s)+"; ignored")
				}
			}
			it.addDepends(strList(m, "depends_on"))
		case "status":
			s := strings.ToLower(str(m, "status"))
			if !allStatuses[s] {
				bad(ln.n, "unknown status "+safe(s)+"; ignored")
				continue
			}
			it.Status, it.StatusNote = s, str(m, "note")
		case "answer":
			a := str(m, "answer")
			if a == "" {
				a = note
			}
			it.Answer, it.AnswerTS, it.AnswerActor = a, ln.ts, str(m, "actor")
			it.Status = "answered"
		case "note":
			if note != "" {
				it.Notes = append(it.Notes, noteLine{ln.ts, note})
			}
		case "risk":
			if note != "" {
				it.Risks = append(it.Risks, noteLine{ln.ts, note})
			}
		case "contact":
			who := str(m, "who")
			if who == "" {
				bad(ln.n, "contact with no who")
				continue
			}
			st := strings.ToLower(strings.ReplaceAll(str(m, "state"), "_", "-"))
			if st != "" && !contactStates[st] {
				bad(ln.n, "unknown contact state "+safe(st)+"; ignored")
				st = ""
			}
			var c *contact
			for _, x := range it.Contacts {
				if strings.EqualFold(x.Who, who) {
					c = x
				}
			}
			if c == nil {
				c = &contact{Who: who, FirstTS: ln.ts}
				it.Contacts = append(it.Contacts, c)
			}
			if st != "" {
				c.State = st
				if st == "sent" && c.SentTS.IsZero() {
					c.SentTS = ln.ts
				}
			}
			set(&c.Via, "via")
			set(&c.Ref, "ref")
			set(&c.ReplyBy, "reply_by")
			set(&c.Direction, "direction")
		case "depends":
			it.addDepends(strList(m, "depends_on"))
			if len(strList(m, "depends_on")) == 0 {
				bad(ln.n, "depends with no depends_on")
			}
		case "check":
			if c := str(m, "check"); c != "" {
				it.Check = c
			}
		default:
			bad(ln.n, "unknown action "+safe(action))
		}
	}
	for _, id := range res.order {
		it := res.items[id]
		if it.Type == "" {
			bad(0, "item "+safe(id)+" has no usable type; not imported")
			continue
		}
		rep.Items++
		rep.ItemsByType[it.Type]++
		eff := it.effective()
		switch it.Type {
		case "T":
			rep.TasksByStatus[eff]++
		case "D":
			rep.DeadlinesByStatus[eff]++
		case "Q":
			rep.QuestionsByStatus[eff]++
			if eff != "answered" && eff != "dropped" && eff != "done" {
				rep.OpenQuestions++
			}
		}
		for _, c := range it.Contacts {
			rep.Contacts++
			s := c.State
			if s == "" {
				s = "drafted"
			}
			rep.ContactsByState[s]++
		}
		rep.DependsEdges += len(it.Depends)
		if it.Check != "" {
			rep.Checks++
		}
		rep.Risks += len(it.Risks)
	}
	return res, nil
}

func (it *item) addDepends(ids []string) {
	for _, d := range ids {
		dup := false
		for _, x := range it.Depends {
			dup = dup || x == d
		}
		if !dup {
			it.Depends = append(it.Depends, d)
		}
	}
}

// effective applies the contact rules to the latest status. An item is
// waiting while any contact is sent (unless it is finished), and goes back to
// todo (open for a question) when every contact is replied or no-reply and
// the status was waiting.
func (it *item) effective() string {
	st := it.Status
	if st == "" {
		st = "todo"
		if it.Type == "Q" {
			st = "open"
		}
	}
	finished := st == "done" || st == "dropped" || st == "answered"
	anySent, allClosed := false, len(it.Contacts) > 0
	for _, c := range it.Contacts {
		anySent = anySent || c.State == "sent"
		allClosed = allClosed && (c.State == "replied" || c.State == "no-reply")
	}
	switch {
	case anySent && !finished:
		return "waiting"
	case allClosed && st == "waiting":
		if it.Type == "Q" {
			return "open"
		}
		return "todo"
	}
	return st
}
