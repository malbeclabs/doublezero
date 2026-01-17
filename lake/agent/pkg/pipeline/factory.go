package pipeline

import "os"

// Note: Due to import cycle constraints, callers should import version-specific
// packages directly and create pipelines accordingly.
//
// Example usage:
//
//	import (
//	    "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
//	    v1 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v1"
//	    v2 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v2"
//	    v3 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v3"
//	)
//
//	version := pipeline.DefaultVersion()
//	var runner pipeline.Runner
//	var err error
//
//	switch version {
//	case pipeline.VersionV1:
//	    prompts, _ := v1.LoadPrompts()
//	    cfg.Prompts = prompts
//	    runner, err = v1.New(cfg)
//	case pipeline.VersionV2:
//	    prompts, _ := v2.LoadPrompts()
//	    cfg.Prompts = prompts
//	    runner, err = v2.New(cfg)
//	case pipeline.VersionV3:
//	    prompts, _ := v3.LoadPrompts()
//	    cfg.Prompts = prompts
//	    runner, err = v3.New(cfg)
//	}

// DefaultVersion returns the default pipeline version.
// This can be overridden by the PIPELINE_VERSION environment variable.
func DefaultVersion() Version {
	if v := os.Getenv("PIPELINE_VERSION"); v != "" {
		switch Version(v) {
		case VersionV1:
			return VersionV1
		case VersionV2:
			return VersionV2
		case VersionV3:
			return VersionV3
		}
	}
	// v3 is the default
	return VersionV3
}
