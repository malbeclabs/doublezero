---------------------------- MODULE Untitled ----------------------------
(*
  Simple TLA+ specification for the telemetry program's basic state machine.

  This models:
  - Submitting telemetry data (creates pending record)
  - Validating telemetry (transitions pending -> validated)
  - Basic invariants around state transitions

  To run this:
  1. Install TLA+ Toolbox: https://lamport.azurewebsites.net/tla/toolbox.html
  2. Open this file in the toolbox
  3. Create a model with TelemetryIds = {t1, t2} and Validators = {v1, v2}
  4. Add the invariants as properties to check
  5. Run TLC model checker
*)

EXTENDS Integers, TLC

CONSTANTS
  TelemetryIds,    \* Set of telemetry record identifiers
  Validators       \* Set of authorized validators

VARIABLES
  telemetry_state, \* Maps telemetry_id -> state ("none", "pending", "validated")
  validated_by     \* Maps telemetry_id -> validator (who validated it)

vars == <<telemetry_state, validated_by>>

\* Type definitions
TelemetryStates == {"none", "pending", "validated"}

TypeOK ==
  /\ telemetry_state \in [TelemetryIds -> TelemetryStates]
  /\ validated_by \in [TelemetryIds -> Validators \cup {"none"}]

\* Initial state - nothing exists yet
Init ==
  /\ telemetry_state = [t \in TelemetryIds |-> "none"]
  /\ validated_by = [t \in TelemetryIds |-> "none"]

\* Submit new telemetry - anyone can submit
SubmitTelemetry(t) ==
  /\ telemetry_state[t] = "none"
  /\ telemetry_state' = [telemetry_state EXCEPT ![t] = "pending"]
  /\ UNCHANGED validated_by

\* Validate pending telemetry - only validators can validate
ValidateTelemetry(t, v) ==
  /\ v \in Validators
  /\ telemetry_state[t] = "pending"
  /\ telemetry_state' = [telemetry_state EXCEPT ![t] = "validated"]
  /\ validated_by' = [validated_by EXCEPT ![t] = v]

\* Next state relation
Next ==
  \/ \E t \in TelemetryIds: SubmitTelemetry(t)
  \/ \E t \in TelemetryIds, v \in Validators: ValidateTelemetry(t, v)

\* Spec
Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
\* INVARIANTS - Properties that should always be true

\* Telemetry can only move forward through states (none -> pending -> validated)
StateProgression ==
  \A t \in TelemetryIds:
    \/ telemetry_state[t] = "none"
    \/ telemetry_state[t] = "pending"
    \/ telemetry_state[t] = "validated"

\* Validated telemetry must have a validator recorded
ValidatedHasValidator ==
  \A t \in TelemetryIds:
    telemetry_state[t] = "validated" => validated_by[t] \in Validators

\* Can't validate something that was never submitted
MustSubmitFirst ==
  \A t \in TelemetryIds:
    telemetry_state[t] = "validated" =>
      \E s \in DOMAIN telemetry_state:
        s = t /\ telemetry_state[s] # "none"

\* Only one validator per telemetry record
SingleValidator ==
  \A t \in TelemetryIds:
    telemetry_state[t] = "validated" =>
      validated_by[t] # "none"

-----------------------------------------------------------------------------
\* PROPERTIES TO EXPLORE

\* Eventually, if telemetry is submitted, it can be validated
\* (Liveness property - checks that progress is possible)
EventuallyValidated ==
  \A t \in TelemetryIds:
    telemetry_state[t] = "pending" ~> telemetry_state[t] = "validated"

=============================================================================