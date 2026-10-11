# v0alpha2 conformance

These fixtures specify current Decision reduction, evaluator-profile validation,
and runner failure enrichment. Unchanged policy and rule-match contracts remain
v0alpha1. Published v0alpha1 conformance fixtures are preserved separately.

`reduction/cases.json` covers matched deny over failed and indeterminate evidence,
failed evidence over matched review or uncertainty, uncertainty over matched
allow and matched review, explicit review, allow, and default routing. Review
from uncertainty requires `evaluation.indeterminate_rule` and no failure object.

`evaluator-profile/` specifies new profile versions and fixed terminal
`onIndeterminate: review`, `onFailure: failure`, including rejection of the old
failure terminal. Numeric canonical identity fixtures cover the new profile
version and content digest. Other validation rules are unchanged.

`runner/failure-mapping.json` preserves operational error mapping and retryability;
its generic indeterminate case now reduces to review. Trace version and terminal
values are unchanged; trace terminal is evidence metadata, not the Decision outcome.
