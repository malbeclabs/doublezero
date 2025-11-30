package gmon

type TargetSource interface {
	All() map[TargetID]Target
	Added() <-chan Target
	Removed() <-chan TargetID
}

// FanInTargetSource merges multiple TargetSource[T] into one TargetSource[T].
//
// - All() returns the union of all underlying sources' All() maps.
// - Added()/Removed() forward events from all sources into shared channels.
//
// TargetIDs must be globally unique across sources (e.g. "pub/<pk>" vs "dz/<pk>").
type FanInTargetSource struct {
	sources []TargetSource

	addedCh   chan Target
	removedCh chan TargetID
}

func NewFanInTargetSource(sources ...TargetSource) *FanInTargetSource {
	f := &FanInTargetSource{
		sources:   sources,
		addedCh:   make(chan Target, 256),
		removedCh: make(chan TargetID, 256),
	}

	// Fan-in events from each source.
	for _, src := range sources {
		src := src

		go func() {
			for t := range src.Added() {
				select {
				case f.addedCh <- t:
				default:
				}
			}
		}()

		go func() {
			for id := range src.Removed() {
				select {
				case f.removedCh <- id:
				default:
				}
			}
		}()
	}

	return f
}

// All recomputes the union of all underlying sources' All() maps.
func (f *FanInTargetSource) All() map[TargetID]Target {
	out := make(map[TargetID]Target)
	for _, src := range f.sources {
		for id, t := range src.All() {
			out[id] = t
		}
	}
	return out
}

func (f *FanInTargetSource) Added() <-chan Target     { return f.addedCh }
func (f *FanInTargetSource) Removed() <-chan TargetID { return f.removedCh }
