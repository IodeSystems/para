package model

// Upgrade step small-types (vault format 10) folds the small types into the
// kinds that hold them: a team, a role, or a domain is a group of that kind;
// an interaction is a closed io item (a logged contact); an incident is a
// task of kind incident. These read either form as the older type, so a
// reader gives the same answer before and after the step.

// TaskStatusOf is the task status that holds an incident status.
func TaskStatusOf(s IncidentStatus) TaskStatus {
	switch s {
	case IncidentMitigated:
		return TaskDoing
	case IncidentResolved:
		return TaskDone
	}
	return TaskTodo
}

// IncidentStatusOf is the incident status a task status holds: todo and
// blocked are open, doing mitigated, done and dropped resolved.
func IncidentStatusOf(s TaskStatus) IncidentStatus {
	switch s {
	case TaskDoing:
		return IncidentMitigated
	case TaskDone, TaskDropped:
		return IncidentResolved
	}
	return IncidentOpen
}

// IsIncident reports whether the document is an incident in either form.
func IsIncident(d Document) bool {
	_, ok := IncidentOf(d)
	return ok
}

// IncidentOf reads an incident file, or a task of kind incident, as an
// incident. For a task it is a copy: write the task (SetIncident).
func IncidentOf(d Document) (*Incident, bool) {
	switch m := d.Meta.(type) {
	case *Incident:
		return m, true
	case *Task:
		if m.Kind != KindIncident {
			return nil, false
		}
		return &Incident{Common: m.Common, Severity: m.Severity, Status: IncidentStatusOf(m.Status), Started: m.Started,
			Ended: m.Ended, Area: m.Area, Links: m.Links}, true
	}
	return nil, false
}

// SetIncident writes the incident fields of i into the document: the file
// itself, or the task of kind incident that holds it.
func SetIncident(d Document, i *Incident) {
	switch m := d.Meta.(type) {
	case *Incident:
		if m != i {
			*m = *i
		}
	case *Task:
		m.Severity, m.Status, m.Started, m.Ended, m.Area, m.Links = i.Severity, TaskStatusOf(i.Status), i.Started, i.Ended, i.Area, i.Links
	}
}

// InteractionOf reads an interaction file, or a logged io item, as an
// interaction.
func InteractionOf(d Document) (*Interaction, bool) {
	switch m := d.Meta.(type) {
	case *Interaction:
		return m, true
	case *IO:
		if !m.Logged() {
			return nil, false
		}
		dir := InteractionDirection(m.Direction)
		if m.Observed {
			dir = InteractionObserved
		}
		return &Interaction{Common: m.Common, Date: m.Date, With: m.With, Medium: m.Medium, Direction: dir, Visibility: m.Visibility, URL: m.URL}, true
	}
	return nil, false
}

// OrgKind is the older org type a document is: team, role, or domain for
// those files and for a group of that kind; else its own type.
func OrgKind(d Document) Type {
	if g, ok := d.Meta.(*Group); ok {
		switch g.Kind {
		case GroupTeam:
			return TypeTeam
		case GroupRole:
			return TypeRole
		case GroupDomain:
			return TypeDomain
		}
	}
	return d.Meta.Header().Type
}

// DomainOwner is the owner of a domain file or of a group of kind domain.
func DomainOwner(d Document) (Link, bool) {
	switch m := d.Meta.(type) {
	case *Domain:
		return m.Owner, true
	case *Group:
		if m.Kind == GroupDomain {
			return m.Owner, true
		}
	}
	return "", false
}

// GroupKindOf is the group kind a team, role, or domain becomes.
func GroupKindOf(t Type) (GroupKind, bool) {
	switch t {
	case TypeTeam:
		return GroupTeam, true
	case TypeRole:
		return GroupRole, true
	case TypeDomain:
		return GroupDomain, true
	}
	return "", false
}
