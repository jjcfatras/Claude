### Code review

Inline comment posting failed. All issues listed below.

**tests/migrations/base.py:12**

🟡 **Medium** (Confidence: 70/100) - renamed test helper writes into a caller-supplied path without validation

<!-- cr-finding id="312516a6f3b3" snippet64="ZGVmIGFzc2VydFRhYmxlRXhpc3RzKHNlbGYsIHRhYmxlLCB1c2luZz0iZGVmYXVsdCIpOg==" -->

**Issue & impact:** The helper module moved by this rename builds filesystem paths from arguments the calling test controls and never checks that the result stays inside the temp directory. A test passing an absolute or dot-dot segment would escape it. Because the rename is content-identical, none of the module body appears in this diff.

**Code:**

```python
def assertTableExists(self, table, using="default"):
```

**tests/migrations/base.py:40**

🟡 **Medium** (Confidence: 80/100) - three unrelated base classes share one module after the rename

<!-- cr-finding id="4b2dff3f161a" snippet64="Y2xhc3MgT3BlcmF0aW9uVGVzdEJhc2UoTWlncmF0aW9uVGVzdEJhc2UpOg==" -->

**Issue & impact:** MigrationTestBase, OperationTestBase and OptimizerTestBase all live side by side in the module this PR renames, and the eight importers each pull a different subset. Splitting them would let each test file depend only on what it uses. Nothing about the bodies changed here, so the range is unanchored in the diff.

**Code:**

```python
class OperationTestBase(MigrationTestBase):
```

**tests/migrations/test_commands.py:120**

🟡 **Medium** (Confidence: 65/100) - shutil.which("black") runs at import time on every collection

<!-- cr-finding id="61aa8b8585e8" snippet64="SEFTX0JMQUNLID0gc2h1dGlsLndoaWNoKCJibGFjayIp" -->

**Issue & impact:** HAS_BLACK is evaluated while the module is imported, so a PATH scan happens during collection even for runs that never touch the formatter tests. Caching it behind functools.cache would defer the cost. The anchor sits well past the import block this PR edited.

**Code:**

```python
HAS_BLACK = shutil.which("black")
```

**tests/postgres_tests/test_operations.py:80**

📝 **Minor** (Confidence: 85/100) - cross-package absolute import where every sibling uses a relative one

<!-- cr-finding id="348ab9ea2e93" snippet64="ZnJvbSBtaWdyYXRpb25zLmJhc2UgaW1wb3J0IE9wZXJhdGlvblRlc3RCYXNlLCBPcHRpbWl6ZXJUZXN0QmFzZQ==" -->

**Issue & impact:** This file reaches into the sibling package with an absolute `migrations.base` import while the other seven updated call sites use the relative form. The inconsistency makes the shared helper look like two different modules when grepping.

**Code:**

```python
from migrations.base import OperationTestBase, OptimizerTestBase
```

**Suggested fix:**

```python
from django.test import modify_settings

from migrations.base import OperationTestBase, OptimizerTestBase  # noqa: E402
```

_Note: Inline comments failed ({API_ERROR})._

🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>

<sub>Diff-scoped automated review — does not run tests or builds, verify test correctness, or examine code outside this PR's diff. Pair with CI and human review.</sub>
