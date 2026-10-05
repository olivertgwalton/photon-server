## What and why

<!-- One to three sentences. -->

## How it was verified

## Checklist

- [ ] It needs to exist now: no code for a future need, no knob nobody turns, no interface with one implementation.
- [ ] It is not already here: the repo and the standard library were searched, and nothing now does the same job twice.
- [ ] The shape is idiomatic: concrete returns, consumer-side interfaces, ctx first, enums for modes.
- [ ] Each error is handled once: handled or returned, wrapped only where the words help.
- [ ] Every goroutine has an owner and a stop; tests pass under `-race`.
- [ ] A test fails without this change, and it tests behaviour a client or operator would notice, not the code restated.
- [ ] It is one concern and small; refactors are in their own PR.
- [ ] Comments only where something is surprising; every `//nolint` names its linter and says why.
- [ ] Any new dependency is justified in this description.
- [ ] Nothing here needs a later PR to undo or complete it.

## Out of scope
