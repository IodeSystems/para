package model

import "gopkg.in/yaml.v3"

// Below GoalsFormat a goal is a file of type project, and a task, note,
// obligation, rfx, or tracked doc links its goal with project:. The vault
// reads such a file in the format-11 form (LegacyRead) and writes it back in
// the old form (LegacyWrite), so a machine on an older para still reads what
// this one writes until the owner runs upgrade step goals.

// LegacyGoalKeyTypes are the types whose project: key links a goal below
// GoalsFormat.
var LegacyGoalKeyTypes = []Type{TypeTask, TypeNote, TypeObligation, TypeRFX, TypeTrackedDoc}

func legacyGoalKey(t Type) bool {
	for _, x := range LegacyGoalKeyTypes {
		if x == t {
			return true
		}
	}
	return false
}

// LegacyType is the format-11 type of a file whose type key is t, below
// GoalsFormat: project is goal.
func LegacyType(t Type) Type {
	if t == TypeProject {
		return TypeGoal
	}
	return t
}

// LegacyRead returns the frontmatter mapping root of a file below GoalsFormat
// in the format-11 form: type project is goal, and on LegacyGoalKeyTypes the
// key project is goal (unless goal is set too). root is not changed: a
// mapping that needs a change is copied. A nil or non-mapping root is
// returned as it is.
func LegacyRead(root *yaml.Node) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode {
		return root
	}
	typ, hasGoal := "", false
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "type":
			typ = root.Content[i+1].Value
		case "goal":
			hasGoal = true
		}
	}
	retype := Type(typ) == TypeProject
	rekey := legacyGoalKey(Type(typ)) && !hasGoal
	if !retype && !rekey {
		return root
	}
	out := *root
	out.Content = make([]*yaml.Node, len(root.Content))
	copy(out.Content, root.Content)
	for i := 0; i+1 < len(out.Content); i += 2 {
		k := out.Content[i]
		switch {
		case retype && k.Value == "type":
			v := *out.Content[i+1]
			v.Value = string(TypeGoal)
			out.Content[i+1] = &v
		case rekey && k.Value == "project":
			nk := *k
			nk.Value = "goal"
			out.Content[i] = &nk
		}
	}
	return &out
}

// LegacyReadMap is LegacyRead on a decoded frontmatter map, changed in place.
func LegacyReadMap(fm map[string]any) {
	typ, _ := fm["type"].(string)
	if Type(typ) == TypeProject {
		fm["type"] = string(TypeGoal)
		return
	}
	if legacyGoalKey(Type(typ)) {
		if v, ok := fm["project"]; ok {
			if _, has := fm["goal"]; !has {
				fm["goal"] = v
				delete(fm, "project")
			}
		}
	}
}

// LegacyWrite returns d in the form a file below GoalsFormat has: a goal is
// written as type project, and on LegacyGoalKeyTypes the goal link is written
// as project:. d itself is not changed.
func LegacyWrite(d Document) Document {
	switch m := d.Meta.(type) {
	case *Goal:
		c := *m
		c.Type = TypeProject
		d.Meta = &c
	case *Task:
		c := *m
		c.Project, c.Goal = legacyLink(c.Project, c.Goal)
		d.Meta = &c
	case *Note:
		c := *m
		c.Project, c.Goal = legacyLink(c.Project, c.Goal)
		d.Meta = &c
	case *Obligation:
		c := *m
		c.Project, c.Goal = legacyLink(c.Project, c.Goal)
		d.Meta = &c
	case *RFX:
		c := *m
		c.Project, c.Goal = legacyLink(c.Project, c.Goal)
		d.Meta = &c
	case *TrackedDoc:
		c := *m
		c.Project, c.Goal = legacyLink(c.Project, c.Goal)
		d.Meta = &c
	}
	return d
}

// legacyLink moves a goal link to project when project is free.
func legacyLink(project, goal Link) (Link, Link) {
	if goal == "" || project != "" {
		return project, goal
	}
	return goal, ""
}
