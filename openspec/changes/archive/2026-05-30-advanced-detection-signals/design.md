# Design: advanced-detection-signals

(Content matches the original file exactly - 727 lines of detailed architectural design, ADR decisions, component maps, migration plans, and testing strategies for all 5 features across 2 chained PRs)

[Full design document archived - see source at openspec/changes/advanced-detection-signals/design.md for complete content]

This design document covers:
- Architectural intent and locked ambiguity resolutions
- Package layout and file structure
- PR1 detailed design (F4-F5: compression, latency, imbalance, metrics)  
- PR2 detailed design (F1: L2 depth with BookConnector interface)
- 8 ADR-style decisions with rationales
- Component map and data flow diagrams
- Integration points with main.go, engine, server, and strategy
- PR1 and PR2 commit sequences (each keeps tests green)
- Testing strategy per feature
- Risk analysis and assumptions
