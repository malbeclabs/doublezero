package dzmon

import (
	write "github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/solmon"
)

type InfluxSample struct {
	res DoubleZeroProbeResult

	extraTags map[string]string
}

func NewInfluxSample(res DoubleZeroProbeResult, extraTags map[string]string) InfluxSample {
	return InfluxSample{
		res:       res,
		extraTags: extraTags,
	}
}

func (p InfluxSample) Points() []*write.Point {
	pts := solmon.NewInfluxSample(p.res.ValidatorProbeResult, p.extraTags).Points()
	for _, pt := range pts {
		pt.AddTag("kind", "dz")
	}
	return pts
}
