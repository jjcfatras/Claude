### Code review

Inline comment posting failed. All issues listed below.

**activejob/lib/active_job/queue_adapters.rb:130**

📝 **Minor** (Confidence: 90/100) - the lookup example in the doc comment now advertises the async adapter as the canonical choice

<!-- cr-finding id="d06815a2218b" snippet64="ICAgICAgIyAgICMgPT4gQWN0aXZlSm9iOjpRdWV1ZUFkYXB0ZXJzOjpBc3luY0FkYXB0ZXI=" -->

**Issue & impact:** Two other tables in this same file were edited to drop a row, but the prose above them still counts the adapters inline, so the numbers no longer agree with the rows beneath them.

_Note: This issue was flagged in a prior review but the code has since changed._

**Code:**

```ruby
      #   # => ActiveJob::QueueAdapters::AsyncAdapter
```

**activejob/test/cases/test_case_test.rb:41**

🟡 **Medium** (Confidence: 80/100) - case/when ladder duplicates the adapter registry it should be reading

<!-- cr-finding id="e8d700c3110c" snippet64="ICAgICAgICAgICAgICAgd2hlbiA6c25lYWtlcnM=" -->

**Issue & impact:** Each removal from the registry forces a matching edit in this ladder, which is the third place the same mapping is written out. Deriving the constant from the symbol would make the ladder unnecessary.

**Code:**

```ruby
               when :sneakers
```

**activejob/test/integration/queuing_test.rb:34**

🟡 **Medium** (Confidence: 70/100) - delayed_job block inherits a jobs_manager teardown that no longer runs

<!-- cr-finding id="9ea2a513f12e" snippet64="ICBpZiBhZGFwdGVyX2lzPyg6ZGVsYXllZF9qb2Ip" -->

**Issue & impact:** Three tests that called jobs_manager.stop_workers were deleted just above, and that manager was what restored worker state between cases. The block starting here reuses the same manager without an equivalent reset.

**Code:**

```ruby
  if adapter_is?(:delayed_job)
```

_Note: Inline comments failed ({API_ERROR})._

🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>

<sub>Diff-scoped automated review — does not run tests or builds, verify test correctness, or examine code outside this PR's diff. Pair with CI and human review.</sub>
