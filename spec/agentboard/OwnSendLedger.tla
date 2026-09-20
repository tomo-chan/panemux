-------------------------- MODULE OwnSendLedger --------------------------
(***************************************************************************)
(* A TLA+ model of internal/board's `ownSendLedger` (ledger.go): the       *)
(* short-lived record of Send calls panemux itself issued, which the relay *)
(* consults before believing a polled-back row that claims From ==         *)
(* SystemID. Forging that identity is the attack this ledger exists to     *)
(* stop, so its Record / Consume / Forget / TTL-expiry state machine is    *)
(* security-critical and small enough to check exhaustively.               *)
(*                                                                         *)
(* SCOPE: one ledger key.  The Go ledger is a map from                     *)
(* (destHost, team, to, bodyHash) to a slice of expiry timestamps, and no  *)
(* operation ever reads or writes a key other than the one it was given.   *)
(* Keys are therefore independent and one key is the whole state machine.  *)
(* That independence is an assumption this spec does not itself prove —    *)
(* the Go-side conformance checker asserts it directly on every step (see  *)
(* internal/board/ledger_conformance_test.go).                             *)
(*                                                                         *)
(* ABSTRACTION: a key's slice is abstracted to two counts, `expired` and   *)
(* `live`.  This is faithful only because entries are appended in call     *)
(* order with expiry `now + ttl` and the clock is monotonic, so the slice  *)
(* is sorted ascending by expiry and the already-expired occurrences are   *)
(* always a prefix.  A test that moves the injected clock BACKWARDS breaks *)
(* that assumption; the conformance checker computes both counts from the  *)
(* real slice, so such a run shows up as a conformance failure rather than *)
(* as silent agreement.                                                     *)
(*                                                                         *)
(* `recorded`, `matched`, `forgotten` and `collected` are history counters.*)
(* They never gate a transition, so the transition table exported for      *)
(* Tier 1 can project them away (see scripts/model_check.sh).  They exist  *)
(* only to state Conservation, which is what a set-instead-of-multiset     *)
(* implementation violates -- the real bug PR #167's second review round   *)
(* found, and the reason this subsystem was picked to pilot issue #168.    *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS MaxEntries,   \* bound on occurrences held for the key at once
          MaxRecords    \* bound on total Record calls, to keep TLC finite

VARIABLES
  expired,    \* occurrences past their expiry that Consume has not yet dropped
  live,       \* occurrences still matchable
  recorded,   \* history: Record calls so far
  matched,    \* history: Consume calls that returned TRUE
  forgotten,  \* history: occurrences removed by Forget
  collected,  \* history: expired occurrences garbage-collected by Consume
  action,     \* name of the step that produced this state
  result      \* Consume's return value for that step, "-" when not a Consume

vars == <<expired, live, recorded, matched, forgotten, collected, action, result>>

Actions == {"Init", "Record", "Expire", "ConsumeHit", "ConsumeMiss",
            "ForgetLive", "ForgetExpired", "ForgetEmpty"}

Results == {"-", "TRUE", "FALSE"}

Held == expired + live

TypeOK ==
  /\ expired   \in 0..MaxEntries
  /\ live      \in 0..MaxEntries
  /\ Held <= MaxEntries
  /\ recorded  \in 0..MaxRecords
  /\ matched   \in 0..MaxRecords
  /\ forgotten \in 0..MaxRecords
  /\ collected \in 0..MaxRecords
  /\ action    \in Actions
  /\ result    \in Results

(***************************************************************************)
(* Every occurrence ever recorded is accounted for exactly once: still     *)
(* held (live or expired-but-not-yet-collected), consumed, forgotten, or   *)
(* garbage-collected.  A map keyed by ownSendKey holding ONE expiry per    *)
(* key -- the pre-#167 shape -- loses an occurrence on every duplicate     *)
(* Record and violates this.                                              *)
(***************************************************************************)
Conservation == recorded = matched + forgotten + collected + Held

(***************************************************************************)
(* The security bound: panemux can never believe more own-sends than it    *)
(* actually issued.                                                        *)
(***************************************************************************)
NoOverMatch == matched <= recorded

Init ==
  /\ expired   = 0
  /\ live      = 0
  /\ recorded  = 0
  /\ matched   = 0
  /\ forgotten = 0
  /\ collected = 0
  /\ action    = "Init"
  /\ result    = "-"

\* Record: one new matchable occurrence, appended (ledger.go's Record).
Record ==
  /\ Held < MaxEntries
  /\ recorded < MaxRecords
  /\ live'     = live + 1
  /\ recorded' = recorded + 1
  /\ expired'  = expired
  /\ UNCHANGED <<matched, forgotten, collected>>
  /\ action' = "Record"
  /\ result' = "-"

\* Expire: ambient clock advance past k of the live occurrences' expiries.
\* Not a ledger method -- time passing between two ledger calls.
Expire ==
  /\ live > 0
  /\ \E k \in 1..live :
       /\ live'    = live - k
       /\ expired' = expired + k
  /\ UNCHANGED <<recorded, matched, forgotten, collected>>
  /\ action' = "Expire"
  /\ result' = "-"

\* Consume with at least one live occurrence: removes exactly one of them,
\* garbage-collects every expired occurrence it walked past, returns TRUE.
ConsumeHit ==
  /\ live > 0
  /\ live'      = live - 1
  /\ expired'   = 0
  /\ collected' = collected + expired
  /\ matched'   = matched + 1
  /\ UNCHANGED <<recorded, forgotten>>
  /\ action' = "ConsumeHit"
  /\ result' = "TRUE"

\* Consume with nothing live: garbage-collects regardless, returns FALSE.
ConsumeMiss ==
  /\ live = 0
  /\ live'      = 0
  /\ expired'   = 0
  /\ collected' = collected + expired
  /\ UNCHANGED <<recorded, matched, forgotten>>
  /\ action' = "ConsumeMiss"
  /\ result' = "FALSE"

\* Forget removes the LAST occurrence unconditionally (a slice truncation).
\* With the slice sorted ascending by expiry, the last occurrence is a live
\* one whenever any live one exists -- hence three disjoint cases rather
\* than one.
ForgetLive ==
  /\ live > 0
  /\ live'      = live - 1
  /\ expired'   = expired
  /\ forgotten' = forgotten + 1
  /\ UNCHANGED <<recorded, matched, collected>>
  /\ action' = "ForgetLive"
  /\ result' = "-"

ForgetExpired ==
  /\ live = 0
  /\ expired > 0
  /\ expired'   = expired - 1
  /\ live'      = live
  /\ forgotten' = forgotten + 1
  /\ UNCHANGED <<recorded, matched, collected>>
  /\ action' = "ForgetExpired"
  /\ result' = "-"

ForgetEmpty ==
  /\ live = 0
  /\ expired = 0
  /\ UNCHANGED <<expired, live, recorded, matched, forgotten, collected>>
  /\ action' = "ForgetEmpty"
  /\ result' = "-"

Next ==
  \/ Record
  \/ Expire
  \/ ConsumeHit
  \/ ConsumeMiss
  \/ ForgetLive
  \/ ForgetExpired
  \/ ForgetEmpty

Spec == Init /\ [][Next]_vars

(***************************************************************************)
(* Consume never invents a match: it can only return TRUE out of a state   *)
(* that actually held a live occurrence.                                   *)
(***************************************************************************)
ConsumeNeverForges == [][ (action' = "ConsumeHit") => live > 0 ]_vars

(***************************************************************************)
(* The multiset property, stated over steps: a Consume that immediately    *)
(* follows a Record always matches.  This is the one a set-shaped ledger   *)
(* breaks -- two identical Records leave one entry, so the second Consume  *)
(* misses and a real, delivered message is dropped as "invalid from".      *)
(***************************************************************************)
RecordIsImmediatelyMatchable ==
  [][ (action = "Record" /\ action' \in {"ConsumeHit", "ConsumeMiss"})
        => action' = "ConsumeHit" ]_vars

(***************************************************************************)
(* Forget closes the forgery window straight away: a Forget that follows a *)
(* Record with nothing else in between leaves no live occurrence behind    *)
(* when that Record was the only one.                                      *)
(***************************************************************************)
ForgetUndoesTheLastRecord ==
  [][ (action = "Record" /\ recorded = 1 /\ action' = "ForgetLive")
        => live' = 0 ]_vars
===========================================================================
