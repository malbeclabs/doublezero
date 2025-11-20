# Change, Configuration, and Compatibility Management

## Summary

**Status: Active**

This RFC provides a framework to manage software configuration and compatibility through semantic versioning, feature flags, and well-formed and intentional commits. Standardization unlocks automation and should keep human involvement with deployment and verification to a minimum.

## Motivation

Too many of our actions, from deployment of services to issuing new releases to verifying deployments, are manual and require human interaction. We don't have clear guidelines on versioning or compatibility windows which can cause some consternation or, worse, a network or functional outage. This RFC seeks to provide a foundation and a path forward to remedy these issues.

## New Terminology

### References
* [Semantic Versioning](https://semver.org/): Semantic Versioning details the way in which code changes initiate a version change
* [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): A structured commit message convention that enables automated tooling for version bumping, changelog generation, and other release processes aligned with semantic versioning

## Alternatives Considered

An alternative approach that was considered but rejected is to keep features in their own branches and then merge them into main when the work is done. This is a conventional approach but not without its problems. The main problem is that code kept sequestered in its branch for an extended period of time can have unexpected results when merged in all at once. These surprises can be merge conflicts of varying complexity to code behaving in unexpected ways which can be difficult to untangle.

## Detailed Design

### Release Cadence

* Devnet releases happen daily built off the latest commit in main for each service
* Testnet releases happen weekly and each release is versioned according to Semantic Versioning 2.0.0
  * These should be manually triggered to start
* Mainnet releases happen on a TBD cadence and is versioned according to Semantic Versioning 2.0.0

### Versioning and Compatibility

DoubleZero follows Semantic Versioning 2.0.0 for versioning:
* patch releases (0.0.x) are backwards compatible bug fixes
* minor releases (0.x.0) are backwards compatible features or new functionality
* major releases (x.0.0) are API breaking changes or significant feature releases

The intent is to have every service have the same version as this reduces confusion, surprises, and simplifies the overall versioning scheme. This isn't a rigid rule and can shift as needed to optimize delivery over strict adherence to a rule.

Compatibility between software is defined through compatibility windows which are guaranteed for one subsequent minor release. As an example, an update from 2.9.x to 3.0.0 would keep compatibility with 2.9.x until 3.1.x is released. Minor releases and patch releases, by definition, should not break the API contract. Major releases shouldn't be a surprise; a public roadmap should give users an estimated, non-binding release schedule to help guide their upgrade path.

At the close of a compatibility window, when the code is no longer used, it is removed to keep the code base lean with actively used code only. This helps simplify the code as well as remove code that is unreachable and no longer used.

Devnet and testnet don't adhere to this upgrade path. Devnet releases happen daily and testnet releases happen weekly; both devnet and testnet should be stable, as releases are gated by integration tests but there's no guarantee. There are no notifications for devnet breaking changes but there can be some notification for a breaking testnet release or a network outage. Non-breaking changes are not be announced as they should be transparent.

Versioning is done manually currently by creating a tag in GitHub which then triggers a GitHub Action to create a release as a debian or redhat package. Manual versioning gives way to automated versioning through commit message structure as defined by the Conventional Commits specification.

Commit messages are of the format `<type>(service name): #<ticket number> <description>`. `<type>` maps to semver with the following keywords: `fix` which maps to `PATCH`, `feat` which maps to `MINOR` and an `!` following the `type` is a breaking change that maps to `MAJOR`. There are other acceptable `types` which can be found [here](https://www.conventionalcommits.org/en/v1.0.0/#summary) in entry 4.

*NOTE* Only commits that are merged into the main branch must follow this requirement. Since we squash and rebase before merging into main, only that commit must follow this format. Commits in a branch are similar to drafts of a document; only the merging commit matters.

Examples:
* PATCH: `fix(controller): #123 some description`
* MINOR: `feat(agent): #561 some description`
* MAJOR: `feat(doublezerod)!: #456 some description of a breaking change`

Reverts can be handled with the `revert` type but we should optimize for rolling forward rather than rolling backwards. There are, however, always exceptions to the rule. Since we don't control the appliances where the software is run, we need to have a playbook or some agreed upon process with network contributors to coordinate a rollback. It feels like that should be dealt with in a different RFC - perhaps under the `mainnet-beta network contributor builds` milestone.

### Feature Flags

An engineering principle of DoubleZero is that code is merged into main as quickly as possible so that it's exercised in a remote environment and validated. With breaking changes or incomplete features, this isn't generally possible or advisable.

Feature flags are a way to add code that is incomplete or incompatible, but keep it hidden from users and execution. Releases are the mechanism through which feature flags are disabled.

#### Application Feature Flags
Features in development require tests like any other code that is written and released. Most likely, unit tests or individual service tests  written until the feature is complete. At that point, end-to-end tests are written. In fact, end-to-end tests must be written and verified before a feature could be considered complete and then released.

Environment variables are the trigger for feature flagging for DoubleZero. Environment variables are supported in Rust and Go, as well as most other languages, so we can maintain consistent practices across the board. We can map the names of the environment variables to the expected release or a string that identifies the feature within a release to provide more granularity. Environment variables simplify testing for features gated behind a flag, as well. Environment variables should be clearly named starting with `DZ_` to make it clear these are environment variables for feature flagging.

### Deployment Verification

#### Automated Tests

Ensuring a functional and performant deployment is a critical component of automated deployment. The goal of this RFC is to provide a baseline solution that provides a reliable signal for the health of a deployment, is simple, and can be extended as needs dictate. We already have a growing set of end-to-end tests that can be utilized to validate deployment. With some minor changes, those can be run via GitHub Actions after a deploy is completed and then the output can be hooked into Grafana. Alerts can be derived from the E2E tests which notify if some threshold is crossed.

We should also have tests that are constantly running. We could use something like [Blackbox prober exporter](https://github.com/prometheus/blackbox_exporter) in conjunction with some simple app that manages coordination like tunnel and user lifecycle actions for the DoubleZero modes like IBRL and multicast. While we can derive the health of the network through its quotidian functionality, we do need a way to validate features that haven't been publicly exposed in an automated way.

#### Rollbacks

Rollbacks should be used only in extreme circumstances. The expectation is Mainnet deploys should be successful; after all, there are unit tests, E2E tests, and devnet and testnet environments for testing and verification. Once multihoming is added to the Anza client, even in the event of a bad deploy, it should be transparent to a user, at least in terms of packets flowing. That failover should provide time to rectify a bad deploy in the rare event that it happens.

If a rollback must be made, well-defined compatibility windows should reduce friction from a user's perspective. Because we don't own the hardware that runs the software, we need to coordinate with network contributors which could take some amount of time. Hopefully, the compatibility windows will ensure that even if we have a bad deploy, it should be safe to rollback to the previous version.

## Impact

This RFC should have a positive impact on DoubleZero through more frequent deploys, more predictable changes, and defined compatibility windows. This RFC encourages getting code into main as quickly as possible; feature flags enable incompatible or incomplete features to be merged and hidden until they're ready. This process should also encourage more frequent releases with daily devnet releases, weekly testnet releases, and TBR mainnet-beta releases.

## Security Considerations

Security posture should largely remain the same as there aren't any changes to how we interact or deploy code. There is potentially more unused code behind feature flags which could be a security vulnerability if the code isn't vetted. Feature flagged code must follow the same reviews and security audits so unused or unactivated code shouldn't meaningfully negatively impact our security posture.

## Backward Compatibility

There are no backwards compatibility requirements for this RFC. Future compatibility requirements are outlined in the `Compatibility` section.

## Open Questions
* How frequently do we deploy to mainnet-beta? (We don't need the answer for this rev of the RFC)
* Do we need a minimum timeframe after a major release before a subsequent minor release cuts off compatibility? IE, if 3.x.x is released, and 3.1.x is ready to be released, must it wait some defined window of time to allow users time to upgrade safely?
* Do we assign persons to monitor a deploy until we have confidence that our tests are sufficient? (rotating group, not a single person)
* What processes do we need to put in place with network contributors to make sure they upgrade within a reasonable amount of time? Do we slash rewards for lagging contributors? Jeff, Rahul have some experience with this at helium
* Do we need a minimum percentage of adoption in testnet before rolling out a new mainnet-beta release?
  * Do we provide higher rewards for early upgraders / testers?


