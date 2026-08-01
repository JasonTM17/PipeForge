# Data quality

PipeForge stores reusable quality-rule metadata in the Go control plane and
freezes the validated definition into each processing job. Editing a rule
therefore affects future jobs only; an active job always evaluates the exact
snapshot that was accepted with its request.

## Supported rules

| Rule | Scope | Configuration |
| --- | --- | --- |
| `NOT_NULL` | one column | none |
| `UNIQUE` | one to 16 columns | optional `maxTrackedValues` |
| `BETWEEN` | one column | numeric `min`, `max` |
| `MIN_LENGTH`, `MAX_LENGTH` | one column | integer `value` |
| `REGEX` | one column | bounded `pattern` |
| `EMAIL_FORMAT` | one column | none |
| `DATE_FORMAT` | one column | bounded `format` |
| `ALLOWED_VALUES`, `REFERENTIAL_SET` | one column | scalar `values` list |
| `COLUMN_TYPE` | one column | `string`, `integer`, `number`, `boolean`, `date`, or `datetime` |
| `ROW_COUNT_BETWEEN` | dataset | numeric `min`, `max` |

`CUSTOM_EXPRESSION` is rejected. This is deliberate: arbitrary code execution
is not an acceptable quality-rule boundary. A future expression language needs
its own grammar, resource budget, and security review before it can be enabled.

## API

Authenticated owners with `datasets:read`/`datasets:write` can manage rules at
`/api/v1/quality-rules`; the nested
`/api/v1/datasets/{datasetID}/quality-rules` route is available when the dataset
is already known. `/v1` aliases are supported for compatibility. Rule names are
unique among active rules within a dataset. Delete is a soft delete and every
create/update/delete is audit-recorded by the SQL repository.

The API rejects unknown JSON fields, unsafe column identifiers, duplicate
columns, invalid severity/type combinations, oversized sets, and invalid regex
constructs. List responses are paginated with a maximum page size of 100.

## Worker output

The worker writes one attempt-scoped `quality.json` artifact under
`reports/{jobId}/attempt-{attemptNumber}/quality.json`, validated as
`quality.report.v1`. Failure references contain only a row number and a
truncated hash; raw row values are not returned through the API or broker.
The report is bounded to 16 MiB and at most 128 failure references per rule.

## Verification

```powershell
python\.venv\Scripts\python.exe -m pytest python/tests/test_validators.py -q
python\.venv\Scripts\python.exe scripts/validate-contracts.py
Push-Location go
go test ./internal/quality ./internal/httpapi ./internal/job
Pop-Location
```

The Go HTTP tests cover lifecycle, ownership isolation, soft deletion, strict
JSON input, and fail-closed custom expressions. Python tests cover cross-chunk
aggregation, missing columns, disabled rules, and bounded rule evaluation.
