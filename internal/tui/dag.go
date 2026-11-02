package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// DAG renderer for para's own graph data (tasks and requests). Layered, top
// down: an edge runs from a prerequisite to what depends on it. Long edges
// pass through one-column dummy nodes. A cycle is never dropped: the layering
// treats one edge of each cycle as reversed, the edge is drawn with its
// arrowhead pointing up, and a legend lists every cycle.

// DNode is a node. Label is truncated to fit.
type DNode struct{ ID, Label string }

// DEdge runs From (above) to To (below).
type DEdge struct{ From, To string }

type dnode struct {
	id    string
	label string
	lines []string // the label, wrapped to at most two lines
	dummy bool
	layer int
	x, w  int
}

type dseg struct {
	a, b   *dnode // a is the upper node
	edge   int    // index of the edge it belongs to
	head   bool   // carries the arrowhead
	headUp bool   // the arrowhead points up, at a: a reversed edge
	track  int
	sx, tx int // port columns on a and on b
}

// Line-drawing masks.
const (
	mU = 1 << iota
	mD
	mL
	mR
)

var maskRune = map[uint8]rune{
	mU: '│', mD: '│', mU | mD: '│', mL: '─', mR: '─', mL | mR: '─',
	mD | mR: '┌', mD | mL: '┐', mU | mR: '└', mU | mL: '┘',
	mU | mD | mR: '├', mU | mD | mL: '┤', mL | mR | mD: '┬', mL | mR | mU: '┴',
	mU | mD | mL | mR: '┼',
}

// crossRune marks two lines that pass through each other without joining.
// A '┼' is a junction: some edge turns there.
const crossRune = '╫'

const skipCell = -1 // second cell of a wide rune

// RenderDAG draws the graph at most width cells wide where it can. cycles
// lists node ids in cycle order, for the legend; a one-node cycle is a self
// loop and is marked on the node.
func RenderDAG(nodes []DNode, edges []DEdge, cycles [][]string, width int) string {
	byID := map[string]*dnode{}
	for _, n := range nodes {
		byID[n.ID] = &dnode{id: n.ID, label: n.Label}
	}
	// Edges: drop duplicates, edges to unknown nodes, and self loops (marked).
	selfLoop := map[string]bool{}
	var es []DEdge
	seenEdge := map[DEdge]bool{}
	for _, e := range edges {
		if byID[e.From] == nil || byID[e.To] == nil || seenEdge[e] {
			continue
		}
		seenEdge[e] = true
		if e.From == e.To {
			selfLoop[e.From] = true
			continue
		}
		es = append(es, e)
	}
	for _, c := range cycles {
		if len(c) == 1 {
			selfLoop[c[0]] = true
		}
	}
	for id := range selfLoop {
		byID[id].label += " ↻"
	}

	// Back edges by depth-first search, in node order: reversing them makes
	// the graph acyclic.
	out := map[string][]int{}
	for i, e := range es {
		out[e.From] = append(out[e.From], i)
	}
	reversed := map[int]bool{}
	state := map[string]int{} // 1 on the stack, 2 done
	var dfs func(id string)
	dfs = func(id string) {
		state[id] = 1
		for _, ei := range out[id] {
			switch to := es[ei].To; state[to] {
			case 0:
				dfs(to)
			case 1:
				reversed[ei] = true
			}
		}
		state[id] = 2
	}
	for _, n := range nodes {
		if state[n.ID] == 0 {
			dfs(n.ID)
		}
	}
	// Layering: longest path on the acyclic graph.
	up := map[string][]string{} // layering parents
	for i, e := range es {
		from, to := e.From, e.To
		if reversed[i] {
			from, to = to, from
		}
		up[to] = append(up[to], from)
	}
	var depth func(id string) int
	memo := map[string]int{}
	depth = func(id string) int {
		if d, ok := memo[id]; ok {
			return d
		}
		memo[id] = 0
		d := 0
		for _, p := range up[id] {
			d = max(d, depth(p)+1)
		}
		memo[id] = d
		return d
	}
	for _, n := range nodes {
		byID[n.ID].layer = depth(n.ID)
	}

	// Segments, with dummy nodes on long edges.
	var segs []dseg
	nd := 0
	var dummies []*dnode
	for i, e := range es {
		a, b := byID[e.From], byID[e.To]
		rev := reversed[i]
		if rev {
			a, b = b, a
		}
		prev := a
		first := true
		for l := a.layer + 1; l < b.layer; l++ {
			nd++
			d := &dnode{id: "~" + itoa(nd), dummy: true, layer: l, w: 1}
			dummies = append(dummies, d)
			segs = append(segs, dseg{a: prev, b: d, edge: i, head: rev && first, headUp: rev})
			prev, first = d, false
		}
		segs = append(segs, dseg{a: prev, b: b, edge: i, head: !rev || first, headUp: rev})
	}

	maxL := 0
	for _, n := range byID {
		maxL = max(maxL, n.layer)
	}
	layers := make([][]*dnode, maxL+1)
	for _, n := range nodes {
		layers[byID[n.ID].layer] = append(layers[byID[n.ID].layer], byID[n.ID])
	}
	for _, d := range dummies {
		layers[d.layer] = append(layers[d.layer], d)
	}
	for _, l := range layers {
		sort.SliceStable(l, func(i, j int) bool { return l[i].id < l[j].id })
	}
	order(layers, segs)

	// Shrink labels until the widest layer fits.
	const gap = 2
	maxLabel := 28
	fit := func() int {
		w := 0
		for _, l := range layers {
			s := 0
			for i, n := range l {
				if i > 0 {
					s += gap
				}
				if n.dummy {
					s++
				} else {
					s += min(cells(n.label), maxLabel) + 4
				}
			}
			w = max(w, s)
		}
		return w
	}
	for maxLabel > 4 && fit() > width {
		maxLabel--
	}
	for _, n := range byID {
		n.lines = wrapLabel(n.label, maxLabel)
		for _, l := range n.lines {
			n.w = max(n.w, cells(l)+4)
		}
	}
	if place(layers, segs, gap, true) > width {
		place(layers, segs, gap, false)
	}
	return draw(layers, segs, reversed, cycles, gap)
}

// order sorts each layer by the barycenter of its neighbours, a few sweeps.
func order(layers [][]*dnode, segs []dseg) {
	pos := func() map[*dnode]int {
		m := map[*dnode]int{}
		for _, l := range layers {
			for i, n := range l {
				m[n] = i
			}
		}
		return m
	}
	for pass := 0; pass < 6; pass++ {
		p := pos()
		for _, l := range layers {
			bc := map[*dnode]float64{}
			for _, n := range l {
				s, c := 0.0, 0
				for _, sg := range segs {
					if pass%2 == 0 && sg.b == n {
						s += float64(p[sg.a])
						c++
					}
					if pass%2 == 1 && sg.a == n {
						s += float64(p[sg.b])
						c++
					}
				}
				bc[n] = float64(p[n])
				if c > 0 {
					bc[n] = s / float64(c)
				}
			}
			sort.SliceStable(l, func(i, j int) bool { return bc[l[i]] < bc[l[j]] })
		}
	}
}

// place sets x. With pull, a node sits under the mean of its parents.
func place(layers [][]*dnode, segs []dseg, gap int, pull bool) int {
	tw := 0
	for li, l := range layers {
		if li == 0 {
			x := 0
			for _, n := range l {
				n.x = x
				x += n.w + gap
			}
		} else {
			des := map[*dnode]float64{}
			for _, n := range l {
				s, c := 0.0, 0
				for _, sg := range segs {
					if sg.b == n {
						s += float64(sg.a.x) + float64(sg.a.w)/2
						c++
					}
				}
				if c > 0 && pull {
					des[n] = s/float64(c) - float64(n.w)/2
				}
			}
			sort.SliceStable(l, func(i, j int) bool { return des[l[i]] < des[l[j]] })
			x := 0
			for _, n := range l {
				n.x = max(x, int(des[n]+0.5))
				x = n.x + n.w + gap
			}
		}
		for _, n := range l {
			tw = max(tw, n.x+n.w)
		}
	}
	return tw
}

// wrapLabel breaks a label into at most two lines of w cells, at a space when
// there is one; text that still does not fit ends in an ellipsis.
func wrapLabel(label string, w int) []string {
	if cells(label) <= w {
		return []string{label}
	}
	rs := []rune(label)
	cut := w
	for i := min(w, len(rs)-1); i > 0; i-- {
		if rs[i] == ' ' {
			cut = i
			break
		}
	}
	first := strings.TrimRight(string(rs[:min(cut, len(rs))]), " ")
	rest := strings.TrimLeft(string(rs[min(cut, len(rs)):]), " ")
	return []string{first, truncate(rest, w)}
}

func draw(layers [][]*dnode, segs []dseg, reversed map[int]bool, cycles [][]string, gap int) string {
	maxL := len(layers) - 1
	nh := 3 // rows of a node: border, one or two lines of label, border
	for _, l := range layers {
		for _, n := range l {
			if len(n.lines) > 1 {
				nh = 4
			}
		}
	}
	gapSegs := make([][]*dseg, maxL+1)
	for i := range segs {
		s := &segs[i]
		gapSegs[s.a.layer] = append(gapSegs[s.a.layer], s)
	}
	cx := func(n *dnode) int { return n.x + n.w/2 }
	assignPorts(segs, cx)
	gapH := make([]int, maxL+1)
	for g := 0; g < maxL; g++ {
		var tracks [][]*dseg
	next:
		for _, s := range gapSegs[g] {
			if s.sx == s.tx {
				s.track = -1
				continue
			}
			lo, hi := min(s.sx, s.tx), max(s.sx, s.tx)
			for ti, t := range tracks {
				ok := true
				for _, o := range t {
					olo, ohi := min(o.sx, o.tx), max(o.sx, o.tx)
					if lo <= ohi+1 && olo <= hi+1 {
						ok = false
						break
					}
				}
				if ok {
					tracks[ti] = append(t, s)
					s.track = ti
					continue next
				}
			}
			tracks = append(tracks, []*dseg{s})
			s.track = len(tracks) - 1
		}
		gapH[g] = len(tracks) + 2
	}
	y := make([]int, maxL+2)
	for l := 0; l <= maxL; l++ {
		y[l+1] = y[l] + nh + gapH[l]
	}
	H := y[maxL+1] - gapH[maxL]
	W := 0
	for _, l := range layers {
		for _, n := range l {
			W = max(W, n.x+n.w)
		}
	}
	mask := make([][]uint8, H)
	text := make([][]rune, H)
	ownV := make([][][]int, H) // edges that drew a vertical bit in the cell
	ownH := make([][][]int, H)
	for i := range mask {
		mask[i] = make([]uint8, W)
		text[i] = make([]rune, W)
		ownV[i] = make([][]int, W)
		ownH[i] = make([][]int, W)
	}
	put := func(r, c int, m uint8, edge int) {
		if r < 0 || r >= H || c < 0 || c >= W {
			return
		}
		mask[r][c] |= m
		if m&(mU|mD) != 0 {
			ownV[r][c] = append(ownV[r][c], edge)
		}
		if m&(mL|mR) != 0 {
			ownH[r][c] = append(ownH[r][c], edge)
		}
	}
	vline := func(c, r0, r1, edge int) {
		for r := r0; r <= r1; r++ {
			if r > r0 {
				put(r, c, mU, edge)
			}
			if r < r1 {
				put(r, c, mD, edge)
			}
		}
	}
	for g := 0; g < maxL; g++ {
		top := y[g] + nh
		bot := top + gapH[g] - 1
		for _, s := range gapSegs[g] {
			xa, xb := s.sx, s.tx
			if s.track < 0 {
				for r := top; r <= bot; r++ {
					put(r, xa, mU|mD, s.edge)
				}
			} else {
				tr := top + 1 + s.track
				vline(xa, top, tr, s.edge)
				put(top, xa, mD, s.edge)
				lo, hi := min(xa, xb), max(xa, xb)
				for c := lo; c <= hi; c++ {
					if c > lo {
						put(tr, c, mL, s.edge)
					}
					if c < hi {
						put(tr, c, mR, s.edge)
					}
				}
				vline(xb, tr, bot, s.edge)
			}
			if s.head {
				if s.headUp {
					text[top][xa] = '▲'
				} else {
					text[bot][xb] = '▼'
				}
			}
		}
	}
	for li, l := range layers {
		for _, n := range l {
			if n.dummy {
				for r := y[li]; r < y[li]+nh; r++ {
					put(r, n.x, mU|mD, -1)
				}
				continue
			}
			r0 := y[li]
			rb := r0 + nh - 1
			text[r0][n.x] = '┌'
			text[r0][n.x+n.w-1] = '┐'
			text[rb][n.x] = '└'
			text[rb][n.x+n.w-1] = '┘'
			for c := n.x + 1; c < n.x+n.w-1; c++ {
				text[r0][c] = '─'
				text[rb][c] = '─'
			}
			for i := 1; i < nh-1; i++ {
				text[r0+i][n.x] = '│'
				text[r0+i][n.x+n.w-1] = '│'
				if i-1 >= len(n.lines) {
					continue
				}
				c := n.x + 2
				for _, r := range n.lines[i-1] {
					text[r0+i][c] = r
					if w := ansi.StringWidth(string(r)); w == 2 {
						text[r0+i][c+1] = skipCell
						c += 2
					} else {
						c++
					}
				}
			}
			if li < maxL {
				for _, s := range gapSegs[li] {
					if s.a == n && text[rb][s.sx] == '─' {
						text[rb][s.sx] = '┬'
					}
				}
			}
		}
	}
	var sb strings.Builder
	for r := 0; r < H; r++ {
		var line strings.Builder
		for c := 0; c < W; c++ {
			switch {
			case text[r][c] == skipCell:
			case text[r][c] != 0:
				line.WriteRune(text[r][c])
			case mask[r][c] != 0:
				line.WriteRune(junction(mask[r][c], ownV[r][c], ownH[r][c]))
			default:
				line.WriteByte(' ')
			}
		}
		sb.WriteString(strings.TrimRight(line.String(), " "))
		sb.WriteByte('\n')
	}
	return sb.String() + legend(cycles)
}

// assignPorts gives each edge its own column on the border of the nodes it
// leaves and enters, spread across the box and ordered by where the other end
// is, so edges do not share an arrowhead. A node with more edges than interior
// columns shares columns.
func assignPorts(segs []dseg, cx func(*dnode) int) {
	out := map[*dnode][]*dseg{}
	in := map[*dnode][]*dseg{}
	for i := range segs {
		s := &segs[i]
		out[s.a] = append(out[s.a], s)
		in[s.b] = append(in[s.b], s)
	}
	spread := func(n *dnode, k, j int) int {
		if n.dummy || k <= 1 {
			return cx(n)
		}
		inner := n.w - 2
		if k > inner {
			j = j * inner / k
			k = inner
		}
		if k <= 1 {
			return cx(n)
		}
		return n.x + 1 + j*(inner-1)/(k-1)
	}
	for n, ss := range out {
		sort.SliceStable(ss, func(i, j int) bool { return cx(ss[i].b) < cx(ss[j].b) })
		for j, s := range ss {
			s.sx = spread(n, len(ss), j)
		}
	}
	for n, ss := range in {
		sort.SliceStable(ss, func(i, j int) bool { return cx(ss[i].a) < cx(ss[j].a) })
		for j, s := range ss {
			s.tx = spread(n, len(ss), j)
		}
	}
}

// junction picks the glyph for a cell. A full cross is a junction ('┼') when
// one edge drew both a vertical and a horizontal bit there, which is a turn;
// otherwise two lines pass through each other, drawn as crossRune.
func junction(m uint8, v, h []int) rune {
	if m == mU|mD|mL|mR {
		for _, a := range v {
			for _, b := range h {
				if a == b {
					return '┼'
				}
			}
		}
		return crossRune
	}
	return maskRune[m]
}

// legend lists each cycle, closed, so none is hidden by the layout.
func legend(cycles [][]string) string {
	var sb strings.Builder
	for _, c := range cycles {
		if len(c) == 0 {
			continue
		}
		sb.WriteString("cycle: " + strings.Join(c, " -> ") + " -> " + c[0] + "\n")
	}
	return sb.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
