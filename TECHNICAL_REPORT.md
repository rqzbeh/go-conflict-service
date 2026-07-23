# گزارش فنی سرویس Go (۱–۲ صفحه)

## معماری

```
UI (React, RTL، فارسی)
        │
        ▼
   conflictd (Go HTTP)
   ├── Conflict Engine
   │    ├── Parser / Feature Extraction (بند، حکم، مبلغ، ماه، ارجاع)
   │    ├── Embedding: neural gemini-embedding-001 (+ fallback hashed)
   │    ├── Semantic Search (`GET /search`, isCandidate)
   │    ├── Classifier قطعی (+ LLM اختیاری borderline / خلاصه)
   │    ├── Resolver (نظارتی > داخلی، تاریخ جلالی، needs_review)
   │    └── Store (حافظه / Postgres JSONB)
   └── Eligibility Assistant
        ├── Profile / Intake / Cold-start
        ├── Matching Engine قطعی (هم‌تراز reference_engine)
        ├── Rule extraction از متن بخشنامه
        ├── Offer / Explainer / Legal summary
        └── Mock Identity / Financial / RBCI
```

## استخراج بند و ویژگی

- تقسیم متن روی «بند N)»
- `RulingType`: permission / prohibition / obligation / exception / amendment
- شرایط: ریسک، ضامن، وثیقه، ماه/سال، `amount_toman` (ریال÷۱۰، میلیون/میلیارد، واژه‌عدد)
- ارجاع: `بخشنامه X` و شناسه‌های BX/GT

## Embedding / Semantic Search

1. **پیش‌فرض عملیاتی:** `POST {OPENAI_BASE_URL}/embeddings` با مدل
   `gemini-embedding-001` (۳۰۷۲ بعدی). مدل chat (`ag/...`) embeddings ندارد.
2. **Fallback آفلاین:** hashed bag-of-tokens + synonym + char 3-gram (۱۲۸ بعدی).
3. استارتاپ: `EnrichStoreEmbeddings` بردارهای قدیمی ۱۲۸ بعدی را در صورت وجود API ارتقا می‌دهد.
4. جست‌وجو: ترکیب cosine embedding + lexical overlap.

## تشخیص تعارض

انواع: `full_contradiction` | `partial_contradiction` | `overlap_without_conflict` | `supersession` | `neutral`.

- نسخ صریح/جزئی: «بند N بخشنامه X» فقط همان بند N (`referencedClauseNumber` + `amendmentTargetsOtherClause`)
- آستانه ماه/سال فقط روی **subject یکسان** (جلوگیری از FP بین دسته‌چک و وام)
- مبلغ: subject یکسان یا سقف هم‌خانواده
- `LLM_CLASSIFY=0` پیش‌فرض: classify قطعی در Analyze/Scan (پایدار و سریع)
- LLM chat: `plain_language_summary` و deep-review با guardrail (نمی‌تواند تعارض قطعی را خنثی کند)

## اولویّت‌بندی (Resolver)

1. سطح سلسله‌مراتب: supervisory > internal  
2. تاریخ صدور جلالی جدیدتر  
3. هم‌سطح + هم‌تاریخ → `needs_review` (تصمیم انسانی)

## پایداری و استقرار

- `DATABASE_URL` → Postgres JSONB؛ وگرنه `STATE_PATH` یا حافظه
- seed از `DATA_DIR/circulars*`
- Compose: `compose.go-conflict.yml` → host `18080`
- VPS: `http://nl-main.z3df1lter.uk:18080/` مسیر `/opt/circular-conflict`

## نتایج ارزیابی (بازتولیدپذیر)

| آزمون | نتیجه |
|---|---|
| `go test ./internal/conflict` | پاس (۵۲ تست آفلاین) |
| `cmd/evalconflict` — ۸ سناریو / ۹ زوج | Precision=Recall=F1=**1.0** |
| eligibility `run_eval.py` | **۵۴/۵۴** decision + evidence؛ gap 20/20 |
| cold-start student/small_credit/age28 | score **۵۸** medium |
| `/health` embeddings | `openai_compatible_neural` / `gemini-embedding-001` |

سناریوهای PDF در GT: نظارتی>داخلی، سقف جدیدتر، ماه جدیدتر، هم‌تاریخ needs_review،
اصلاح جزئی بند ۲، لغو BX، سه تعارض طراحی‌شده آرشیو BX-1005 با BX-1007/1002/1003،
هم‌پوشانی سازگار بدون تعارض.

## GT تعارض

کمیته در بسته چالش GT رسمی زوج‌های تعارض منتشر نکرده؛
`go-conflict-service/eval/ground_truth.json` از سناریوهای متن PDF + تعارض‌های
طراحی‌شده در بخشنامه‌های مصنوعی BX ساخته شده است. GT رسمی eligibility همان
`eval/ground_truth.json` بسته چالش است (۵۴ زوج).

## تحویل ویدیو

ویدیوی دمو طبق درخواست صریح کاربر **صرف‌نظر** شده و جزء تحویل نیست.
