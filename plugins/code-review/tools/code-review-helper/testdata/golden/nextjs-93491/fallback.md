### Code review

Inline comment posting failed. All issues listed below.

**packages/next/src/client/route-params.ts:49**

📝 **Minor** (Confidence: 80/100) - decode+encode round-trip on every URL segment

<!-- cr-finding id="53826bf61716" snippet64="Ly8gUGF0aG5hbWUgcGFydHMgY29tZSBmcm9tIFVSTC5wYXRobmFtZS5zcGxpdCgnLycp" -->

**Issue & impact:** _Note: This comment was placed on the nearest diff line; the issue actually occurs on line 48._

canonicalizeURLPart now runs on every catchall and dynamic segment for every navigation. The decode/encode round-trip is fast individually but adds up with deep catchalls; consider memoizing for repeat keys.

**Code:**

```typescript
// Pathname parts come from URL.pathname.split('/')
```

**packages/next/src/client/route-params.ts:55**

🔴 **Critical** (Confidence: 50/100) - decodeURIComponent throws but try/catch returns the encoded form silently

<!-- cr-finding id="67c29b9b9337" snippet64="dHJ5IHsgcmV0dXJuIGVuY29kZVVSSUNvbXBvbmVudChkZWNvZGVVUklDb21wb25lbnQocGFydCkpIH0gY2F0Y2ggeyByZXR1cm4gcGFydCB9" -->

**Issue & impact:** When decodeURIComponent fails on a malformed sequence, the catch returns the original part. This is a silent fallback that hides the malformation from any caller — a more defensive option would be to throw an InvalidPathError or at least log a warning so observability picks up bad input.

**Code:**

```typescript
try { return encodeURIComponent(decodeURIComponent(part)) } catch { return part }
```

**packages/next/src/client/route-params.ts:121**

🟡 **Medium** (Confidence: 78/100) - decoded user-controlled URL parts re-encoded without explicit allow-list

<!-- cr-finding id="5fa27f198dc9" snippet64="cmV0dXJuIGNhbm9uaWNhbGl6ZVVSTFBhcnQocGF0aG5hbWVQYXJ0c1twYXJ0SW5kZXhdKQ==" -->

**Issue & impact:** canonicalizeURLPart blindly decodeURIComponent's the input then re-encodes. If a malicious pathname part contains a UTF-8 sequence that survives decode but produces unexpected characters after re-encoding, the resulting segment could mismatch server-side routing in ways that bypass middleware checks.

_Note: This issue was flagged in a prior review but the code has since changed._

**Code:**

```typescript
return canonicalizeURLPart(pathnameParts[partIndex])
```

**test/e2e/app-dir/segment-cache/encoded-slash-params/components/link-accordion.tsx:18**

🟡 **Medium** (Confidence: 65/100) - checked toggle without controlled-onChange parity loses NormalizedPathname brand

<!-- cr-finding id="d6f3202365eb" snippet64="PGlucHV0IHR5cGU9ImNoZWNrYm94IiBjaGVja2VkPXtpc1Zpc2libGV9IG9uQ2hhbmdlPXsoKSA9PiBzZXRJc1Zpc2libGUoIWlzVmlzaWJsZSl9IC8+" -->

**Issue & impact:** The function signature of canonicalizeURLPart returns a plain string which loses the NormalizedPathname brand applied earlier in the file. Downstream callers that consume the result must re-cast or risk a silent type-narrowing escape — the same concern surfaces in this LinkAccordion's controlled state where href is passed through without re-narrowing.

**Code:**

```tsx
<input type="checkbox" checked={isVisible} onChange={() => setIsVisible(!isVisible)} />
```

**packages/next/src/client/route-params.ts:200**

🟡 **Medium** (Confidence: 78/100) - out-of-diff helper duplication that should be unified

<!-- cr-finding id="813f34f71a76" snippet64="Ly8gYXJiaXRyYXJ5IGNvZGUgYXQgbGluZSAyMDA=" -->

**Issue & impact:** There is an existing helper in this module that performs a similar decode/encode round-trip on the server side. Maintaining two slightly-different helpers will drift; recommend extracting a shared canonicalizeURLPart utility used by both sides.

**Code:**

```typescript
// arbitrary code at line 200
```

_Note: Inline comments failed ({API_ERROR})._

🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>

<sub>Diff-scoped automated review — does not run tests or builds, verify test correctness, or examine code outside this PR's diff. Pair with CI and human review.</sub>
