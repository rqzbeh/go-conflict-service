# Deployment Report

## Target

- Host: `nl-main.z3df1lter.uk`
- User: `root`
- Deploy path: `/opt/circular-conflict`
- Compose file: `/opt/circular-conflict/compose.go-conflict.yml`
- Public URL: `http://nl-main.z3df1lter.uk:18080/`

Port `8080` was already used by `warehouse-routing-api-1`, so this service is published on host port `18080`.

## Commands Used

```bash
docker compose -f compose.go-conflict.yml up -d --build
curl -fsS http://localhost:18080/health
```

The Docker build ran `go test ./...` inside the image build and passed.

## Remote Verification

The deployed API passed these checks on the VPS:

- `GET /health` returned the service name and seeded circular count.
- `GET /health` confirmed LLM is enabled with model `ag/gemini-3.6-flash-high`.
- `GET /circulars` returned 8 seeded circulars.
- `POST /circulars` accepted a test circular.
- `POST /circulars/T-REMOTE/analyze` found a high-risk checkbook contradiction.
- The contradiction resolved to `BX-1007#1`, the superior supervisory clause.
- `GET /circulars/T-REMOTE/clauses/1` returned exact clause evidence.
- `POST /scans/archive` returned an archive report.
- A real OpenAI-compatible API integration test against `https://nl-main.z3df1lter.uk/v1` passed with model `ag/gemini-3.6-flash-high`.
- A deployed LLM-backed API analysis found the expected high-risk checkbook contradiction and included an `LLM:` rationale in the report.
- The bonus LLM summary path was tested against the real endpoint and returns plain-language items for legal/compliance users.
- Archive scan latency was fixed by keeping clause-pair comparison deterministic during full-archive scans and using LLM only once for the final legal/compliance summary. Remote `/scans/archive` completed in about 3 seconds after the fix.

After verification, the container was restarted to clear the in-memory test circular. Final public health check:

```json
{"circulars":8,"llm":{"base_url":"https://nl-main.z3df1lter.uk/v1","enabled":true,"model":"ag/gemini-3.6-flash-high"},"service":"go-conflict-service","status":"ok"}
```
