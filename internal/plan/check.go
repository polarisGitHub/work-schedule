package plan

import "fmt"

// Check 用 Problem 里的声明式数据复核一个结果，抛出冲突清单。
//
// 检查器不重写任何规则逻辑：候选集 / 硬约束 / 代价项都能对"某个具体结果"求值，
// 所以同一批数据既喂求解器、又能复核结果。纯函数，不碰数据库。
func Check(p *Problem, r *Result) []Violation {
	out := []Violation{}

	unitByID := make(map[UnitID]Unit, len(p.Units))
	for _, u := range p.Units {
		unitByID[u.ID] = u
	}
	assigned := make(map[UnitID]TeacherID, len(r.Assignments))
	for _, a := range r.Assignments {
		assigned[a.Unit] = a.Teacher
	}

	// 1) 覆盖 + 候选资格
	for _, u := range p.Units {
		t, ok := assigned[u.ID]
		if !ok {
			out = append(out, Violation{Level: LevelHard, Kind: "cover", Unit: Ptr(u.ID),
				Message: fmt.Sprintf("%s 没有排老师", u.Key)})
			continue
		}
		if len(p.Candidates[u.ID]) > 0 && !containsTeacher(p.Candidates[u.ID], t) {
			out = append(out, Violation{Level: LevelHard, Kind: "candidate", Unit: Ptr(u.ID), Teacher: Ptr(t),
				Message: fmt.Sprintf("%s 排了 %d，但它不在候选集里", u.Key, t)})
		}
	}

	// 2) 硬约束
	for _, c := range p.Hard {
		switch ct := c.(type) {
		case PairFix:
			t, has := assigned[ct.Unit]
			switch ct.Mode {
			case ModeFix:
				if !has || t != ct.Teacher {
					out = append(out, Violation{Level: LevelHard, Kind: "pair_fix", Unit: Ptr(ct.Unit), Teacher: Ptr(ct.Teacher),
						Message: fmt.Sprintf("单元 %d 必须排老师 %d（%s）", ct.Unit, ct.Teacher, ct.Reason)})
				}
			case ModeBan:
				if has && t == ct.Teacher {
					out = append(out, Violation{Level: LevelHard, Kind: "pair_fix", Unit: Ptr(ct.Unit), Teacher: Ptr(ct.Teacher),
						Message: fmt.Sprintf("单元 %d 禁止排老师 %d（%s）", ct.Unit, ct.Teacher, ct.Reason)})
				}
			}
		case CountBound:
			n := 0
			for _, a := range r.Assignments {
				u, ok := unitByID[a.Unit]
				if !ok {
					continue
				}
				for _, cell := range u.Cells {
					if ct.Scope.MatchCell(a.Teacher, cell) {
						n++
						break
					}
				}
			}
			if (ct.Min >= 0 && n < ct.Min) || (ct.Max >= 0 && n > ct.Max) {
				out = append(out, Violation{Level: LevelHard, Kind: "count_bound",
					Message: fmt.Sprintf("计数约束被违反：实际 %d，要求 [%d, %d]（%s）", n, ct.Min, ct.Max, ct.Reason)})
			}
		case Cover:
			for _, id := range ct.Units {
				if _, ok := assigned[id]; !ok {
					out = append(out, Violation{Level: LevelHard, Kind: "cover", Unit: Ptr(id),
						Message: fmt.Sprintf("单元 %d 未被覆盖（%s）", id, ct.Reason)})
				}
			}
		}
	}

	// 3) 软代价 → 偏差
	for _, c := range p.Costs {
		switch ct := c.(type) {
		case Balance:
			counts := make(map[TeacherID]int, len(ct.Group))
			for _, t := range ct.Group {
				counts[t] = 0
			}
			for _, a := range r.Assignments {
				if _, ok := counts[a.Teacher]; ok {
					counts[a.Teacher]++
				}
			}
			min, max := -1, -1
			for _, t := range ct.Group {
				n := counts[t]
				if min == -1 || n < min {
					min = n
				}
				if n > max {
					max = n
				}
			}
			if min >= 0 && max-min > 0 {
				out = append(out, Violation{Level: LevelSoft, Kind: "balance",
					Message: fmt.Sprintf("次数不均：最多 %d 次、最少 %d 次，偏差 %d", max, min, max-min),
					Delta:   float64(max - min)})
			}
		case Spread, Prefer:
			// v1 不判这两个原语的软偏差：它们留给求解器消费。
		}
	}

	return out
}

func containsTeacher(list []TeacherID, t TeacherID) bool {
	for _, v := range list {
		if v == t {
			return true
		}
	}
	return false
}
