# PonyTimer

An open-source poker tournament clock, built for multi-display,
server-authoritative operation.

> **Status: specification phase.** The v1 design is complete and written
> down in [docs/spec.md](docs/spec.md) (Japanese). Implementation has not
> started yet; work is tracked in GitHub Issues.

## What it will be

- **Server-authoritative timer** — the server holds the level end as an
  absolute timestamp (`level_ends_at`); display terminals render locally
  from it, so every screen in the room stays in sync. Delivery is
  SSE + REST with a 30-second snapshot poll as a safety net.
- **Browser displays** — one URL works on PCs, tablets, TV browsers and
  Fire TV Stick (Silk). Split view shows 1 / 2 / 4 timers on one screen.
- **Separate admin web UI** with account-based login; display screens use
  a read-only token URL.
- **Spreadsheet-style structure editor** with live validation and a
  pressure graph (average stack in BB per level).
- **Auto structure generation** from a master blind ladder (the WSOP
  mantissa set {1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8} × 10^n), following
  the rules R1–R13 in the spec — thinning/repeating ladder values instead
  of interpolating numbers. Tournaments are expected to end when the
  heads-up average stack reaches 10 BB (total chips / 20).
- **Mixed-game ready** — blind games (NL/PL and Limit, with BIG BET) and
  stud games (BRING-IN / COMPLETE / ANTE), with stud rounds toggleable
  per tournament.

## Tech stack (planned)

- Go, single binary (static assets embedded)
- SQLite
- No heavy frontend framework

## Lineage

PonyTimer is a project of **Ponyboy Music** — sibling project of
[PhysioBow](https://github.com/yppp/lc-test), a physical-modelling
bowed-string instrument.

The feature set is modeled on the SunVy Poker Club / SunVy Timer
(Smart Timer) clock. This is a clean-room functional imitation: no SunVy
names, images, copy, or other assets are used.

## License

MIT License. See [LICENSE](LICENSE).
