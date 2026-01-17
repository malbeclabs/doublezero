// Package workflow implements multi-step question-answering workflows.
// The workflow breaks the process into discrete steps that vary by version.
//
// For v1 workflow:
//
//	import (
//	    "github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
//	    v1 "github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/v1"
//	)
//
//	prompts, _ := v1.LoadPrompts()
//	p, _ := v1.New(&workflow.Config{Prompts: prompts, ...})
package workflow
