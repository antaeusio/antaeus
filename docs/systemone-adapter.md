# System One adapter

`io.antaeus.systemone` evaluates policy rules with a server that speaks the
System One protocol (`POST /v1/systemone`). Each server is identified by a
provider:

| Provider | Server | Adapter versions | Credential slot | Default variable |
| --- | --- | --- | --- | --- |
| `antaeus` | An Antaeus System One server, such as the open-source [`antaeusio/nli-server`](https://github.com/antaeusio/nli-server) | `0.2.0` | `antaeus-api-key` | `ANTAEUS_API_KEY` |
| `contrastive-lm` | The self-hosted [Contrastive Language Model (CLM)](https://github.com/Contrastive-LM/CLM) reference server | `0.1.0`, `0.2.0` | `clm-api-key` | `CLM_API_KEY` |

Version `0.2.0` is current. Version `0.1.0`, which supports CLM only, stays
installed so existing profiles keep their digests and keep working.

> **Status: experimental.** Neither server's quality on semantic policy
> conditions has been measured on a published evaluation set. CLM's published
> benchmarks cover agent, computer-use, and tool-calling tasks. Do not use either
> for enforcement without evaluating it on representative cases. When you run
> the server yourself, your inputs go to the infrastructure you choose.

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
example. Two CLM example profiles are in [`examples/clm`](../examples/clm):

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
| `adapter` | `{"id": "io.antaeus.systemone", "version": "0.2.0"}`, or `"0.1.0"` for CLM |
| `protocol` | `{"id": "io.antaeus.rule-match", "version": "v0alpha1"}` |
| `requiredCapabilities` | a subset of `json-input`, `structured-rule-results`, `confidence-scores` |
| `provider` | `antaeus` (version `0.2.0` only) or `contrastive-lm` |
| `model` | the name the server serves, for example `clm-latest` |
| `modelRevision`, `instructionTemplate` | omitted |
| `credentialSlot` | omitted, or the provider's slot from the table above when the server requires a key |
| `parameters.endpoint` | the server's base URL; no other parameters are accepted |

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
reference is that provider's variable (`ANTAEUS_API_KEY` or `CLM_API_KEY`), and
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
| TLS verification failure | `systemone.tls_failed` | no |
| Missing, extra, or non-`noul` answers, or a probability outside 0–1 | `systemone.output_invalid` | no |
| Malformed response or a success status other than 200 | `systemone.response_malformed` | no |
| Response larger than 1 MiB | `systemone.response_too_large` | no |

As with every adapter, the runner records only the three retryable codes in
Decisions; the others appear as `evaluation.adapter_failed`.

## Not yet included

- Other System One providers, including TypeSafe Jev, which will be enabled
  once its live API contract is verified.
- `nli-server` answers only yes/no questions and rejects `choice` and `score`
  questions; this adapter sends only yes/no questions.
- Confidence escalation from CLM to an evaluator that does not report
  confidence, such as OpenAI. Evaluator profile v0alpha1 requires every
  evaluator on a confidence-routed path to report confidence.
- A measured comparison of these servers with other evaluators on policy tasks.
