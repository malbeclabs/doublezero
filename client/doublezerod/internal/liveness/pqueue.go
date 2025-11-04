package liveness

type priorityQ struct{ a []*event }

func (q priorityQ) Len() int {
	return len(q.a)
}

func (q priorityQ) Less(i, j int) bool {
	if !q.a[i].when.Equal(q.a[j].when) {
		return q.a[i].when.Before(q.a[j].when)
	}
	return q.a[i].seq < q.a[j].seq
}

func (q priorityQ) Swap(i, j int) {
	q.a[i], q.a[j] = q.a[j], q.a[i]
}

func (q *priorityQ) Push(x any) {
	q.a = append(q.a, x.(*event))
}

func (q *priorityQ) Pop() any {
	n := len(q.a)
	x := q.a[n-1]
	q.a = q.a[:n-1]
	return x
}
