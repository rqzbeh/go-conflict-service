# سرویس Go — تشخیص تعارض بخشنامه + دستیار اهلیت بانکی

پیاده‌سازی فقط داخل `go-conflict-service/` است. پوشه چالش
`EligibilityAssistant&IntelligentBankingOffer/` فقط به‌عنوان **داده فقط‌خواندنی**
خوانده می‌شود و تغییر داده نمی‌شود.

دو دامنه:

1. **Circular Conflict & Consistency Detection** (الزام PDF اصلی)
2. **Agentic Eligibility Assistant & Intelligent Banking Offer** (`/assist` + بسته چالش)

## اجرا محلی

```bash
cd go-conflict-service
go test ./...
go vet ./...
DATA_DIR="../EligibilityAssistant&IntelligentBankingOffer/data" \
  go run ./cmd/evalconflict eval/ground_truth.json

DATA_DIR="../EligibilityAssistant&IntelligentBankingOffer/data" \
MOCK_ROOT="../EligibilityAssistant&IntelligentBankingOffer" \
OPENAI_BASE_URL=https://nl-main.z3df1lter.uk/v1 \
OPENAI_API_KEY=replace-me \
OPENAI_MODEL=ag/gemini-3.6-flash-high \
OPENAI_EMBEDDING=1 \
OPENAI_EMBEDDING_MODEL=gemini-embedding-001 \
go run ./cmd/conflictd
```

UI و API: `http://localhost:8080`

## Docker (از ریشه مخزن)

```bash
cat > .env <<'EOF'
OPENAI_BASE_URL=https://nl-main.z3df1lter.uk/v1
OPENAI_API_KEY=replace-me
OPENAI_MODEL=ag/gemini-3.6-flash-high
OPENAI_TIMEOUT_SECONDS=20
OPENAI_EMBEDDING=1
OPENAI_EMBEDDING_MODEL=gemini-embedding-001
OPENAI_EMBEDDING_TIMEOUT_SECONDS=12
LLM_CLASSIFY=0
EOF

docker compose -f compose.go-conflict.yml up -d --build
curl -fsS http://127.0.0.1:18080/health
python3 EligibilityAssistant\&IntelligentBankingOffer/eval/run_eval.py --url http://127.0.0.1:18080
```

## APIهای اصلی

### اهلیت / آفر

| مسیر | توضیح |
|---|---|
| `POST /assist` | اهلیت ۶ محصول + آفر + Gap + evidence + conversation |
| `POST /assist/intake` | intake مشتری غیرموجود |
| `GET /identity/{national_id}` | هویت |
| `GET /financial/{national_id}` | مالی |
| `POST /rbci/score` | `lookup` یا `cold_start` |
| `GET /products` | کاتالوگ |
| `GET /eligibility/rules` | قواعد استخراج‌شده از متن بخشنامه |

### تعارض بخشنامه

| مسیر | توضیح |
|---|---|
| `POST /circulars` | ثبت + پارس بندها + embedding |
| `POST /circulars/{id}/analyze` | مقایسه با آرشیو |
| `GET /circulars/{id}/report` | گزارش مغایرت |
| `GET /circulars/{id}/clauses/{n}` | بند دقیق (evidence) |
| `GET /search?q=...` | جست‌وجوی معنایی بندها |
| `POST /scans/archive` | اسکن کل آرشیو |
| `GET /relationships` | روابط actionable |
| `POST /relationships/{id}/deep-review` | بازبینی عمیق (LLM اختیاری) |
| `PATCH /relationships/{id}` | تصمیم انسانی حقوقی/تطبیق |

## Embedding و LLM

| متغیر | نقش |
|---|---|
| `OPENAI_BASE_URL` + `OPENAI_API_KEY` | gateway سازگار OpenAI |
| `OPENAI_EMBEDDING_MODEL` | پیش‌فرض `gemini-embedding-001` (۳۰۷۲ بعدی) — **نه** مدل chat |
| `OPENAI_EMBEDDING=0` | خاموش کردن عصبی → fallback hashed محلی |
| `OPENAI_MODEL` | chat برای خلاصه/deep-review (`ag/gemini-3.6-flash-high`) |
| `LLM_CLASSIFY=0` | پیش‌فرض: طبقه‌بندی جفت‌بند **قطعی** (سریع/پایدار) |
| `LLM_CLASSIFY=1` | LLM فقط برای جفت‌های borderline |

`GET /health` فیلدهای `embeddings` و `llm` را بدون افشای کلید برمی‌گرداند.

## سناریوهای نمونه

```bash
# خانه‌دار
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"12345678"}'

# مدیر
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"23456789"}'

# چک برگشتی / ریسک زیاد
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"90123456"}'

# Cold-start → score ۵۸ / medium
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"99999991","self_declared":{"name":"مشتری جدید","age":28,"job_category":"student","purpose_category":"small_credit","declared_monthly_income":12000000,"requested_amount":80000000}}'
```

## ارزیابی

| ابزار | انتظار |
|---|---|
| `go test ./...` | unit/integration (۵۲ تست آفلاین در `internal/conflict`) |
| `go run ./cmd/evalconflict` | F1=1.0 روی ۸ سناریو / ۹ زوج GT تعارض |
| `python eval/run_eval.py --url ...` | ۵۴/۵۴ decision + evidence + gap |
| cold-start student/small_credit/age28 | score **۵۸** medium |

## استقرار

- URL: `http://nl-main.z3df1lter.uk:18080/`
- مسیر VPS: `/opt/circular-conflict`
- Postgres + `SCAN_INTERVAL_SECONDS` برای اسکن دوره‌ای
- اسکریپت سریع: `scripts/fast-deploy.sh` (ریشه مخزن)

## مستندات

| فایل | محتوا |
|---|---|
| `PDF_COMPLIANCE_CHECKLIST.md` | انطباق خط‌به‌خط با PDF |
| `TECHNICAL_REPORT.md` | معماری و روش (۱–۲ صفحه) |
| `ELIGIBILITY_ASSISTANT_FA.md` | دامنه اهلیت |
| `DEPLOYMENT_REPORT.md` | وضعیت استقرار |
| `EligibilityAssistant&IntelligentBankingOffer.md` | متن PDF تبدیل‌شده |
| `FINAL_REPORT_FA.html` / `.pdf` | گزارش نهایی فارسی |

## تحویل PDF

- کد + Dockerfile + README + UI/API + F1 + گزارش روش: **انجام شده**
- ویدیوی دمو: **صرف‌نظر به درخواست کارفرما/کاربر**
