package pipeline

// This file is intentionally minimal.
// Version-specific pipelines should be imported directly:
//
//   import v1 "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline/v1"
//
// Usage:
//   prompts, _ := v1.LoadPrompts()
//   p, _ := v1.New(&pipeline.Config{Prompts: prompts, ...})
//   result, _ := p.Run(ctx, question)
