package main

import (
	"math"
)

const (
	dims     = 14
	leafSize = 32
)

func quantize(v float64) uint16 {
	if v < -0.5 {
		return 0
	}
	q := v*65534.0 + 1.5
	if q < 1 {
		return 1
	}
	if q > 65535 {
		return 65535
	}
	return uint16(q)
}

const dequantScale = float32(1.0 / 65534.0)

func sqDistQQ(a, b [dims]uint16) float32 {
	var sum float32
	for i := 0; i < dims; i++ {
		d := float32(int32(a[i]) - int32(b[i]))
		sum += d * d
	}
	return sum
}

type Point struct {
	Vec   [dims]uint16
	Fraud bool
}

type vpNode struct {
	lo    int32
	hi    int32
	split int32
	mu    float32
	left  int32
	right int32
}

type VPTree struct {
	points []Point
	idx    []int32
	nodes  []vpNode
	root   int32
}

type distIndex struct {
	dist float32
	idx  int32
}

func BuildVPTree(pts []Point) *VPTree {
	n := len(pts)
	if n == 0 {
		return &VPTree{root: -1}
	}

	idx := make([]int32, n)
	for i := range idx {
		idx[i] = int32(i)
	}

	t := &VPTree{
		points: pts,
		idx:    idx,
		nodes:  make([]vpNode, 0, n/leafSize*2+2),
		root:   -1,
	}

	tmp := make([]distIndex, n)
	t.root = t.buildNode(0, n, tmp)
	return t
}

func (t *VPTree) buildNode(lo, hi int, tmp []distIndex) int32 {
	if lo >= hi {
		return -1
	}

	nodeIdx := int32(len(t.nodes))
	t.nodes = append(t.nodes, vpNode{lo: int32(lo), hi: int32(hi), left: -1, right: -1})

	if hi-lo <= leafSize {
		return nodeIdx
	}

	vpOff := lo + (hi-lo)/2
	t.idx[lo], t.idx[vpOff] = t.idx[vpOff], t.idx[lo]
	vp := t.points[t.idx[lo]].Vec

	inner := hi - lo - 1
	for i := 0; i < inner; i++ {
		pointIdx := t.idx[lo+1+i]
		tmp[i] = distIndex{
			dist: sqDistQQ(vp, t.points[pointIdx].Vec),
			idx:  pointIdx,
		}
	}

	mid := inner / 2
	selectDistIndex(tmp[:inner], mid)
	t.nodes[nodeIdx].mu = float32(math.Sqrt(float64(tmp[mid].dist)))

	for i := 0; i < inner; i++ {
		t.idx[lo+1+i] = tmp[i].idx
	}

	split := lo + 1 + mid + 1
	t.nodes[nodeIdx].split = int32(split)

	t.nodes[nodeIdx].left = t.buildNode(lo+1, split, tmp)
	t.nodes[nodeIdx].right = t.buildNode(split, hi, tmp)

	return nodeIdx
}

func (t *VPTree) KNNFraudCount(query [dims]float32, k int) int {
	q := quantizeQuery(query)
	state := newKNNState(k)
	t.searchNode(t.root, q, &state)
	return state.fraudCount()
}

func (t *VPTree) searchNode(nodeIdx int32, query [dims]uint16, state *knnState) {
	if nodeIdx < 0 {
		return
	}
	node := t.nodes[nodeIdx]

	if node.left < 0 {
		for i := node.lo; i < node.hi; i++ {
			p := t.points[t.idx[i]]
			state.add(sqDistQQ(p.Vec, query), p.Fraud)
		}
		return
	}

	vp := t.points[t.idx[node.lo]]
	d2 := sqDistQQ(vp.Vec, query)
	d := float32(math.Sqrt(float64(d2)))
	state.add(d2, vp.Fraud)

	tau := state.tau()

	if d < node.mu {
		t.searchNode(node.left, query, state)
		tau = state.tau()
		if node.mu-d <= tau {
			t.searchNode(node.right, query, state)
		}
	} else {
		t.searchNode(node.right, query, state)
		tau = state.tau()
		if d-node.mu <= tau {
			t.searchNode(node.left, query, state)
		}
	}
}

type knnState struct {
	k     int
	len   int
	worst int
	dist  [5]float32
	fraud [5]bool
}

func newKNNState(k int) knnState {
	if k > 5 {
		k = 5
	}
	return knnState{k: k}
}

func (s *knnState) add(d float32, fraud bool) {
	if s.len < s.k {
		s.dist[s.len] = d
		s.fraud[s.len] = fraud
		if d > s.dist[s.worst] {
			s.worst = s.len
		}
		s.len++
		return
	}
	if d >= s.dist[s.worst] {
		return
	}
	s.dist[s.worst] = d
	s.fraud[s.worst] = fraud
	for i := 0; i < s.k; i++ {
		if s.dist[i] > s.dist[s.worst] {
			s.worst = i
		}
	}
}

func (s *knnState) tau() float32 {
	if s.len < s.k {
		return float32(math.MaxFloat32)
	}
	return float32(math.Sqrt(float64(s.dist[s.worst])))
}

func (s *knnState) fraudCount() int {
	count := 0
	for i := 0; i < s.len; i++ {
		if s.fraud[i] {
			count++
		}
	}
	return count
}

func selectDistIndex(a []distIndex, k int) {
	left, right := 0, len(a)-1
	for left < right {
		pivot := partitionDistIndex(a, left, right)
		if k == pivot {
			return
		}
		if k < pivot {
			right = pivot - 1
		} else {
			left = pivot + 1
		}
	}
}

func partitionDistIndex(a []distIndex, left, right int) int {
	mid := left + (right-left)/2
	pivotIdx := medianDistIndex(a, left, mid, right)
	a[pivotIdx], a[right] = a[right], a[pivotIdx]
	pivot := a[right].dist

	store := left
	for i := left; i < right; i++ {
		if a[i].dist < pivot {
			a[store], a[i] = a[i], a[store]
			store++
		}
	}
	a[store], a[right] = a[right], a[store]
	return store
}

func medianDistIndex(a []distIndex, i, j, k int) int {
	x, y, z := a[i].dist, a[j].dist, a[k].dist
	if x < y {
		if y < z {
			return j
		}
		if x < z {
			return k
		}
		return i
	}
	if x < z {
		return i
	}
	if y < z {
		return k
	}
	return j
}

func bruteKNNFraudCount(points []Point, query [dims]float32, k int) int {
	if k <= 0 {
		return 0
	}

	q := quantizeQuery(query)
	distances := [5]float32{}
	frauds := [5]bool{}
	if k > len(distances) {
		k = len(distances)
	}
	for i := 0; i < k && i < len(distances); i++ {
		distances[i] = float32(math.MaxFloat32)
	}

	found := 0
	worst := 0
	for _, p := range points {
		d := sqDistQQ(p.Vec, q)
		if found < k {
			distances[found] = d
			frauds[found] = p.Fraud
			if d > distances[worst] {
				worst = found
			}
			found++
			continue
		}
		if d >= distances[worst] {
			continue
		}
		distances[worst] = d
		frauds[worst] = p.Fraud
		for i := 0; i < k; i++ {
			if distances[i] > distances[worst] {
				worst = i
			}
		}
	}

	count := 0
	for i := 0; i < found; i++ {
		if frauds[i] {
			count++
		}
	}
	return count
}

func quantizeQuery(query [dims]float32) [dims]uint16 {
	var q [dims]uint16
	for i, v := range query {
		q[i] = quantize(float64(v))
	}
	return q
}
