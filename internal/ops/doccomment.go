package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Comments on a tracked document: what a person wrote about a range of it,
// with the quoted text. A comment often carries an obligation, an ask, or an
// open question: doc_comment_file turns it into a commitment, an Ask, or a
// question, and doc_comment_resolve closes it otherwise.

// DocCommentAddInput is the input of doc_comment_add. Quote is the text the
// comment is about, from version (default the latest); start and end are its
// place in that version's text in code points, when the source gives them.
type DocCommentAddInput struct {
	ID      string `json:"id"`
	By      string `json:"by"`   // person id, or free text such as a name
	Text    string `json:"text"` // the comment
	Quote   string `json:"quote,omitempty"`
	Version int    `json:"version,omitempty"`
	Start   *int   `json:"start,omitempty"`
	End     *int   `json:"end,omitempty"`
	URL     string `json:"url,omitempty"` // link to the comment
	At      string `json:"at,omitempty"`  // RFC 3339; default now
	// Became is the id of an Ask, question, or task that already holds it.
	Became string `json:"became,omitempty"`
}

// DocCommentAddOutput is the output of doc_comment_add.
type DocCommentAddOutput struct {
	ID      string           `json:"id"`
	Comment string           `json:"comment"` // the comment id, c<n>
	Anchor  *model.DocAnchor `json:"anchor,omitempty"`
	Rev     string           `json:"rev"`
}

// DocCommentAdd records a comment a person made on a tracked document.
var DocCommentAdd = defineSafe("doc_comment_add", "Record a comment a person made on a tracked document: by, text, the quote it is about (from version, default latest; with start and end when the source gives them), url, at, and became when an Ask, question, or task already holds it. The quote must be in that version", func(ctx context.Context, d Deps, in DocCommentAddInput) (DocCommentAddOutput, error) {
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocCommentAddOutput{}, err
	}
	text := cleanText(in.Text)
	if text == "" {
		return DocCommentAddOutput{}, errors.New("text is empty")
	}
	by, err := ioParty(ctx, d, "by", in.By)
	if err != nil {
		return DocCommentAddOutput{}, err
	}
	if by == "" {
		return DocCommentAddOutput{}, errors.New("by is empty: who wrote the comment")
	}
	at, err := ioTime(in.At)
	if err != nil {
		return DocCommentAddOutput{}, err
	}
	if at == "" {
		at = d.Store.Now().UTC().Format(time.RFC3339)
	}
	link, err := ioRef(in.URL)
	if err != nil {
		return DocCommentAddOutput{}, fmt.Errorf("url: %w", err)
	}
	c := model.DocComment{ID: nextCommentID(m.Comments), By: by, At: at, URL: link, Text: text, Quote: strings.TrimSpace(in.Quote), Version: in.Version}
	if c.Version == 0 {
		c.Version = m.Latest
	}
	if c.Version < 1 || c.Version > m.Latest {
		return DocCommentAddOutput{}, fmt.Errorf("version %d: %s has versions 1 to %d", c.Version, doc.ID, m.Latest)
	}
	if (in.Start == nil) != (in.End == nil) {
		return DocCommentAddOutput{}, errors.New("give start and end together")
	}
	if c.Quote == "" && in.Start != nil {
		return DocCommentAddOutput{}, errors.New("start and end place a quote: give quote")
	}
	if c.Quote != "" {
		v, err := readVersion(ctx, d, doc.ID, c.Version)
		if err != nil {
			return DocCommentAddOutput{}, err
		}
		if c.Start, c.End, err = placeQuote(v.text, c.Quote, in.Start, in.End); err != nil {
			return DocCommentAddOutput{}, fmt.Errorf("quote in version %d: %w", c.Version, err)
		}
		if c.Version < m.Latest {
			latest, err := readVersion(ctx, d, doc.ID, m.Latest)
			if err != nil {
				return DocCommentAddOutput{}, err
			}
			s, e, ok := locateQuote(latest.text, c.Quote, *c.Start)
			c.Anchor = &model.DocAnchor{Version: m.Latest, Start: s, End: e, Lost: !ok}
		}
	}
	if in.Became != "" {
		if c.Became, err = linkTo(ctx, d, "became", in.Became, model.TypeRFX, model.TypeQuestion, model.TypeTask); err != nil {
			return DocCommentAddOutput{}, err
		}
		c.Resolution, c.ResolvedAt = model.CommentBecame, at
	}
	m.Comments = append(m.Comments, c)
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return DocCommentAddOutput{}, err
	}
	return DocCommentAddOutput{ID: doc.ID, Comment: c.ID, Anchor: c.Anchor, Rev: doc.Rev}, nil
})

// placeQuote checks start and end against the text, or finds the quote. The
// result is the place in code points.
func placeQuote(text, quote string, start, end *int) (*int, *int, error) {
	r := []rune(text)
	if start != nil {
		s, e := *start, *end
		if s < 0 || e > len(r) || s >= e {
			return nil, nil, fmt.Errorf("start %d and end %d are outside the text (%d code points)", s, e, len(r))
		}
		if strings.Join(strings.Fields(string(r[s:e])), " ") != strings.Join(strings.Fields(quote), " ") {
			return nil, nil, fmt.Errorf("the text at %d to %d is not the quote", s, e)
		}
		return &s, &e, nil
	}
	s, e, ok := locateQuote(text, quote, 0)
	if !ok {
		return nil, nil, errors.New("not found: quote the document's words exactly")
	}
	return &s, &e, nil
}

func nextCommentID(cs []model.DocComment) string {
	n := 0
	for _, c := range cs {
		if k, err := strconv.Atoi(strings.TrimPrefix(c.ID, "c")); err == nil && k > n {
			n = k
		}
	}
	return "c" + strconv.Itoa(n+1)
}

func findComment(m *model.TrackedDoc, id string) (*model.DocComment, error) {
	id = strings.TrimSpace(id)
	for i := range m.Comments {
		if m.Comments[i].ID == id {
			return &m.Comments[i], nil
		}
	}
	return nil, fmt.Errorf("comment %q not found", id)
}

// CommentView is a comment with what it became.
type CommentView struct {
	model.DocComment `json:",inline"`
	// BecameTitle and BecameClosed describe the item it became.
	BecameTitle  string `json:"became_title,omitempty"`
	BecameClosed bool   `json:"became_closed,omitempty"`
	// Lost is true when the quote is not in the latest version.
	Lost bool `json:"lost"`
}

// DocCommentsInput is the input of doc_comments.
type DocCommentsInput struct {
	ID   string `json:"id" query:"id" required:"true"`
	Open bool   `json:"open,omitempty" query:"open"` // only comments with no resolution
}

// DocCommentsOutput is the output of doc_comments.
type DocCommentsOutput struct {
	ID       string        `json:"id"`
	Latest   int           `json:"latest"`
	Open     int           `json:"open"`
	Comments []CommentView `json:"comments"`
}

// DocComments lists the comments of a tracked document with their resolution.
var DocComments = defineRead("doc_comments", "List the comments of a tracked document with their resolution, what each became, and whether its quote is still in the latest version", func(ctx context.Context, d Deps, in DocCommentsInput) (DocCommentsOutput, error) {
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocCommentsOutput{}, err
	}
	out := DocCommentsOutput{ID: doc.ID, Latest: m.Latest, Comments: []CommentView{}}
	for _, c := range m.Comments {
		if c.Open() {
			out.Open++
		} else if in.Open {
			continue
		}
		v := CommentView{DocComment: c, Lost: c.Anchor != nil && c.Anchor.Lost}
		if id := c.Became.Target(); id != "" {
			if b, err := d.Store.Get(ctx, id); err == nil {
				v.BecameTitle, v.BecameClosed = b.Title, closesDoc(b)
			}
		}
		out.Comments = append(out.Comments, v)
	}
	return out, nil
})

// DocCommentResolveInput is the input of doc_comment_resolve.
type DocCommentResolveInput struct {
	ID         string                  `json:"id"`
	Comment    string                  `json:"comment"`
	Resolution model.CommentResolution `json:"resolution"`       // resolved, dropped, or became
	Became     string                  `json:"became,omitempty"` // with became: the Ask, question, or task id
	Note       string                  `json:"note,omitempty"`   // why; required for dropped
}

// DocCommentResolveOutput is the output of doc_comment_resolve.
type DocCommentResolveOutput struct {
	ID         string                  `json:"id"`
	Comment    string                  `json:"comment"`
	Resolution model.CommentResolution `json:"resolution"`
	Rev        string                  `json:"rev"`
}

// DocCommentResolve closes a comment: resolved, dropped with a note, or held
// by an item that exists.
var DocCommentResolve = define("doc_comment_resolve", "Close a comment of a tracked document: resolved (it needs nothing more), dropped (note says why), or became (an existing Ask, question, or task holds it). To make the item, use doc_comment_file", func(ctx context.Context, d Deps, in DocCommentResolveInput) (DocCommentResolveOutput, error) {
	return resolveComment(ctx, d, in)
})

func resolveComment(ctx context.Context, d Deps, in DocCommentResolveInput) (DocCommentResolveOutput, error) {
	if !slices.Contains(model.CommentResolutions, in.Resolution) {
		return DocCommentResolveOutput{}, fmt.Errorf("resolution %q: want resolved, dropped, or became", in.Resolution)
	}
	note := ioLine(in.Note)
	if in.Resolution == model.CommentDropped && note == "" {
		return DocCommentResolveOutput{}, errors.New("note is empty: say why the comment is dropped")
	}
	if (in.Resolution == model.CommentBecame) != (in.Became != "") {
		return DocCommentResolveOutput{}, errors.New("became goes with resolution became")
	}
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocCommentResolveOutput{}, err
	}
	c, err := findComment(m, in.Comment)
	if err != nil {
		return DocCommentResolveOutput{}, err
	}
	if !c.Open() {
		return DocCommentResolveOutput{}, fmt.Errorf("comment %s is already %s", c.ID, c.Resolution)
	}
	if in.Became != "" {
		if c.Became, err = linkTo(ctx, d, "became", in.Became, model.TypeRFX, model.TypeQuestion, model.TypeTask); err != nil {
			return DocCommentResolveOutput{}, err
		}
	}
	c.Resolution, c.Note, c.ResolvedAt = in.Resolution, note, d.Store.Now().UTC().Format(time.RFC3339)
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return DocCommentResolveOutput{}, err
	}
	return DocCommentResolveOutput{ID: doc.ID, Comment: c.ID, Resolution: c.Resolution, Rev: doc.Rev}, nil
}

// Comment file targets.
const (
	CommentToAsk        = "ask"
	CommentToQuestion   = "question"
	CommentToCommitment = "commitment"
)

// DocCommentFileInput is the input of doc_comment_file. The new item takes the
// tracked document's project or area unless one is given. An Ask is
// incoming (the commenter asks the owner) and goes to route, default the
// document's route, else its system. A commitment is due on due, default
// seven days on.
type DocCommentFileInput struct {
	ID       string `json:"id"`
	Comment  string `json:"comment"`
	To       string `json:"to"` // ask, question, or commitment
	Project  string `json:"project,omitempty"`
	Area     string `json:"area,omitempty"`
	Task     string `json:"task,omitempty"` // question: scope a task
	Root     bool   `json:"root,omitempty"` // question: scope the whole vault
	Route    string `json:"route,omitempty"`
	Due      string `json:"due,omitempty"`       // commitment: YYYY-MM-DD
	DoneWhen string `json:"done_when,omitempty"` // commitment: how we know it is done
}

// DocCommentFileOutput is the output of doc_comment_file.
type DocCommentFileOutput struct {
	ID       string     `json:"id"`
	Comment  string     `json:"comment"`
	Became   string     `json:"became"`
	Type     model.Type `json:"type"`
	Warnings []string   `json:"warnings"`
	Rev      string     `json:"rev"`
}

// DocCommentFile turns a comment into an Ask, a question, or a commitment and
// links it.
var DocCommentFile = defineSafe("doc_comment_file", "Turn a comment of a tracked document into an Ask (the commenter asks the owner), a question, or a commitment (a task the owner owes, due default a week on), in the document's project or area unless given; the comment links it and closes as became", func(ctx context.Context, d Deps, in DocCommentFileInput) (DocCommentFileOutput, error) {
	return fileComment(ctx, d, in)
})

func fileComment(ctx context.Context, d Deps, in DocCommentFileInput) (DocCommentFileOutput, error) {
	doc, m, err := getTracked(ctx, d, in.ID)
	if err != nil {
		return DocCommentFileOutput{}, err
	}
	c, err := findComment(m, in.Comment)
	if err != nil {
		return DocCommentFileOutput{}, err
	}
	if !c.Open() {
		return DocCommentFileOutput{}, fmt.Errorf("comment %s is already %s", c.ID, c.Resolution)
	}
	out := DocCommentFileOutput{ID: doc.ID, Comment: c.ID, Warnings: []string{}}
	project, area := in.Project, in.Area
	if project == "" && area == "" && in.Task == "" && !in.Root {
		project, area = m.Project.Target(), m.Area.Target()
		if project != "" {
			area = ""
		}
	}
	if in.To != CommentToCommitment && (in.Due != "" || in.DoneWhen != "") {
		return out, errors.New("due and done_when are for a commitment")
	}
	if in.To != CommentToQuestion && (in.Task != "" || in.Root) {
		return out, errors.New("task and root scope a question")
	}
	if in.To != CommentToAsk && in.Route != "" {
		return out, errors.New("route is for an Ask")
	}
	text := commentText(doc, *c)
	switch in.To {
	case CommentToAsk:
		route := firstOf(ioLine(in.Route), m.Route, string(m.System))
		var tg []TargetInput
		if id := ioLinks([]string{c.By}); len(id) > 0 {
			tg = append(tg, TargetInput{To: id[0], Medium: mediumOf(m.System)})
		}
		r, err := RFXAdd.Run(ctx, d, RFXAddInput{Direction: model.RFXIn, Ask: text, Route: route, Targets: tg, Project: project})
		if err != nil {
			return out, err
		}
		out.Became, out.Type, out.Warnings = r.ID, model.TypeRFX, r.Warnings
	case CommentToQuestion:
		root := in.Root || (project == "" && area == "" && in.Task == "")
		r, err := QuestionAdd.Run(ctx, d, QuestionAddInput{Text: text, Project: project, Area: area, Task: in.Task, Root: root})
		if err != nil {
			return out, err
		}
		out.Became, out.Type = r.ID, model.TypeQuestion
	case CommentToCommitment:
		if project == "" && area == "" {
			return out, fmt.Errorf("%s has no project or area: give project or area for the commitment", doc.ID)
		}
		due := in.Due
		if due == "" {
			due = d.Store.Now().AddDate(0, 0, 7).Format(dateLayout)
		}
		title, body, err := splitCapture(text)
		if err != nil {
			return out, err
		}
		r, err := TaskAdd.Run(ctx, d, TaskAddInput{Project: project, Area: area, Title: title, Body: body, Due: due, Kind: model.KindCommitment,
			DoneWhen: firstOf(ioLine(in.DoneWhen), "The comment "+c.ID+" on "+doc.Title+" is answered or done.")})
		if err != nil {
			return out, err
		}
		out.Became, out.Type = r.ID, model.TypeTask
	default:
		return out, fmt.Errorf("to %q: want ask, question, or commitment", in.To)
	}
	res, err := resolveComment(ctx, d, DocCommentResolveInput{ID: doc.ID, Comment: c.ID, Resolution: model.CommentBecame, Became: out.Became})
	if err != nil {
		return out, fmt.Errorf("%s was made, but comment %s was not linked: %w", out.Became, c.ID, err)
	}
	out.Rev = res.Rev
	return out, nil
}

// commentText is a comment as the first line of an item and its body: who
// said what, the quote, and where.
func commentText(doc model.Document, c model.DocComment) string {
	first, rest, _ := strings.Cut(c.Text, "\n")
	who := c.By
	parts := []string{ioLine(first), strings.TrimSpace(rest)}
	if c.Quote != "" {
		parts = append(parts, "> "+strings.ReplaceAll(c.Quote, "\n", "\n> "))
	}
	where := fmt.Sprintf("Comment %s by %s on [[%s]] (version %d).", c.ID, who, doc.ID, c.Version)
	if c.URL != "" {
		where += " " + c.URL
	}
	return joinBody(append(parts, where)...)
}
