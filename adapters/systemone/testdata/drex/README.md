# Drex response fixtures

Response bodies from the hosted Drex API (`https://drex.nace.ai/v1/systemone`),
used by the System One adapter tests. Request IDs are replaced with
placeholders; everything else is unchanged.

| File | Status | Provenance |
| --- | --- | --- |
| `success-200.json` | 200 | Captured live on 2026-09-27 (`drex-latest`) |
| `credential-rejected-401.json` | 401 | Captured live on 2026-09-27 |
| `request-rejected-422.json` | 422 | Captured live on 2026-09-27 (empty `questions`) |
| `throttled-429.json` | 429 | From Drex documentation; not yet captured live |
| `overloaded-529.json` | 529 | From Drex documentation; not yet captured live |

Live responses carry `x-request-id` and list `retry-after, retry-after-ms` in
`access-control-expose-headers`. The documented 429 and 529 responses send
`retry-after` in whole seconds and `retry-after-ms` in milliseconds. Replace
the documented fixtures with live captures when available.
