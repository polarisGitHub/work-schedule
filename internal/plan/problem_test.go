package plan

import "testing"

func TestSelectionScopeMatchCell(t *testing.T) {
	teacher := TeacherID(7)
	day := "2026-10-06"
	shift := int64(1)

	cases := []struct {
		name  string
		scope SelectionScope
		tes   TeacherID
		cell  Cell
		want  bool
	}{
		{"空 scope 全匹配", SelectionScope{}, 7, Cell{Day: day, ShiftID: 1, ClassID: 3}, true},
		{"teacher 命中", SelectionScope{Teacher: &teacher}, 7, Cell{Day: day}, true},
		{"teacher 未命中", SelectionScope{Teacher: &teacher}, 8, Cell{Day: day}, false},
		{"day 命中", SelectionScope{Day: &day}, 7, Cell{Day: day}, true},
		{"day 未命中", SelectionScope{Day: &day}, 7, Cell{Day: "2026-10-07"}, false},
		{"shift 命中", SelectionScope{ShiftID: &shift}, 7, Cell{ShiftID: 1}, true},
		{"shift 未命中", SelectionScope{ShiftID: &shift}, 7, Cell{ShiftID: 2}, false},
		{"days 滑窗命中", SelectionScope{Days: []string{"2026-10-05", day}}, 7, Cell{Day: day}, true},
		{"days 滑窗未命中", SelectionScope{Days: []string{"2026-10-05"}}, 7, Cell{Day: day}, false},
	}
	for _, c := range cases {
		if got := c.scope.MatchCell(c.tes, c.cell); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestProblemUnitForCell(t *testing.T) {
	p := &Problem{Units: []Unit{
		{ID: 1, Key: "a", Cells: []Cell{{Day: "2026-10-06", ShiftID: 1, ClassID: 3}}},
		{ID: 2, Key: "b", Cells: []Cell{
			{Day: "2026-10-06", ShiftID: 2, ClassID: 3},
			{Day: "2026-10-06", ShiftID: 2, ClassID: 4},
		}},
	}}
	u, ok := p.UnitForCell(Cell{Day: "2026-10-06", ShiftID: 2, ClassID: 4})
	if !ok || u.ID != 2 {
		t.Fatalf("期望命中 unit 2，得到 ok=%v unit=%d", ok, u.ID)
	}
	if _, ok := p.UnitForCell(Cell{Day: "2026-10-06", ShiftID: 9, ClassID: 9}); ok {
		t.Fatal("不存在的格子不应命中")
	}
}
