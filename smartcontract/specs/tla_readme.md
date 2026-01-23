# TLA+ Formal Verification for Telemetry Program

## What is this?

This directory contains TLA+ specifications for formally verifying the state machines and protocols in our smart contracts. We're starting with the simplest thing: the telemetry submission and validation flow.

## Why TLA+?

TLA+ lets us model our system as a state machine and automatically check that certain properties always hold - **before** we deploy to mainnet.

Think of it as: "What if we could test every possible ordering of operations?" That's what TLA+ does.

## The Telemetry Spec

The `Telemetry.tla` spec models:

```
[none] --submit--> [pending] --validate--> [validated]
```

And checks invariants like:
- ✅ Once validated, can't go back to pending
- ✅ Validated telemetry must have a validator recorded  
- ✅ Can't validate something that was never submitted
- ✅ Only one validator per record

## Getting Started

### 1. Install TLA+ Toolbox

Download from: https://lamport.azurewebsites.net/tla/toolbox.html

(It's a Java app, works on Mac/Linux/Windows)

### 2. Open the spec

1. Launch TLA+ Toolbox
2. File → Open Spec → Add New Spec
3. Point it to `Telemetry.tla`

### 3. Create a model

1. Click "New Model" (or TLC Model Checker → New Model)
2. Set constants:
   - `TelemetryIds = {t1, t2}`
   - `Validators = {v1, v2}`
3. Add invariants to check:
   - `TypeOK`
   - `ValidatedHasValidator`
   - `SingleValidator`
4. Click "Run TLC"

TLA+ will explore **all possible states** your system can reach with 2 telemetry IDs and 2 validators.

### 4. What to look for

- **Green checkmark**: All invariants hold! 🎉
- **Red X**: TLA+ found a violation and will show you the exact sequence of steps that broke the invariant

## Example: Finding a Bug

Let's say we forgot to check if telemetry is already validated. TLA+ would show:

```
Error: Invariant SingleValidator is violated
Trace:
1. Init
2. SubmitTelemetry(t1)
3. ValidateTelemetry(t1, v1)
4. ValidateTelemetry(t1, v2)  <-- Bug! Can validate twice
```

Now you know exactly what to fix in your Solana program.

## Next Steps

Once the team is comfortable with this basic spec, we can model:

1. **Multiple validators** - require N of M signatures
2. **Timeouts** - telemetry expires if not validated within X blocks
3. **Concurrent submissions** - what happens with ordering?
4. **Economic properties** - token balances, rewards distribution

## Tips for the Team

- **Start small**: Model one flow at a time
- **Focus on the scary stuff**: What keeps you up at night? Model that.
- **Invariants > Implementation**: Focus on *what* should be true, not *how* the code works
- **Use TLA+ like tests**: Run it before every PR that touches state transitions

## Resources

- [TLA+ Video Course](https://lamport.azurewebsites.net/video/videos.html) - Leslie Lamport's intro
- [Learn TLA+](https://learntla.com/) - Excellent practical guide
- [TLA+ Examples](https://github.com/tlaplus/Examples) - Real-world specs

## Questions?

Ask in #engineering - several of us have used TLA+ and can help!

---

**Pro tip from the BEAM world**: If you've written GenServers with state machines, you already know how to think in TLA+. It's the same mental model, just more rigorous.