# System One adapter (self-hosted CLM)

`io.antaeus.systemone@0.1.0` evaluates policy rules with a server that speaks the
System One protocol (`POST /v1/systemone`). Version 0.1.0 supports the
self-hosted [Contrastive Language Model (CLM)](https://github.com/Contrastive-LM/CLM)
reference server. CLM is an Apache-2.0, open-weights model that scores
candidate answers against a state instead of generating text.

> **Status: experimental.** CLM's quality on semantic policy conditions has not
> been measured; its published benchmarks cover agent, computer-use, and
> tool-calling tasks. Do not use it for enforcement without evaluating it on
> representative cases. You run the server yourself, so your inputs go to the
> infrastructure you choose.

## How it evaluates a policy

Each policy rule becomes one yes/no (`noul`) question. The question's ID is the
rule ID and its instructions are the rule's `when` condition. The canonical
input is sent as the `state` object. Rule outcomes and the policy name are
never sent.

The server returns the probability `p` that each condition is true. The adapter
normalizes it as follows:

| Probability | Rule status | Confidence |
| --- | --- | --- |
| `p > 0.5` | `matched` | `p` |
| `p < 0.5` | `not_matched` | `1 - p` |
| `p = 0.5` | `indeterminate` | `0.5` |

Because every rule carries a confidence, profiles can enable confidence routing.
With `onLowConfidence: indeterminate`, rules below the threshold become
indeterminate, and the Decision is `failure` (unless a matched `deny` applies)
so your application can send it to a person.

## Running a CLM server

CLM needs Linux with an NVIDIA GPU. Follow the
[CLM quickstart](https://github.com/Contrastive-LM/CLM#quickstart): serve the
Qwen3-8B encoder with vLLM, then start `clm-serve`, which listens on port 8700
by default. Set `CLM_API_KEY` on the server to require a bearer token.

The CLM repository also includes `tools/playground_mock.py`, which serves the
real API with a fake encoder and no GPU. Use it to check connectivity and
configuration only: its numbers are meaningless.

## Profiles

Two example profiles are in [`examples/clm`](../examples/clm):

- [`confidence-gated.json`](../examples/clm/confidence-gated.json): CLM alone.
  Rules answered with less than 0.8 confidence become indeterminate.
- [`openai-fallback.json`](../examples/clm/openai-fallback.json): CLM first,
  with the [OpenAI evaluator](./openai-adapter.md) as an operational fallback
  when the CLM server is unavailable, throttled, or times out.

```sh
antaeus evaluate-profile --profile examples/clm/confidence-gated.json \
  --policy examples/marketplace/listing-policy.yaml \
  --input examples/marketplace/replica-watch.json
```

A System One evaluator must declare:

| Field | Requirement |
| --- | --- |
| `mode` | `semantic` |
| `adapter` | `{"id": "io.antaeus.systemone", "version": "0.1.0"}` |
| `protocol` | `{"id": "io.antaeus.rule-match", "version": "v0alpha1"}` |
| `requiredCapabilities` | a subset of `json-input`, `structured-rule-results`, `confidence-scores` |
| `provider` | `contrastive-lm` |
| `model` | the served model name, for example `clm-latest` |
| `modelRevision`, `instructionTemplate` | omitted |
| `credentialSlot` | omitted, or `clm-api-key` when the server requires a key |
| `parameters.endpoint` | the server's base URL; no other parameters are accepted |

The endpoint is part of the profile digest. It must be an `https` URL, or
`http` only to a loopback host (`localhost`, a `127.0.0.0/8` address, `::1`,
or an IPv4-mapped loopback address such as `::ffff:127.0.0.1`). It may
include a path prefix but no user information, query, or fragment. The adapter
appends `/v1/systemone`, never follows redirects, verifies TLS certificates, and
ignores proxy environment variables.

The CLM server hot-reloads model heads under the same name, so results for a
name such as `clm-latest` are not exactly reproducible across head updates. The
Decision records the model name the server reports.

## Credentials

When a profile declares the `clm-api-key` slot, its installed default reference
is the `CLM_API_KEY` environment variable, and explicit bindings take precedence.
The key is sent as a bearer token and never appears in output. Without the
slot, no credential is read or sent. Because a profile chooses where input is
sent, a System One profile selected by project configuration requires
`antaeus config trust` even without a credential. `antaeus config check`
reports only credential access, so it can succeed for such a project while
`evaluate-profile` still asks for trust.

## Limits and failures

Requests are limited to 4 MiB and responses to 1 MiB.

| Condition | Adapter code | Retryable |
| --- | --- | --- |
| HTTP 401 or 403 | `systemone.credential_rejected` | no |
| HTTP 400, 404, or 422 | `systemone.request_rejected` | no |
| HTTP 408 or deadline | `evaluator.timeout` | yes |
| HTTP 429 | `evaluator.throttled` | yes |
| HTTP 500, 502, 503, or 504, or a connection failure | `evaluator.unavailable` | yes |
| TLS verification failure | `systemone.tls_failed` | no |
| Missing, extra, or non-`noul` answers, or a probability outside 0–1 | `systemone.output_invalid` | no |
| Malformed response or a success status other than 200 | `systemone.response_malformed` | no |
| Response larger than 1 MiB | `systemone.response_too_large` | no |

As with every adapter, the runner records only the three retryable codes in
Decisions; the others appear as `evaluation.adapter_failed`.

## Not yet included

- Other System One providers, including TypeSafe Jev, which will be enabled
  once its live API contract is verified.
- Confidence escalation from CLM to an evaluator that does not report
  confidence, such as OpenAI. Evaluator profile v0alpha1 requires every
  evaluator on a confidence-routed path to report confidence.
- A measured comparison of CLM with other evaluators on policy tasks.
