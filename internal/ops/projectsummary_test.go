package ops

import (
	"encoding/json"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// list {type: project} gives each row the summary the bundle would: state
// line and age, goals in flight, waits, next cron (none while its goal is
// parked), last activity. Rows come stalest state first; status filters.
func TestProjectListSummary(t *testing.T) {
	tv, d := cronVault(t, "2026-11-01T09:00:00Z")
	garden := must(t, ProjectAdd, d, ProjectAddInput{Title: "Garden", StateLine: "Beds dug."})
	g1 := must(t, GoalAdd, d, GoalAddInput{Title: "Plant beds", DoneWhen: "Beds planted.", Project: garden.ID})
	g2 := must(t, GoalAdd, d, GoalAddInput{Title: "Pond", DoneWhen: "Pond holds water.", Project: garden.ID})
	t1 := must(t, TaskAdd, d, TaskAddInput{Goal: g1.ID, Title: "Get compost", DoneWhen: "Compost here.", Due: "2026-11-05"})
	must(t, TaskBlock, d, TaskBlockInput{ID: t1.ID, Reason: "supplier closed"})
	must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Is a permit needed", Project: garden.ID, Route: "domain-x", SelfServe: "none found"})
	addOut(t, d, `{"type":"obligation","title":"Water","fields":{"project":"`+garden.ID+`","recur":"weekly","due":"2026-11-06","makes":"task","goal":"`+g1.ID+`"}}`)
	addOut(t, d, `{"type":"obligation","title":"Top up pond","fields":{"project":"`+garden.ID+`","recur":"daily","due":"2026-11-02","makes":"task","goal":"`+g2.ID+`"}}`)
	invoke(t, Edit, d, `{"id":"`+g2.ID+`","fields":{"status":"parked","resume_when":"after the frost"}}`)

	tv.at("2026-11-04T09:00:00Z")
	shed := must(t, ProjectAdd, d, ProjectAddInput{Title: "Shed", StateLine: "Roof on."})
	blank := must(t, ProjectAdd, d, ProjectAddInput{Title: "Bikes"})
	retired := must(t, ProjectAdd, d, ProjectAddInput{Title: "Boat"})
	must(t, ProjectRetire, d, ProjectRetireInput{ID: retired.ID})

	ls := must(t, List, d, ListInput{Type: model.TypeProject})
	var order []string
	for _, r := range ls.Rows {
		order = append(order, r.ID)
	}
	if want := []string{blank.ID, garden.ID, shed.ID}; len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("order = %v, want %v", order, want)
	}
	row := ls.Rows[1].Project
	sh := must(t, ProjectShow, d, ProjectShowInput{ID: garden.ID})
	if row == nil || row.StateLine != sh.Project.StateLine || row.StateAgeDays != sh.StateAgeDays || row.StateAgeDays != 3 ||
		row.OpenGoals != len(sh.Goals) || row.WaitsOn != len(sh.WaitsOn) || row.WaitsOn != 2 || row.FirstWait != sh.WaitsOn[0].Title ||
		row.LastActivity != sh.LastActivity || row.NextCron != "2026-11-06" {
		t.Fatalf("row = %+v\nbundle goals %d waits %+v last %s crons %v", row, len(sh.Goals), sh.WaitsOn, sh.LastActivity, sh.Crons)
	}
	if b := ls.Rows[0].Project; b == nil || b.StateAgeDays != -1 || b.OpenGoals != 0 || b.NextCron != "" {
		t.Fatalf("blank row = %+v", b)
	}
	if l := must(t, List, d, ListInput{Type: model.TypeProject, Status: "retired"}); len(l.Rows) != 1 || l.Rows[0].ID != retired.ID || l.Rows[0].Project == nil {
		t.Fatalf("retired = %+v", l.Rows)
	}
	if l := must(t, List, d, ListInput{Type: model.TypeProject, Limit: 1}); len(l.Rows) != 1 || l.Rows[0].ID != blank.ID || l.Count != 3 {
		t.Fatalf("limit = %+v count %d", l.Rows, l.Count)
	}
	b, _ := json.MarshalIndent(ls.Rows[1], "", " ")
	t.Logf("row:\n%s", b)
}
