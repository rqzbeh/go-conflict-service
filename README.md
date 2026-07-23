# سرویس Go — تشخیص تعارض بخشنامه + دستیار اهلیت بانکی

این سرویس فقط داخل `go-conflict-service/` پیاده‌سازی شده و پوشه چالش اصلی
`EligibilityAssistant&IntelligentBankingOffer/` را فقط به‌عنوان دادهٔ فقط‌خواندنی
می‌خواند.

دو دامنه را با هم پوشش می‌دهد:

1. **Circular Conflict & Consistency Detection** (الزام PDF اصلی)
2. **Agentic Eligibility Assistant & Intelligent Banking Offer** (بسته چالش + `/assist`)

## اجرا محلی

```bash
cd go-conflict-service
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/evalconflict eval/ground_truth.json

DATA_DIR="../EligibilityAssistant&IntelligentBankingOffer/data" \
MOCK_ROOT="../EligibilityAssistant&IntelligentBankingOffer" \
go run ./cmd/conflictd
```

UI و API روی `http://localhost:8080`.

## Docker

از ریشه مخزن:

```bash
cat > .env <<'EOF'
OPENAI_BASE_URL=https://nl-main.z3df1lter.uk/v1
OPENAI_API_KEY=replace-me
OPENAI_MODEL=ag/gemini-3.6-flash-high
OPENAI_TIMEOUT_SECONDS=20
EOF

docker compose -f compose.go-conflict.yml up -d --build
curl -fsS http://127.0.0.1:18080/health
python3 EligibilityAssistant\&IntelligentBankingOffer/eval/run_eval.py --url http://127.0.0.1:18080
```

## APIهای اصلی

### اهلیت / آفر

| مسیر | توضیح |
|---|---|
| `POST /assist` | نقطه داوری؛ اهلیت ۶ محصول + آفر + Gap + evidence + conversation |
| `POST /assist/intake` | مسیر مکالمه‌ای مشتری غیرموجود |
| `GET /identity/{national_id}` | هویت |
| `GET /financial/{national_id}` | مالی |
| `POST /rbci/score` | `lookup` یا `cold_start` |
| `GET /products` | کاتالوگ محصولات |
| `GET /eligibility/rules` | ماتریس محصول↔شرط با متن بند استخراج‌شده |

### تعارض بخشنامه

| مسیر | توضیح |
|---|---|
| `POST /circulars` | ثبت بخشنامه جدید + پارس بندها |
| `POST /circulars/{id}/analyze` | مقایسه با آرشیو |
| `GET /circulars/{id}/report` | گزارش مغایرت |
| `GET /circulars/{id}/clauses/{n}` | بند دقیق برای evidence |
| `POST /scans/archive` | اسکن کل آرشیو |
| `GET /relationships` | روابط actionable |
| `POST /relationships/{id}/deep-review` | بازبینی عمیق (LLM اختیاری) |
| `PATCH /relationships/{id}` | تصمیم انسانی حقوقی/تطبیق |

## سناریوهای PDF / چالش

```bash
# خانه‌دار — رد اکثر اعتباری‌ها با Gap
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"12345678"}'

# مدیر — اغلب مشروط به ضامن
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"23456789"}'

# چک برگشتی / ریسک زیاد
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"90123456"}'

# مشتری جدید — Cold-start امتیاز ۵۸ / متوسط
curl -sS -X POST localhost:8080/assist -H 'content-type: application/json' \
  -d '{"national_id":"99999991","self_declared":{"name":"مشتری جدید","age":28,"job_category":"student","purpose_category":"small_credit","declared_monthly_income":12000000,"requested_amount":80000000}}'
```

## ارزیابی

- `go test ./...` — unit/integration + ground truth ۵۴ زوج
- `go run ./cmd/evalconflict` — Precision/Recall/F1 تعارض‌های نمونه
- `python eval/run_eval.py --url ...` — decision/evidence/gap از بسته رسمی

## استقرار فعلی

- VPS: `http://nl-main.z3df1lter.uk:18080/`
- مسیر: `/opt/circular-conflict`
- Postgres پایدار + اسکن دوره‌ای آرشیو

## مستندات فارسی

- `ELIGIBILITY_ASSISTANT_FA.md`
- `PDF_COMPLIANCE_CHECKLIST.md`
- `TECHNICAL_REPORT.md`
- `EligibilityAssistant&IntelligentBankingOffer.md` (تبدیل PDF)

## Bonus

- خلاصه ساده حقوقی/تطبیق با LLM سازگار OpenAI (`plain_language_summary` / `legal_summary`)
- Deep-review رابطه با guardrail قاعده‌محور
- UI فارسی RTL برای کارمند شعبه و واحد حقوقی
