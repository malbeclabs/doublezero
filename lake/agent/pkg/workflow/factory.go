package workflow

// Note: Due to import cycle constraints, callers should import the v3
// package directly and create workflows accordingly.
//
// Example usage:
//
//	import (
//	    "github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
//	    v3 "github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/v3"
//	)
//
//	prompts, _ := v3.LoadPrompts()
//	cfg.Prompts = prompts
//	runner, err := v3.New(cfg)
