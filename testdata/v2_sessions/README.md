# V2 Session Golden Fixtures

These JSONL files preserve the event shapes observed in Claude Code and Codex
sessions. User text, paths, identifiers, model names, and timestamps are
synthetic so the fixtures can stay in source control.

The fixtures are intentionally small. Tests apply low Chunk limits to prove:

- complete-line cursor boundaries;
- deterministic hashes over uncompressed bytes;
- incomplete-tail handling;
- identical bytes at different cursor ranges remain valid.

Provider usage parsing expectations will be added to the same fixture set in
the Token Usage parser phase. Client-derived Token totals are not authoritative.
