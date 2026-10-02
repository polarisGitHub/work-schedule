package solver

import (
	"context"
	"sort"

	"work-schedule/internal/plan"
)

// GreedySwap 贪心 + 局部交换。
//
// 先按候选数升序（MRV，最受限制的先排）逐个单元挑增量代价最小且可行的老师；
// 再对没排上的单元做一轮局部交换：把占住它候选老师的那个单元挪到别的候选上。
type GreedySwap struct{}

func (GreedySwap) Name() string        { return "greedy-swap" }
func (GreedySwap) Label() string       { return "贪心 + 局部交换" }
func (GreedySwap) Deterministic() bool { return true }

func (GreedySwap) Capability() Capability {
	return Capability{
		Constraints: []string{"CountBound", "PairFix", "Cover", "ExclusiveSlot"},
		Costs:       []string{"Balance", "Spread", "Prefer"},
		Optimal:     false,
	}
}

func init() { Register(GreedySwap{}) }

// slotKey 老师 × 天 × 班次，用于 ExclusiveSlot。
type slotKey struct {
	teacher plan.TeacherID
	day     string
	shift   int64
}

// boundState 一条 CountBound 及其当前计数。
type boundState struct {
	bound plan.CountBound
	n     int
}

type solveState struct {
	p        *plan.Problem
	unitByID map[plan.UnitID]plan.Unit

	candidates map[plan.UnitID][]plan.TeacherID
	banned     map[plan.UnitID]map[plan.TeacherID]bool
	fixed      map[plan.UnitID]plan.TeacherID

	assigned  map[plan.UnitID]plan.TeacherID
	teacherN  map[plan.TeacherID]int
	dayN      map[plan.TeacherID]map[string]int
	occupancy map[slotKey]plan.UnitID
	bounds    []boundState
	exclusive bool

	reason map[plan.UnitID]string
}

func newSolveState(p *plan.Problem) *solveState {
	st := &solveState{
		p:          p,
		unitByID:   make(map[plan.UnitID]plan.Unit, len(p.Units)),
		candidates: make(map[plan.UnitID][]plan.TeacherID, len(p.Units)),
		banned:     make(map[plan.UnitID]map[plan.TeacherID]bool),
		fixed:      make(map[plan.UnitID]plan.TeacherID),
		assigned:   make(map[plan.UnitID]plan.TeacherID),
		teacherN:   make(map[plan.TeacherID]int),
		dayN:       make(map[plan.TeacherID]map[string]int),
		occupancy:  make(map[slotKey]plan.UnitID),
		reason:     make(map[plan.UnitID]string),
	}
	for _, un := range p.Units {
		st.unitByID[un.ID] = un
		// 候选排序，保证结果可复现（Problem.Candidates 是从 map 出来的，顺序不定）
		cs := append([]plan.TeacherID(nil), p.Candidates[un.ID]...)
		sort.Slice(cs, func(i, j int) bool { return cs[i] < cs[j] })
		st.candidates[un.ID] = cs
	}
	for _, c := range p.Hard {
		switch ct := c.(type) {
		case plan.PairFix:
			switch ct.Mode {
			case plan.ModeFix:
				st.fixed[ct.Unit] = ct.Teacher
			case plan.ModeBan:
				if st.banned[ct.Unit] == nil {
					st.banned[ct.Unit] = map[plan.TeacherID]bool{}
				}
				st.banned[ct.Unit][ct.Teacher] = true
			}
		case plan.CountBound:
			st.bounds = append(st.bounds, boundState{bound: ct})
		case plan.ExclusiveSlot:
			st.exclusive = true
		}
	}
	return st
}

// matchesBound 判断"老师 t 排到单元 u"是否计入这条计数约束。
func matchesBound(b plan.CountBound, t plan.TeacherID, u plan.Unit) bool {
	if b.Scope.Teacher != nil && *b.Scope.Teacher != t {
		return false
	}
	for _, cell := range u.Cells {
		if b.Scope.MatchCell(t, cell) {
			return true
		}
	}
	return false
}

func scopeMatchesUnit(s plan.SelectionScope, t plan.TeacherID, u plan.Unit) bool {
	if s.Teacher != nil && *s.Teacher != t {
		return false
	}
	for _, cell := range u.Cells {
		if s.MatchCell(t, cell) {
			return true
		}
	}
	return false
}

func containsTeacher(list []plan.TeacherID, t plan.TeacherID) bool {
	for _, v := range list {
		if v == t {
			return true
		}
	}
	return false
}

// feasible 增量判断把 t 排到 u 是否满足全部硬约束。
func (st *solveState) feasible(u plan.Unit, t plan.TeacherID) bool {
	if st.banned[u.ID][t] {
		return false
	}
	if st.exclusive {
		for _, cell := range u.Cells {
			if cur, ok := st.occupancy[slotKey{t, cell.Day, cell.ShiftID}]; ok && cur != u.ID {
				return false
			}
		}
	}
	for i := range st.bounds {
		b := &st.bounds[i]
		if b.bound.Max < 0 {
			continue
		}
		delta := 0
		if matchesBound(b.bound, t, u) {
			delta = 1
		}
		if b.n+delta > b.bound.Max {
			return false
		}
	}
	return true
}

// assign 落一条排定并更新计数。
func (st *solveState) assign(u plan.Unit, t plan.TeacherID) {
	st.assigned[u.ID] = t
	st.teacherN[t]++
	if st.dayN[t] == nil {
		st.dayN[t] = map[string]int{}
	}
	if st.exclusive {
		for _, cell := range u.Cells {
			st.occupancy[slotKey{t, cell.Day, cell.ShiftID}] = u.ID
		}
	}
	for i := range st.bounds {
		if matchesBound(st.bounds[i].bound, t, u) {
			st.bounds[i].n++
		}
	}
	for _, cell := range u.Cells {
		st.dayN[t][cell.Day]++
	}
}

// unassign 撤销一条排定。
func (st *solveState) unassign(u plan.Unit) {
	t, ok := st.assigned[u.ID]
	if !ok {
		return
	}
	delete(st.assigned, u.ID)
	st.teacherN[t]--
	if st.exclusive {
		for _, cell := range u.Cells {
			k := slotKey{t, cell.Day, cell.ShiftID}
			if cur, ok := st.occupancy[k]; ok && cur == u.ID {
				delete(st.occupancy, k)
			}
		}
	}
	for i := range st.bounds {
		if matchesBound(st.bounds[i].bound, t, u) {
			st.bounds[i].n--
		}
	}
	for _, cell := range u.Cells {
		st.dayN[t][cell.Day]--
	}
}

// cost 把 t 排到 u 的增量代价（越小越好）。
func (st *solveState) cost(u plan.Unit, t plan.TeacherID) float64 {
	var c float64
	for _, ct := range st.p.Costs {
		switch v := ct.(type) {
		case plan.Balance:
			if containsTeacher(v.Group, t) {
				c += v.Weight * float64(st.teacherN[t])
			}
		case plan.Spread:
			if scopeMatchesUnit(v.Scope, t, u) {
				c += v.Weight * float64(st.sameDayCount(t, u))
			}
		case plan.Prefer:
			if scopeMatchesUnit(v.Scope, t, u) {
				if v.Sign > 0 {
					c -= v.Weight
				} else {
					c += v.Weight
				}
			}
		}
	}
	return c
}

func (st *solveState) sameDayCount(t plan.TeacherID, u plan.Unit) int {
	n := 0
	for _, cell := range u.Cells {
		n += st.dayN[t][cell.Day]
	}
	return n
}

// tryAssignBest 给单元挑一个代价最小且可行的老师。avoid 非 0 时跳过该老师。
func (st *solveState) tryAssignBest(u plan.Unit, avoid plan.TeacherID) bool {
	if _, ok := st.assigned[u.ID]; ok {
		return true
	}
	best, bestCost, found := plan.TeacherID(0), 0.0, false
	for _, t := range st.candidates[u.ID] {
		if avoid != 0 && t == avoid {
			continue
		}
		if !st.feasible(u, t) {
			continue
		}
		c := st.cost(u, t)
		if !found || c < bestCost || (c == bestCost && t < best) {
			best, bestCost, found = t, c, true
		}
	}
	if !found {
		if st.reason[u.ID] == "" {
			if len(st.candidates[u.ID]) == 0 {
				st.reason[u.ID] = "没有候选老师"
			} else {
				st.reason[u.ID] = "候选老师都被同时段占用或超出上限"
			}
		}
		return false
	}
	st.assign(u, best)
	return true
}

// trySwapIn 尝试给未排上的单元腾位置：把它候选老师在同时段占用的那个单元挪到别的候选上。
func (st *solveState) trySwapIn(u plan.Unit) bool {
	for _, t := range st.candidates[u.ID] {
		if st.banned[u.ID][t] {
			continue
		}
		var blockers []plan.UnitID
		for _, cell := range u.Cells {
			if cur, ok := st.occupancy[slotKey{t, cell.Day, cell.ShiftID}]; ok && cur != u.ID {
				blockers = append(blockers, cur)
			}
		}
		for _, bid := range blockers {
			if _, isFixed := st.fixed[bid]; isFixed {
				continue // 锁定的不动
			}
			bu, ok := st.unitByID[bid]
			if !ok {
				continue
			}
			old := st.assigned[bid]
			st.unassign(bu)
			if st.tryAssignBest(bu, t) && st.feasible(u, t) {
				st.assign(u, t)
				return true
			}
			// 挪不动 / 挪了也没用 → 还原
			if _, still := st.assigned[bid]; still {
				st.unassign(bu)
			}
			st.assign(bu, old)
		}
	}
	return false
}

// repairMinBounds 补齐"至少 N 次"的计数约束（如指定值班）。补不上就留给检查器报。
func (st *solveState) repairMinBounds() {
	for i := range st.bounds {
		b := &st.bounds[i]
		if b.bound.Min < 0 || b.bound.Scope.Teacher == nil {
			continue
		}
		t := *b.bound.Scope.Teacher
		for b.n < b.bound.Min {
			if !st.forceAssignForBound(b, t) {
				break
			}
		}
	}
}

func (st *solveState) forceAssignForBound(b *boundState, t plan.TeacherID) bool {
	for _, u := range st.p.Units {
		if _, ok := st.assigned[u.ID]; ok {
			continue
		}
		if !matchesBound(b.bound, t, u) {
			continue
		}
		if !containsTeacher(st.candidates[u.ID], t) {
			continue
		}
		if !st.feasible(u, t) {
			continue
		}
		st.assign(u, t)
		return true
	}
	return false
}

// Solve 实现 Solver。
func (g GreedySwap) Solve(ctx context.Context, p *plan.Problem) (*plan.Result, error) {
	st := newSolveState(p)

	// 1) 固定对（人工锁定 / 指定 Fix）先落。落不进去也照留，交给检查器报。
	for _, un := range p.Units {
		if t, ok := st.fixed[un.ID]; ok {
			st.assign(un, t)
		}
	}

	// 2) MRV 顺序：候选少的先排；同序时按 Key，保证确定性。
	order := append([]plan.Unit(nil), p.Units...)
	sort.SliceStable(order, func(i, j int) bool {
		li, lj := len(st.candidates[order[i].ID]), len(st.candidates[order[j].ID])
		if li != lj {
			return li < lj
		}
		return order[i].Key < order[j].Key
	})

	for idx, un := range order {
		if idx%64 == 0 && ctx.Err() != nil {
			st.reason[un.ID] = "求解超时，未处理"
			continue
		}
		st.tryAssignBest(un, 0)
	}

	// 3) 局部交换：救回没排上的单元
	for _, un := range order {
		if _, ok := st.assigned[un.ID]; ok {
			continue
		}
		st.trySwapIn(un)
	}

	// 4) 补齐 Min 类计数约束
	st.repairMinBounds()

	// 5) 组织结果
	res := &plan.Result{Optimal: false}
	for _, un := range p.Units {
		t, ok := st.assigned[un.ID]
		if !ok {
			res.Unassigned = append(res.Unassigned, plan.Unassigned{Unit: un.ID, Reason: st.reason[un.ID]})
			continue
		}
		_, locked := st.fixed[un.ID]
		res.Assignments = append(res.Assignments, plan.Assignment{Unit: un.ID, Teacher: t, Locked: locked})
	}
	res.Score = st.totalCost()
	return res, nil
}

// totalCost 按 CostTerm 聚合目标值，便于横向比较不同算法。
// 求和顺序保持确定（Group 是切片、单元按原顺序、老师 key 先排序），避免浮点顺序差。
func (st *solveState) totalCost() float64 {
	var c float64
	for _, ct := range st.p.Costs {
		switch v := ct.(type) {
		case plan.Balance:
			min, max := -1, -1
			for _, t := range v.Group {
				n := st.teacherN[t]
				if min == -1 || n < min {
					min = n
				}
				if n > max {
					max = n
				}
			}
			if min >= 0 {
				c += v.Weight * float64(max-min)
			}
		case plan.Spread:
			for _, t := range sortedTeacherKeys(st.dayN) {
				if v.Scope.Teacher != nil && *v.Scope.Teacher != t {
					continue
				}
				mx := 0
				for _, n := range st.dayN[t] {
					if n > mx {
						mx = n
					}
				}
				if mx > 1 {
					c += v.Weight * float64(mx-1)
				}
			}
		case plan.Prefer:
			for _, un := range st.p.Units {
				t, ok := st.assigned[un.ID]
				if !ok {
					continue
				}
				if scopeMatchesUnit(v.Scope, t, un) {
					if v.Sign > 0 {
						c -= v.Weight
					} else {
						c += v.Weight
					}
				}
			}
		}
	}
	return c
}

func sortedTeacherKeys(m map[plan.TeacherID]map[string]int) []plan.TeacherID {
	out := make([]plan.TeacherID, 0, len(m))
	for t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
