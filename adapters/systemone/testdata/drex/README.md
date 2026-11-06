# Drex response fixtures

Response fixtures for the hosted Drex API (`https://drex.nace.ai/v1/systemone`),
used by the System One adapter tests. Historical captures replace request IDs
with placeholders. The newer synthetic-input capture retains only the
explicitly listed fields below. Documented examples are marked separately.

| File | Status | Provenance |
| --- | --- | --- |
| `success-200.json` | 200 | Captured live on 2026-09-27 (`drex-latest`) |
| `success-confidence-200.json` | 200 | Captured from a synthetic request on 2026-11-06 at 12:00 UTC (`drex-v1.5`); sanitized as described below |
| `credential-rejected-401.json` | 401 | Captured live on 2026-09-27 |
| `request-rejected-422.json` | 422 | Captured live on 2026-09-27 (empty `questions`) |
| `throttled-429.json` | 429 | From Drex documentation; not yet captured live |
| `overloaded-529.json` | 529 | From Drex documentation; not yet captured live |

`success-confidence-200.json` comes from one synthetic state describing a used
wooden chair in good condition. The single `described_item` noul question asks
whether the item is described in detail, including its condition. Only `model`,
the synthetic question ID, the answer's `type`, `noul` and `confidence`, and
`usage.input_tokens`/`usage.output_tokens` are retained. These allowlisted values
are unchanged. Provider request IDs, evaluation timings, headers and credentials
are not retained. This capture verifies the response shape on that date, not
when the provider introduced the extra field.

Live responses carry `x-request-id` and list `retry-after, retry-after-ms` in
`access-control-expose-headers`. The documented 429 and 529 responses send
`retry-after` in whole seconds and `retry-after-ms` in milliseconds. Replace
the documented fixtures with live captures when available.
