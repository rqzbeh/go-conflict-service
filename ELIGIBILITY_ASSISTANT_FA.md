# دستیار اهلیت و آفر بانکی (فارسی / RTL)

## هدف

کارمند شعبه با وارد کردن **کد ملی** مشتری، در یک درخواست نتیجه کامل می‌گیرد:

- وضعیت مشتری (موجود / جدید)
- پروفایل یکپارچه هویت + مالی + RBCI
- حکم اهلیت برای هر ۶ محصول (`eligible` / `conditional` / `not_eligible`)
- **Evidence** بند بخشنامه (`circular_id` + `clause`)
- **Gap Analysis** کمّی برای نزدیک‌شدن به اهلیت
- آفرهای رتبه‌بندی‌شده
- پاسخ مکالمه‌ای فارسی
- پیامد عدم پرداخت تعهدات
- خلاصه حقوقی/تطبیق (قاعده‌محور + اختیاری LLM)

## قرارداد خروجی داوری

حداقل فیلدهای لازم در `POST /assist`:

```json
{
  "national_id": "34567890",
  "customer_status": "existing",
  "risk": {
    "risk_level": "low",
    "risk_score": 22,
    "level": "low",
    "score": 22
  },
  "eligibility": [
    {
      "product_id": "P01",
      "decision": "not_eligible",
      "evidence": [{"circular_id": "BX-1001", "clause": "2"}],
      "gap": [{"metric": "avg_monthly_turnover", "current": 18000000, "required": 50000000}]
    }
  ],
  "offers": [],
  "conversation": "...",
  "legal_summary": "...",
  "payment_impact": "...",
  "trace": [{"agent": "Orchestrator", "tool": "start"}]
}
```

نکته: هم `risk_level/risk_score` و هم aliasهای قرارداد OpenAPI یعنی `level/score` برگردانده می‌شوند.

## مسیر مشتری موجود

1. `GET identity/{id}`
2. `GET financial/{id}`
3. `POST rbci/score` با `mode=lookup`
4. Matching Engine قطعی روی ۶ محصول
5. Offer ranking
6. Explainer فارسی + Legal summary

## مسیر مشتری جدید (Cold-start)

اگر هویت 404 باشد:

1. Intake مکالمه‌ای فیلدها: نام، سن، شغل، هدف، درآمد، مبلغ
2. `POST rbci/score` با `mode=cold_start`
3. نمونه مرجع PDF: دانشجو / اعتبار کوچک / سن ۲۸ → **امتیاز ۵۸ / medium**
4. محصولات اعتباری مستقیم `eligible` نمی‌شوند؛ مشروط به افتتاح حساب و سابقه‌سازی (BX-1008#2)
5. سپرده و قرض‌الحسنه طبق استثنای BX-1008#3 قابل بررسی مشروط‌اند

## استخراج قواعد از متن بخشنامه

`GET /eligibility/rules` شروط را از بندهای `data/circulars/*.txt` استخراج می‌کند
(مبلغ، ماه، ریسک، ضامن، سقف و ...). اگر parser شرطی را نبیند، fallback قطعی
همان نگاشت مرجع فقط برای پایداری eval فعال می‌شود؛ مسیر اصلی تصمیم‌گیری
اهلیت با موتور قطعی هم‌تراز `reference_engine` است.

## سناریوهای اجباری

| کد ملی | انتظار |
|---|---|
| `12345678` | خانه‌دار؛ رد بیشتر اعتباری‌ها + Gap درآمد/گردش |
| `23456789` | مدیر؛ غالباً conditional به‌خاطر ریسک متوسط/ضامن |
| `34567890` | کارمند؛ نزدیک eligible ولی ممکن است gap گردش داشته باشد |
| `90123456` | چک برگشتی/ریسک زیاد؛ رد ابزارهای اعتباری با BX-1007 |
| `99999991` | غیرموجود؛ intake + cold-start 58 |

## APIهای Mock داخلی

- `GET /identity/{national_id}`
- `GET /financial/{national_id}`
- `POST /rbci/score`
- `GET /products`
- `GET /eligibility/rules`

## تست‌ها

```bash
go test ./internal/conflict -run 'Assist|Eligibility|RiskContract' -v
python3 ../EligibilityAssistant\&IntelligentBankingOffer/eval/run_eval.py --url http://localhost:8080
```

انتظار: decision accuracy 100٪ روی ۵۴ زوج ground truth رسمی.
