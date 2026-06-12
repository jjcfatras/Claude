### Code review

Inline comment posting failed. All issues listed below.

**pkg/scheduler/framework/runtime/batch.go:174**

🟡 **Medium** (Confidence: 60/100) - nested condition in batchStateCompatible adds branching that is hard to follow

<!-- cr-finding id="2fb28056889b" snippet64="aWYgIWIuZ2VuZXJpY1dvcmtsb2FkRW5hYmxlZCB8fCBjeWNsZUNvdW50ICE9IGIubGFzdEN5Y2xlLmN5Y2xlQ291bnQgewogICAgYi5sb2dVbnVzYWJsZVN0YXRlKGxvZ2dlciwgY3ljbGVDb3VudCwgbWV0cmljcy5CYXRjaEZsdXNoUG9kU2tpcHBlZCkKICAgIHJldHVybiBmYWxzZQp9" -->

**Issue & impact:** The new condition `if !b.genericWorkloadEnabled || cycleCount != b.lastCycle.cycleCount` collapses two distinct error paths into one expression. A cycleCount mismatch with GenericWorkload disabled and a cycleCount mismatch with GenericWorkload enabled now share the same return path, making future regressions harder to diagnose.

**Code:**

```go
if !b.genericWorkloadEnabled || cycleCount != b.lastCycle.cycleCount {
    b.logUnusableState(logger, cycleCount, metrics.BatchFlushPodSkipped)
    return false
}
```

**pkg/scheduler/framework/runtime/framework.go:358**

🟡 **Medium** (Confidence: 80/100) - feature-gate value captured at construction time without re-check

<!-- cr-finding id="8059bad464de" snippet64="Zi5iYXRjaCA9IG5ld09wcG9ydHVuaXN0aWNCYXRjaChmLCB1dGlsZmVhdHVyZS5EZWZhdWx0RmVhdHVyZUdhdGUuRW5hYmxlZChmZWF0dXJlcy5HZW5lcmljV29ya2xvYWQpKQ==" -->

**Issue & impact:** newOpportunisticBatch captures DefaultFeatureGate.Enabled(features.GenericWorkload) once and stores it on the struct. If the feature gate is mutated at runtime, batchStateCompatible will keep using the stale value, which can cause inconsistent scheduling decisions across pod cycles.

**Code:**

```go
f.batch = newOpportunisticBatch(f, utilfeature.DefaultFeatureGate.Enabled(features.GenericWorkload))
```

**pkg/scheduler/framework/runtime/batch.go:200**

🟡 **Medium** (Confidence: 70/100) - out-of-diff defect referenced from the diff context

<!-- cr-finding id="0eaf91fa1f9c" snippet64="ZnVuYyAoYiAqT3Bwb3J0dW5pc3RpY0JhdGNoKSBiYXRjaFN0YXRlQ29tcGF0aWJsZShjdHggY29udGV4dC5Db250ZXh0LCBwb2QgKnYxLlBvZCkgew==" -->

**Issue & impact:** The function batchStateCompatible (line 200, not in this diff) has no test that exercises the new GenericWorkload code path with a NIL lastCycle. Coverage gap should be filled in this PR.

**Code:**

```go
func (b *OpportunisticBatch) batchStateCompatible(ctx context.Context, pod *v1.Pod) {
```

_Note: Inline comments failed ({API_ERROR})._

🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>
