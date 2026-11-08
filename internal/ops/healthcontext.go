package ops

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// No context. An open task or question whose title is all there is: the owner
// cannot tell what it is or why it exists. It has context when any of these
// has text (contextWords words or more, not counting links, URLs, headings,
// and source or import lines):
//   - its own body, a question's context or option details, a task's steps;
//   - a task's done_when (any text);
//   - a page it links in its body, or an io item filed into it or linking it;
//   - its parent (task or obligation), a question's scope task, or its goal
//     (the goal's body, or a brief).
// A meeting and a call are not checked: their time and people say what they
// are. An area is not context: it is too broad to explain one item.

// HealthNoContext is an open task or question with no context.
const HealthNoContext = "no_context"

// contextWords is the fewest words of text that count as context.
const contextWords = 5

// NoContextText is the finding's message.
const NoContextText = "no context: the title is all there is. The body is empty or only a source line, there is no done-when, and no linked page, message, parent, or goal says what it is"

var (
	ctxURLRe  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)|https?://\S+|!?\[\[[^\]]*\]\]`)
	ctxListRe = regexp.MustCompile(`^(?:[-*+>]|\d+[.)])\s+`)
)

// provenance lists line starts, in lower case, that say where an item came
// from and not what it is.
var provenance = []string{"source:", "sources:", "imported from", "imported:", "from:", "filed from", "filed into", "via:", "link:", "links:", "url:", "ref:", "reason check:", "see:"}

// meaningfulText reports whether a body says something beyond its links and
// where it came from: contextWords words or more.
func meaningfulText(body string) bool {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "%%") || strings.HasPrefix(line, "---") {
			continue
		}
		line = strings.TrimSpace(ctxListRe.ReplaceAllString(line, ""))
		low := strings.ToLower(line)
		prov := false
		for _, p := range provenance {
			prov = prov || strings.HasPrefix(low, p)
		}
		if prov {
			continue
		}
		n += len(strings.Fields(ctxURLRe.ReplaceAllString(line, "$1")))
		if n >= contextWords {
			return true
		}
	}
	return false
}

// explains reports whether a linked page has text that can explain an item.
func explains(d model.Document) bool {
	if d.Meta == nil {
		return false
	}
	if meaningfulText(d.Body) {
		return true
	}
	switch m := d.Meta.(type) {
	case *model.Project:
		return m.HasBrief()
	case *model.Task:
		return meaningfulText(m.DoneWhen)
	case *model.Question:
		return meaningfulText(m.Context)
	}
	return false
}

// hasContext reports whether an open task or question has context (see the
// top of this file).
func (h *healther) hasContext(d model.Document) bool {
	if meaningfulText(d.Body) {
		return true
	}
	var linked []string
	switch m := d.Meta.(type) {
	case *model.Task:
		if strings.TrimSpace(m.DoneWhen) != "" || meaningfulText(m.Steps) {
			return true
		}
		linked = append(linked, m.Parent.Target(), m.Incident.Target(), m.Previous.Target(), m.Project.Target())
	case *model.Question:
		if strings.TrimSpace(m.Context) != "" || len(m.OptionDetails) > 0 {
			return true
		}
		if sc := m.Scope.Target(); sc != "" {
			linked = append(linked, sc)
			if sd, ok := h.docs[sc]; ok {
				if t, ok := sd.Meta.(*model.Task); ok {
					linked = append(linked, t.Project.Target())
				}
			}
		}
	}
	for _, x := range wikiRe.FindAllStringSubmatch(d.Body, -1) {
		linked = append(linked, strings.TrimSpace(x[1]))
	}
	linked = append(linked, h.ioFor[d.ID]...)
	for _, id := range linked {
		if ld, ok := h.docs[id]; ok && id != d.ID {
			if _, area := ld.Meta.(*model.Area); !area && explains(ld) {
				return true
			}
		}
	}
	return false
}

// noContext adds the finding for an open task or question with no context.
func (h *healther) noContext(id, goal string, d model.Document) {
	if t, ok := d.Meta.(*model.Task); ok && (t.Kind == model.KindMeeting || t.Kind == model.KindCall) {
		return
	}
	if h.hasContext(d) {
		return
	}
	fix := fmt.Sprintf("read where it came from (context {id: %s}) and write one or two sentences of what it is and why, with the source link, in the body (doc_edit {id: %s, body: ...}); or close it if nobody can say", id, id)
	if _, ok := d.Meta.(*model.Question); ok {
		fix = fmt.Sprintf("read where it came from (context {id: %s}) and say what it is and why (question_edit {id: %s, context: ...}); or question_drop it if nobody can say", id, id)
	}
	h.add(HealthNoContext, id, goal, NoContextText, fix)
}
