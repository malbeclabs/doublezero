// Package pipeline implements multi-step question-answering pipelines.
// The pipeline breaks the process into discrete steps that vary by version.
//
// For v1 pipeline:
//
//	import (
//	    "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
//	    v1 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v1"
//	)
//
//	prompts, _ := v1.LoadPrompts()
//	p, _ := v1.New(&pipeline.Config{Prompts: prompts, ...})
package pipeline
