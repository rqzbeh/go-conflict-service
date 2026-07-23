# گزارش استقرار

## هدف

| مورد | مقدار |
|---|---|
| Host | `nl-main.z3df1lter.uk` |
| User | `root` |
| مسیر | `/opt/circular-conflict` |
| Compose | `compose.go-conflict.yml` |
| URL عمومی | `http://nl-main.z3df1lter.uk:18080/` |

پورت host `8080` قبلاً توسط سرویس دیگر اشغال بود → publish روی **18080**.

## استقرار سریع

```bash
# از ریشه مخزن (نه داخل image تست/npm)
bash scripts/fast-deploy.sh
```

مراحل: تست محلی کوتاه → tar slim روی SSH → `docker compose build` با BuildKit cache
→ health ≤۴۰s → smoke cold-start.

`Dockerfile` فقط باینری Go + UI prebuilt می‌سازد (بدون `go test`/`npm` داخل image).

## env عملیاتی (نمونه)

```
OPENAI_BASE_URL=https://nl-main.z3df1lter.uk/v1
OPENAI_API_KEY=***
OPENAI_MODEL=ag/gemini-3.6-flash-high
OPENAI_TIMEOUT_SECONDS=20
OPENAI_EMBEDDING=1
OPENAI_EMBEDDING_MODEL=gemini-embedding-001
OPENAI_EMBEDDING_TIMEOUT_SECONDS=12
LLM_CLASSIFY=0
```

## تأیید روی VPS (آخرین دور)

| بررسی | نتیجه |
|---|---|
| `GET /health` | `status=ok`، persistence، eligibility |
| embeddings | `backend=openai_compatible_neural`, `model=gemini-embedding-001` |
| llm | enabled، مدل chat بالا |
| circulars seed | ≥۸ بخشنامه BX (+ موارد تست) |
| cold-start `/assist` | risk_score=**۵۸**, medium |
| eligibility `run_eval.py` | **۵۴/۵۴** decision/evidence، gap 20/20 |
| conflict `evalconflict` | F1=**1.0** (۹ TP) |
| analyze: supersession جزئی بند ۲ | فقط هدف `#2` |
| analyze: نظارتی vs داخلی | full_contradiction، win=supervisory |
| analyze: سقف جدیدتر | partial_contradiction، win=newer |
| analyze: هم‌تاریخ دو واحد | needs_review |
| archive scan | تعارض‌های طراحی‌شده BX-1005 با 1007/1002/1003 |

نمونه health (شکل؛ تعداد circulars متغیر است):

```json
{
  "status": "ok",
  "service": "go-conflict-service",
  "embeddings": {
    "enabled": true,
    "backend": "openai_compatible_neural",
    "model": "gemini-embedding-001",
    "base_url": "https://nl-main.z3df1lter.uk/v1"
  },
  "llm": {
    "enabled": true,
    "model": "ag/gemini-3.6-flash-high",
    "base_url": "https://nl-main.z3df1lter.uk/v1"
  },
  "eligibility": {"enabled": true},
  "persistence": {"enabled": true}
}
```

## مخازن

- GitHub (public): https://github.com/rqzbeh/go-conflict-service
- Sharif GitLab (private): https://git.sharifict.com/rqzbeh/go-conflict-service
