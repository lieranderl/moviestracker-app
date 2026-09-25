---
name: tdd
description: The rules of the red-green-refactor loop for AI coding agents. Enforces test-first development, vertical slicing, pre-agreed test seams, and boundary-only mocking.
---

# The /tdd Skill: Test-Driven Development Loop

This skill guides test-first feature development and bug fixing in the Moviestracker codebase.

## When to Reach for This Skill
- Explicit invocation via `/tdd` or when instructed to follow "red-green-refactor".
- Building any concrete behavior with defined inputs and observable outputs.
- Fixing a reported bug by first reproducing it with a failing test.

## Core Rules of the Loop

### 1. Pre-Agreed Seam (Mandatory First Step)
A **seam** is the public boundary where behavior is observed without reaching inside internals.
- **Rule**: Never write a test at an unconfirmed seam.
- Before creating or modifying any test file, pause and name the candidate seam(s):
  - *HTTP / Router Seam*: `httptest.NewServer` or `httptest.ResponseRecorder` exercising HTTP routing, headers, middleware, and SSE stream formatting.
  - *Domain Service Seam*: Exported interfaces/structs (e.g. `auth.SessionManager`), verifying state, thread-safety, and business logic without transport overhead.
- Wait for alignment or clearly state the trade-offs before proceeding.

### 2. Vertical Slicing
- **One seam, one test, one minimal implementation, repeat.**
- The first cycle should be a tracer bullet proving a single happy path end-to-end.
- **Anti-Pattern (Horizontal Slicing)**: Never write all tests upfront before writing implementation code. Bulk tests commit to imagined structures before discovering implementation realities.

### 3. Red -> Green Execution
1. **Red**: Write a single failing test asserting capability. Run the test suite and confirm it fails for the expected reason (not due to a compilation syntax typo).
2. **Green**: Write the bare minimum code required to pass that test. Do not anticipate the test after next.
3. **Refactor**: Once green, clean up code clarity, remove duplication, and run `make lint`.

### 4. Mock Boundaries
- **External Boundaries Only**: Mocks, stubs, and fakes belong exclusively at external system boundaries:
  - Time / System Clock
  - Cryptographic Entropy
  - Network APIs / External Services
  - Filesystem / Database
- **Never Mock Internal Modules**: Do not mock internal domain packages or sibling modules. Use real in-memory implementations.

### 5. Capability-Focused Tests
- Test names describe observable capabilities (`TestUserCanSignInWithValidCredentials`), not internal implementation details (`TestAuthHandlerCallsValidatePassword`).
- Assertions use explicit literals traceable to specifications, not values recomputed using duplicate production logic.
- Tests must survive structural refactoring of internal functions as long as public behavior is unchanged.
