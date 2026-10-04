# System One adapter

`io.antaeus.systemone` evaluates policy rules with a server that speaks the
System One protocol (`POST /v1/systemone`). Each server is identified by a
provider:

| Provider | Server | Adapter versions | Credential slot | Default variable |
| --- | --- | --- | --- | --- |
| `antaeus` | An Antaeus System One server, such as the open-source [`antaeusio/nli-server`](https://github.com/antaeusio/nli-server) | `0.2.0`, `0.3.0`, `0.4.0` | `antaeus-api-key` | `ANTAEUS_API_KEY` |
| `contrastive-lm` | The self-hosted [Contrastive Language Model (CLM)](https://github.com/Contrastive-LM/CLM) reference server | `0.1.0`, `0.2.0`, `0.3.0`, `0.4.0` | `clm-api-key` | `CLM_API_KEY` |
| `drex` | The hosted [Drex](https://drex.nace.ai) API by Nace.AI | `0.3.0`, `0.4.0` | `drex-api-key` (required) | `DREX_API_KEY` |

Version `0.4.0` opts into [provider usage reporting](./provider-usage.md).
It preserves `0.3.0` evaluation and retry behavior while reporting token counts
in a separate Decision extension. Existing examples remain pinned to `0.3.0`;
select `0.4.0` explicitly to obtain accounting. Go `Identity` and `Registration`
retain `0.3.0`; use `UsageIdentity` and `UsageRegistration` for `0.4.0`.

Version `0.3.0` adds provider `drex`, retries HTTP 529, honors
provider `Retry-After` waits, and records the provider's `X-Request-Id` in
adapter metadata. Versions `0.2.0` (Antaeus and CLM) and `0.1.0` (CLM only)
stay installed with their original behavior, so existing profiles keep their
digests and keep working.

> **Status: experimental.** No provider's quality on semantic policy
> conditions has been measured on a published evaluation set. CLM's published
> benchmarks cover agent, computer-use, and tool-calling tasks. Do not use any
> of them for enforcement without evaluating it on representative cases. When
> you run the server yourself, your inputs go to the infrastructure you choose.
> Provider `drex` sends policy conditions and unredacted input to Nace.AI; review
> its terms for processing location and retention before sending personal or
> confidential data.

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

## Running an Antaeus server

[`antaeusio/nli-server`](https://github.com/antaeusio/nli-server) runs on CPU
in a container. Build its image as its README describes, then start it with the
model name and key the example profile expects:

```sh
export ANTAEUS_API_KEY="$(openssl rand -hex 16)"
docker run --rm -d -p 127.0.0.1:8080:8080 \
  -e NLI_MODEL_NAME=antaeus-local -e NLI_API_KEY="$ANTAEUS_API_KEY" \
  antaeus-nli-server:dev

antaeus evaluate-profile --profile examples/antaeus/nli-server.json \
  --policy examples/marketplace/listing-policy.yaml \
  --input examples/marketplace/replica-watch.json
```

The profile's `model` must equal the server's `NLI_MODEL_NAME`; the server
rejects any other name. The server's key (`NLI_API_KEY`) must equal
`ANTAEUS_API_KEY`. For a server without a key, remove the `credentialSlot` and
the `credentialSlots` entry from the profile.

## Using Drex

Drex is a hosted System One service, so there is no server to run. Create an
API key, then:

```sh
export DREX_API_KEY=...   # your nace_sk_ key

antaeus evaluate-profile --profile examples/drex/profile.json \
  --policy examples/marketplace/listing-policy.yaml \
  --input examples/marketplace/replica-watch.json
```

A `drex` evaluator must use endpoint `https://drex.nace.ai` and declare the
`drex-api-key` slot; the adapter rejects any other endpoint so the Drex key is
never sent elsewhere. Drex does not report a model revision, so results for
`drex-latest` are not exactly reproducible across provider updates. Drex limits
concurrent requests per account; the adapter does not queue requests, so a
caller that exceeds the limit receives `evaluator.throttled`, which the profile
may retry after the provider's stated wait.

## Running a CLM server

CLM needs Linux with an NVIDIA GPU. Follow the
[CLM quickstart](https://github.com/Contrastive-LM/CLM#quickstart): serve the
Qwen3-8B encoder with vLLM, then start `clm-serve`, which listens on port 8700
by default. Set `CLM_API_KEY` on the server to require a bearer token.

The CLM repository also includes `tools/playground_mock.py`, which serves the
real API with a fake encoder and no GPU. Use it to check connectivity and
configuration only: its numbers are meaningless.

## Profiles

[`examples/antaeus/nli-server.json`](../examples/antaeus/nli-server.json) uses provider
`antaeus` on adapter `0.2.0`, with the same confidence gating as the first CLM
example. [`examples/drex/profile.json`](../examples/drex/profile.json) uses
provider `drex` on adapter `0.3.0`: a 10-second total budget, one retry on
timeout, unavailable, or throttled, and rules below 0.7 confidence become
indeterminate. Two CLM example profiles are in [`examples/clm`](../examples/clm):

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
| `adapter` | ID `io.antaeus.systemone` and an installed version (`0.1.0`–`0.4.0`); select `0.4.0` for usage reporting |
| `protocol` | `{"id": "io.antaeus.rule-match", "version": "v0alpha1"}` |
| `requiredCapabilities` | a subset of `json-input`, `structured-rule-results`, `confidence-scores` |
| `provider` | `antaeus` (`0.2.0`–`0.4.0`), `contrastive-lm`, or `drex` (`0.3.0` and `0.4.0`) |
| `model` | the name the server serves, for example `clm-latest` or `drex-latest` |
| `modelRevision`, `instructionTemplate` | omitted |
| `credentialSlot` | the provider's slot from the table above; required for `drex`, otherwise omitted when the server needs no key |
| `parameters.endpoint` | the server's base URL (`https://drex.nace.ai` for `drex`); no other parameters are accepted |

The endpoint is part of the profile digest. It must be an `https` URL, or
`http` only to a loopback host (`localhost`, a `127.0.0.0/8` address, `::1`,
or an IPv4-mapped loopback address such as `::ffff:127.0.0.1`). It may
include a path prefix but no user information, query, or fragment. The adapter
appends `/v1/systemone`, never follows redirects, verifies TLS certificates, and
ignores proxy environment variables.

The CLM server hot-reloads model heads under the same name, so results for a
name such as `clm-latest` are not exactly reproducible across head updates. The
Decision records the provider and the model name the server reports, so an
Antaeus server should serve a new name whenever its model changes.

## Credentials

When a profile declares a provider's credential slot, its installed default
reference is that provider's variable (`ANTAEUS_API_KEY`, `CLM_API_KEY`, or
`DREX_API_KEY`), and
explicit bindings take precedence.
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
| HTTP 529 (overloaded), versions `0.3.0` and `0.4.0` | `evaluator.unavailable` | yes |
| TLS verification failure | `systemone.tls_failed` | no |
| Missing, extra, or non-`noul` answers, or a probability outside 0–1 | `systemone.output_invalid` | no |
| Malformed response or a success status other than 200 | `systemone.response_malformed` | no |
| Response larger than 1 MiB | `systemone.response_too_large` | no |

The execution trace uses normalized routing codes: it preserves the three
retryable adapter codes and records other adapter failures as
`evaluation.adapter_failed`. The opt-in `0.4.0` usage extension additionally
preserves bounded underlying adapter failure codes. In versions
`0.1.0` and `0.2.0`, HTTP 529 is `systemone.unexpected_status` and not
retryable.

In versions `0.3.0` and `0.4.0`, a retryable failure also carries the provider's required
wait: `Retry-After-Ms` in milliseconds when valid, otherwise `Retry-After` as
delay-seconds or an HTTP date. Malformed or negative values are ignored. The
runner never retries sooner than that wait, and skips a retry that cannot
start within the evaluation deadline (see
[retry and routing rules](./profile-execution.md#retry-and-routing-rules)).

## Not yet included

- Other System One providers, including TypeSafe Jev, which will be enabled
  once its live API contract is verified.
- `nli-server` answers only yes/no questions and rejects `choice` and `score`
  questions; this adapter sends only yes/no questions.
- Confidence escalation from CLM to an evaluator that does not report
  confidence, such as OpenAI. Evaluator profile v0alpha1 requires every
  evaluator on a confidence-routed path to report confidence.
- A measured comparison of these servers with other evaluators on policy tasks.
- Provider request IDs in Decisions or traces. Versions `0.3.0` and `0.4.0` report
  `X-Request-Id` in adapter metadata, but the v0alpha1 Decision and execution
  trace have no field for it.
- Client-side concurrency limits. Callers sharing a Drex account must keep
  their combined in-flight requests within its limit.
