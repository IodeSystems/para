package ops

import (
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// This file is the one mapping table of vendor names in internal/ops (a test
// refuses them anywhere else here, but the export renderers). Code holds
// mechanism: a source is a class (mail, chat, tracker, wiki, calendar,
// other) plus a free vendor label. Who and what matters is vault data.
//
// An older source names a product in system (gmail, slack, jira, ...). It
// reads as an alias of a class and a vendor through legacySystems; its file
// is not rewritten. source_add, and doc_edit of system or vendor, write the
// class and the vendor.

// sourceKind is the class and the vendor of a source.
type sourceKind struct {
	class  model.SourceSystem
	vendor string
}

// legacySystems maps each older system value to its class and vendor.
var legacySystems = map[model.SourceSystem]sourceKind{
	model.SourceGmail:      {model.SourceMail, "gmail"},
	model.SourceGDocs:      {model.SourceWiki, "gdocs"},
	model.SourceGCal:       {model.SourceCalendar, "gcal"},
	model.SourceSlack:      {model.SourceChat, "slack"},
	model.SourceGitHub:     {model.SourceTracker, "github"},
	model.SourceJira:       {model.SourceTracker, "jira"},
	model.SourceConfluence: {model.SourceWiki, "confluence"},
	model.SourceFolder:     {model.SourceOther, "folder"},
}

// classMedium is the io and Ask medium of a class whatever its vendor.
var classMedium = map[model.SourceSystem]model.Medium{
	model.SourceMail:     model.MediumEmail,
	model.SourceCalendar: model.MediumMeeting,
}

// vendorMedium is the medium of a vendor whose messages have a medium of
// their own. Any other vendor of chat, tracker, or wiki has none: matching
// by medium is skipped and the io item takes medium other.
var vendorMedium = map[string]model.Medium{
	"slack":      model.MediumSlack,
	"teams":      model.MediumTeams,
	"jira":       model.MediumJira,
	"confluence": model.MediumConfluence,
	"github":     model.MediumGithub,
}

// vendorDocSystem is the tracked_doc system of a vendor.
var vendorDocSystem = map[string]model.DocSystem{
	"gdocs":      model.SystemGDoc,
	"jira":       model.SystemJira,
	"confluence": model.SystemConfluence,
	"github":     model.SystemGitHub,
}

// docSystemMedium is the medium of a comment made in a tracked_doc system.
var docSystemMedium = map[model.DocSystem]model.Medium{
	model.SystemConfluence: model.MediumConfluence,
	model.SystemJira:       model.MediumJira,
	model.SystemGitHub:     model.MediumGithub,
}

// mediumWords names a medium for a person.
var mediumWords = map[model.Medium]string{
	model.MediumEmail: "Email", model.MediumSlack: "Slack message", model.MediumJira: "Jira ticket",
	model.MediumConfluence: "Confluence page", model.MediumGithub: "GitHub message", model.MediumTeams: "Teams message",
	model.MediumPhone: "Phone call", model.MediumMeeting: "Meeting", model.MediumOther: "Message",
}

// normVendor is a vendor label as compared: trimmed, lower case.
func normVendor(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// sourceKindOf is the class and vendor of a source: an older system value through
// legacySystems (a vendor set on the file wins), else system and vendor as
// written.
func sourceKindOf(m *model.SourceDoc) sourceKind {
	if k, ok := legacySystems[m.System]; ok {
		if v := normVendor(m.Vendor); v != "" {
			k.vendor = v
		}
		return k
	}
	return sourceKind{class: m.System, vendor: normVendor(m.Vendor)}
}

// newForm writes a source's class and vendor in place of an older system.
func newForm(m *model.SourceDoc) {
	k := sourceKindOf(m)
	m.System, m.Vendor = k.class, k.vendor
}

// mediumOfSource is the io and Ask medium of a source: by class, else by
// vendor, else "".
func mediumOfSource(m *model.SourceDoc) model.Medium {
	k := sourceKindOf(m)
	if md, ok := classMedium[k.class]; ok {
		return md
	}
	return vendorMedium[k.vendor]
}

// docSystemOfSource is the tracked_doc system of a source, or "".
func docSystemOfSource(m *model.SourceDoc) model.DocSystem {
	return vendorDocSystem[sourceKindOf(m).vendor]
}

// mediumOf is the medium of a comment made in a tracked_doc system.
func mediumOf(s model.DocSystem) model.Medium {
	if md, ok := docSystemMedium[s]; ok {
		return md
	}
	return model.MediumOther
}
