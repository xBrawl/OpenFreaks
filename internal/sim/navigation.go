package sim

import (
	"container/heap"
	"math"
)

type navPoint struct{ x, y float64 }
type navNode struct{ index, cost, score int }
type navQueue []navNode

func (q navQueue) Len() int { return len(q) }
func (q navQueue) Less(i, j int) bool {
	if q[i].score == q[j].score {
		return q[i].index < q[j].index
	}
	return q[i].score < q[j].score
}
func (q navQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *navQueue) Push(v any)   { *q = append(*q, v.(navNode)) }
func (q *navQueue) Pop() any     { old := *q; v := old[len(old)-1]; *q = old[:len(old)-1]; return v }

func walkClear(g *TileGrid, a, b navPoint, radius int) bool {
	n := max(1, int(math.Ceil(Dist(a.x, a.y, b.x, b.y)/30)))
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		if !g.CanOccupy(a.x+(b.x-a.x)*t, a.y+(b.y-a.y)*t, radius) {
			return false
		}
	}
	return true
}

func shotClear(w *World, x, y, tx, ty float64, sk *Skill) bool {
	if sk.ProjIndirect || sk.ProjPassEnv || sk.ProjPierceEnv {
		return true
	}
	n := max(1, int(math.Ceil(Dist(x, y, tx, ty)/50)))
	for i := 0; i <= n; i++ {
		f := float64(i) / float64(n)
		a, b := w.Tiles.tileAt(x+(tx-x)*f, y+(ty-y)*f)
		if !w.Tiles.at(a, b) {
			return false
		}
		idx := b*w.Tiles.W + a
		if w.Tiles.BlocksProj[idx] && !w.Tiles.Destroyed[idx] {
			return false
		}
	}
	return true
}

func botNavigate(w *World, p *Player, goal navPoint, tick int) {
	b := p.Brain
	goal.x = clampF(goal.x, float64(p.CollisionRadius), w.Tiles.PixW-float64(p.CollisionRadius))
	goal.y = clampF(goal.y, float64(p.CollisionRadius), w.Tiles.PixH-float64(p.CollisionRadius))
	start := navPoint{p.X, p.Y}
	if walkClear(&w.Tiles, start, goal, p.CollisionRadius) {
		b.Path = nil
		p.ApplyWalk(int(goal.x), int(goal.y))
		return
	}
	if tick >= b.ReplanAt || Dist(goal.x, goal.y, b.Goal.x, b.Goal.y) > 450 || b.Stuck >= 3 {
		b.Path = findPath(&w.Tiles, start, goal, p.CollisionRadius)
		b.Goal = goal
		b.ReplanAt = tick + 16
		b.Stuck = 0
	}
	for len(b.Path) > 0 && Dist(p.X, p.Y, b.Path[0].x, b.Path[0].y) < 75 {
		b.Path = b.Path[1:]
	}
	for len(b.Path) > 1 && walkClear(&w.Tiles, start, b.Path[1], p.CollisionRadius) {
		b.Path = b.Path[1:]
	}
	if len(b.Path) > 0 {
		p.ApplyWalk(int(b.Path[0].x), int(b.Path[0].y))
	} else {
		p.HasMoveTarget = false
	}
}

func findPath(g *TileGrid, start, goal navPoint, radius int) []navPoint {
	sx, sy := g.tileAt(start.x, start.y)
	if !g.at(sx, sy) {
		return nil
	}
	root := sy*g.W + sx
	tx := min(g.W-1, max(0, int(goal.x/PixelsPerTile)))
	ty := min(g.H-1, max(0, int(goal.y/PixelsPerTile)))
	end := ty*g.W + tx
	n := g.W * g.H
	cost := make([]int, n)
	parent := make([]int, n)
	for i := range cost {
		cost[i] = math.MaxInt
		parent[i] = -1
	}
	center := func(i int) navPoint { return navPoint{float64(i%g.W*300 + 150), float64(i/g.W*300 + 150)} }
	h := func(i int) int { return abs(i%g.W-tx) + abs(i/g.W-ty) }
	q := &navQueue{{root, 0, h(root)}}
	heap.Init(q)
	cost[root] = 0
	best := root
	for q.Len() > 0 {
		u := heap.Pop(q).(navNode)
		if u.cost != cost[u.index] {
			continue
		}
		if h(u.index) < h(best) {
			best = u.index
		}
		if u.index == end {
			best = end
			break
		}
		x, y := u.index%g.W, u.index/g.W
		from := center(u.index)
		if u.index == root {
			from = start
		}
		for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
			nx, ny := x+d[0], y+d[1]
			if !g.at(nx, ny) {
				continue
			}
			v := ny*g.W + nx
			if cost[v] <= u.cost+1 || !walkClear(g, from, center(v), radius) {
				continue
			}
			cost[v] = u.cost + 1
			parent[v] = u.index
			heap.Push(q, navNode{v, cost[v], cost[v] + h(v)})
		}
	}
	if best == root {
		return nil
	}
	path := []navPoint{}
	for v := best; v != root && v >= 0; v = parent[v] {
		path = append(path, center(v))
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	if best == end && walkClear(g, center(end), goal, radius) {
		path = append(path, goal)
	}
	return path
}
