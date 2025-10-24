# DoubleZero RFC Template

Use this template when proposing any substantive change to the DoubleZero project. Replace each guideline paragraph with the content for your proposal while preserving the section headings.

---

## Summary

*Brief, self‑contained overview of the proposal.*
Explain **what** the feature is and **why** it is worth adding in one or two short paragraphs. A reader should understand the essence of the idea and its expected benefit without reading further details.

## Motivation

*Why we need this change now.*
Describe the problem or limitation that exists today, who is affected, and any data or examples that illustrate the pain point. Clarify how the proposed feature advances project goals (performance, usability, security, ecosystem growth, etc.).

## New Terminology

*Glossary of any new or overloaded terms.*
List and define new words, acronyms, or protocol messages introduced by this RFC. Keep each definition concise and unambiguous so reviewers share a common vocabulary.

## Alternatives Considered

*Other ways the problem might be solved.*
Outline the main competing approaches (including “do nothing”) and briefly state their advantages and disadvantages. This demonstrates due diligence and helps reviewers weigh trade‑offs.

## Detailed Design

*Exact technical specification.*
Provide enough detail for someone to implement the feature:

* Architecture overview (diagrams encouraged but optional)
* Data structures, schemas, or message formats
* Algorithms, control flow, or state machines
* API or CLI changes (with example calls)
* Configuration options, defaults, and migration steps
  Use subsections as needed; aim for clarity over brevity.

## Impact

*Consequences of adopting this RFC.*
Discuss effects on:

* Existing codebase (modules touched, refactors required)
* Operational complexity (deployment, monitoring, costs)
* Performance (throughput, latency, resource usage)
* User experience or documentation
  Quantify impacts where possible; note any expected ROI.

## Security Considerations

*Threat analysis and mitigations.*
Identify new attack surfaces, trust boundaries, or privacy issues introduced by the change. Describe how each risk is prevented, detected, or accepted and reference relevant best practices.

## Backward Compatibility

*Interaction with existing deployments.*
Explain whether current nodes, data, or integrations continue to work unchanged. If not, spell out migration paths, feature gates, version negotiation, or deprecation timelines.

## Open Questions

*Items that still need resolution.*
List outstanding issues, research tasks, or decisions deferred to later milestones. This section helps reviewers focus feedback and signals areas where contributions are welcomed.

---

*End of template. Delete all instructional text (italicized sentences and bullet guidance) when submitting your RFC.*
