### Code review

Inline comment posting failed. All issues listed below.

**packages/next/src/client/route-params.ts:49**

📝 **Minor** (Confidence: 80/100) - decode+encode round-trip on every URL segment

**Explanation:** _Note: This comment was placed on the nearest diff line; the issue actually occurs on line 48._

canonicalizeURLPart now runs on every catchall and dynamic segment for every navigation. The decode/encode round-trip is fast individually but adds up with deep catchalls; consider memoizing for repeat keys.

**Code:**

```typescript
// Pathname parts come from URL.pathname.split('/')
```

**packages/next/src/client/route-params.ts:121**

🟡 **Medium** (Confidence: 78/100) - decoded user-controlled URL parts re-encoded without explicit allow-list

**Explanation:** canonicalizeURLPart blindly decodeURIComponent's the input then re-encodes. If a malicious pathname part contains a UTF-8 sequence that survives decode but produces unexpected characters after re-encoding, the resulting segment could mismatch server-side routing in ways that bypass middleware checks.

_Note: This issue was flagged in a prior review but the code has since changed._

_This finding was also independently raised by `security` (confidence 78) at `packages/next/src/client/route-params.ts:120`._

**Code:**

```typescript
return canonicalizeURLPart(pathnameParts[partIndex])
```

**test/e2e/app-dir/segment-cache/encoded-slash-params/components/link-accordion.tsx:18**

🟡 **Medium** (Confidence: 65/100) - checked toggle without controlled-onChange parity loses NormalizedPathname brand

**Explanation:** The function signature of canonicalizeURLPart returns a plain string which loses the NormalizedPathname brand applied earlier in the file. Downstream callers that consume the result must re-cast or risk a silent type-narrowing escape — the same concern surfaces in this LinkAccordion's controlled state where href is passed through without re-narrowing.

_This finding was also independently raised by `typescript` (confidence 65) at `packages/next/src/client/route-params.ts:55`._

**Code:**

```tsx
<input type="checkbox" checked={isVisible} onChange={() => setIsVisible(!isVisible)} />
```

**packages/next/src/client/route-params.ts:200**

🟡 **Medium** (Confidence: 78/100) - out-of-diff helper duplication that should be unified

**Explanation:** There is an existing helper in this module that performs a similar decode/encode round-trip on the server side. Maintaining two slightly-different helpers will drift; recommend extracting a shared canonicalizeURLPart utility used by both sides.

**Code:**

```typescript
// arbitrary code at line 200
```


_Note: Inline comments failed ({API_ERROR})._

🤖 Generated with [Claude Code](https://claude.ai/code)

<sub>If this code review was useful, please react with 👍. Otherwise, react with 👎.</sub>
