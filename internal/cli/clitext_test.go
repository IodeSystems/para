package cli

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// TestQuestionAddDecisionWeighing: an agent's decision needs a recommendation
// percent, a confidence, and its basis per option; the CLI takes them.
func TestQuestionAddDecisionWeighing(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC")
	agent := []string{"--actor", "claude", "--why", "The owner asked which bank to use."}
	_, _, err := e.run(append(agent, "question", "add", "Which bank?", "--area", area.ID, "--kind", "decision", "--option", "North", "--option", "South")...)
	if err == nil || !strings.Contains(err.Error(), "recommendation") {
		t.Fatalf("bare agent decision: err = %v", err)
	}
	var q ops.QuestionAddOutput
	e.json(&q, append(agent, "question", "add", "Which bank?", "--area", area.ID, "--kind", "decision", "--option", "North", "--option", "South",
		"--recommendation", "North=70", "--recommendation", "South=30",
		"--option-confidence", "North=60", "--option-basis", "North=Fees page of each bank",
		"--option-detail", `{"name":"South","confidence":50,"confidence_basis":"A call with the branch"}`)...)
	var sh ops.QuestionShowOutput
	e.json(&sh, "question", "show", q.ID)
	v := sh.Question
	if v.Recommended != "North" || len(v.OptionDetails) != 2 {
		t.Fatalf("question = %+v", v)
	}
	for _, od := range v.OptionDetails {
		switch od.Name {
		case "North":
			if od.Recommendation != 70 || od.Confidence != 60 || od.ConfidenceBasis != "Fees page of each bank" {
				t.Fatalf("North = %+v", od)
			}
		case "South":
			if od.Recommendation != 30 || od.Confidence != 50 {
				t.Fatalf("South = %+v", od)
			}
		}
	}
	if _, _, err := e.run("question", "add", "X?", "--area", area.ID, "--recommendation", "North"); err == nil || !strings.Contains(err.Error(), "<option>=<value>") {
		t.Fatalf("bad --recommendation: err = %v", err)
	}
}

// TestLaterAndLinkAddText: later says what it put off and until when; link
// add says what it added, not the whole list.
func TestLaterAndLinkAddText(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC")
	var a, b ops.TaskAddOutput
	e.json(&a, "task", "add", "Renew the lease", "--area", area.ID)
	e.json(&b, "task", "add", "Pay the deposit", "--area", area.ID)
	if out := e.ok("later", a.ID, "--until", "2999-01-01"); !strings.HasPrefix(out, "put off\t"+a.ID+"\tuntil 2999-01-01\nreceipt\t") {
		t.Fatalf("later until = %q", out)
	}
	if out := e.ok("later", b.ID); !strings.HasPrefix(out, "put off\t"+b.ID+"\tsomeday\n") {
		t.Fatalf("later someday = %q", out)
	}
	e.ok("area", "link", "add", area.ID, "Lease folder", "--kind", "docs", "--target", "https://docs.acme.example/lease")
	out := e.ok("area", "link", "add", area.ID, "Bank", "--kind", "other", "--target", "https://bank.acme.example")
	if out != "added\t"+area.ID+"\tBank\tother\thttps://bank.acme.example\n2 links\n" {
		t.Fatalf("link add = %q", out)
	}
	if out := e.ok("area", "link", "remove", area.ID, "Bank"); out != "removed\t"+area.ID+"\tBank\n1 links\n" {
		t.Fatalf("link remove = %q", out)
	}
}

// TestSearchSnippetCompletesCutLink: a link the snippet's edge cut reads as
// the whole link (its title), not as part of an id.
func TestSearchSnippetCompletesCutLink(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Home upkeep and repairs for the house on the hill", "--id", "area-home-upkeep-and-repairs-for-the-house-on-the-hill")
	var task ops.TaskAddOutput
	e.json(&task, "task", "add", "Order plumbing parts", "--area", area.ID, "--body",
		"alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma gizmo [[area-home-upkeep-and-repairs-for-the-house-on-the-hill]] tail more words here and there and everywhere")
	out := e.ok("search", "gizmo")
	if !strings.Contains(out, "gizmo Home upkeep and repairs for the house on the hill") || strings.Contains(out, "gizmo area-home") {
		t.Fatalf("search = %q", out)
	}
}
