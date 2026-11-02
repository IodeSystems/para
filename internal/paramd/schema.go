package paramd

type kind int

const (
	kString kind = iota
	kDate
	kBool
	kEnum
	kList
	kLink      // "[[name]]"
	kRuleLink  // "[[name#^R4]]"
	kSourceMap // source block
)

type field struct {
	Name     string
	Kind     kind
	Enum     []string
	Required bool
}

var (
	projectStatus   = []string{"active", "paused", "done", "dropped"}
	taskStatus      = []string{"todo", "doing", "blocked", "done", "dropped"}
	rfxDirection    = []string{"in", "out"}
	rfxStatus       = []string{"open", "answered", "closed", "declined"}
	fidelities      = []string{"exact", "ocr", "partial", "none"}
	exceptionKinds  = []string{"underspecified", "overly_broad", "bug", "unenforced", "conflict_with", "interpreted_as"}
	exceptionStatus = []string{"open", "resolved", "accepted"}
	conventionState = []string{"draft", "adopted", "retired"}
	journalScopes   = []string{"day", "week", "project"}
)

// Types lists every valid value of the frontmatter "type" field.
var Types = []string{
	"project", "task", "rfx", "constraint", "policy", "transcript",
	"policy_exception", "person", "team", "role", "domain", "convention", "journal", "inbox",
}

// commonFields apply to every type.
var commonFields = []field{
	{Name: "type", Kind: kEnum, Enum: Types, Required: true},
	{Name: "created", Kind: kDate, Required: true},
	{Name: "updated", Kind: kDate},
	{Name: "tags", Kind: kList},
}

// typeFields are the per-type fields, on top of commonFields.
var typeFields = map[string][]field{
	"project": {
		{Name: "status", Kind: kEnum, Enum: projectStatus, Required: true},
		{Name: "owner", Kind: kString, Required: true},
	},
	"task": {
		{Name: "status", Kind: kEnum, Enum: taskStatus, Required: true},
		{Name: "project", Kind: kLink, Required: true},
		{Name: "blocked_by", Kind: kList},
		{Name: "depends_on", Kind: kList},
	},
	"rfx": {
		{Name: "direction", Kind: kEnum, Enum: rfxDirection, Required: true},
		{Name: "status", Kind: kEnum, Enum: rfxStatus, Required: true},
		{Name: "route", Kind: kString, Required: true},
		{Name: "self_serve_check", Kind: kString, Required: true},
		{Name: "project", Kind: kLink},
	},
	"constraint": {
		{Name: "why", Kind: kString, Required: true},
		{Name: "by", Kind: kString, Required: true},
		{Name: "source", Kind: kSourceMap, Required: true},
		{Name: "applies_in", Kind: kList, Required: true},
		{Name: "not_in", Kind: kList, Required: true},
		{Name: "review_by", Kind: kDate, Required: true},
	},
	"policy": {
		{Name: "source", Kind: kSourceMap, Required: true},
		{Name: "verified", Kind: kBool, Required: true},
	},
	"transcript": {
		{Name: "of", Kind: kLink, Required: true},
		{Name: "source", Kind: kSourceMap, Required: true},
	},
	"policy_exception": {
		{Name: "policy", Kind: kLink, Required: true},
		{Name: "rule", Kind: kRuleLink},
		{Name: "kind", Kind: kEnum, Enum: exceptionKinds, Required: true},
		{Name: "status", Kind: kEnum, Enum: exceptionStatus, Required: true},
		{Name: "by", Kind: kString, Required: true},
		{Name: "with", Kind: kLink}, // required when kind is conflict_with
		{Name: "review_by", Kind: kDate},
	},
	"person": {
		{Name: "org", Kind: kString, Required: true},
	},
	"team": {
		{Name: "org", Kind: kString, Required: true},
	},
	"role": {
		{Name: "org", Kind: kString, Required: true},
	},
	"domain": {
		{Name: "owner", Kind: kLink, Required: true},
	},
	"convention": {
		{Name: "status", Kind: kEnum, Enum: conventionState, Required: true},
		{Name: "applies_in", Kind: kList, Required: true},
	},
	"journal": {
		{Name: "date", Kind: kDate, Required: true},
		{Name: "scope", Kind: kEnum, Enum: journalScopes, Required: true},
	},
	"inbox": {},
}

// FieldEnum returns the allowed values of an enum field of a type, or nil.
// model's enums are tested against it.
func FieldEnum(typ, name string) []string {
	for _, fd := range typeFields[typ] {
		if fd.Name == name {
			return fd.Enum
		}
	}
	return nil
}

// Fidelities lists the allowed source.fidelity values.
func Fidelities() []string { return fidelities }
