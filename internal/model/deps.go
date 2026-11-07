package model

// Dependencies of a work item (task, obligation, rfx, question). depends_on
// holds hard dependencies: the item waits on each until it closes. Every
// walker that holds an item back (needs, the value order, reasons, the
// obligation hub, cycle checks) reads HardDeps and nothing else.
// optional_after holds optional ones ("better before"): shown with the item,
// never holding it, so they give no wait, no start date, no inherited due
// date, and no cycle. A link is in at most one of the two lists.

// HardDeps is the depends_on of a work item, or nil.
func HardDeps(m Meta) []Link {
	h, _ := DepLists(m)
	if h == nil {
		return nil
	}
	return *h
}

// OptionalDeps is the optional_after of a work item, or nil.
func OptionalDeps(m Meta) []Link {
	_, o := DepLists(m)
	if o == nil {
		return nil
	}
	return *o
}

// DepLists returns the depends_on and optional_after lists of a work item to
// change in place, or nil and nil for a type that has none.
func DepLists(m Meta) (hard, optional *[]Link) {
	switch x := m.(type) {
	case *Task:
		return &x.DependsOn, &x.OptionalAfter
	case *Obligation:
		return &x.DependsOn, &x.OptionalAfter
	case *RFX:
		return &x.DependsOn, &x.OptionalAfter
	case *Question:
		return &x.DependsOn, &x.OptionalAfter
	}
	return nil, nil
}
