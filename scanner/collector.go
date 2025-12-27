package scanner

type Collector interface {
	Collect(offset int)
}

type CollectorFunc func(offset int)

func (c CollectorFunc) Collect(offset int) {
	c(offset)
}

type SliceCollector struct {
	Results []int
}

func (c *SliceCollector) Collect(offset int) {
	c.Results = append(c.Results, offset)
}

func NewSliceCollector(initialCapacity int) *SliceCollector {
	return &SliceCollector{
		Results: make([]int, 0, initialCapacity),
	}
}
