# چک‌لیست انطباق با PDF (Circular Conflict)

منبع: `EligibilityAssistant&IntelligentBankingOffer.md` (تبدیل PDF).

| الزام PDF | وضعیت | محل |
|---|---|---|
| ثبت بخشنامه جدید | ✅ | `POST /circulars` |
| Parsing/Chunking بندها | ✅ | `ExtractClauses`, `ParseCircular` |
| ابرداده نوع/واحد/تاریخ/موضوع | ✅ | مدل `Circular` |
| Embedding / Semantic Search | ✅ | neural `gemini-embedding-001` + `SearchClauses`؛ fallback hashed |
| مقایسه بندبه‌بند | ✅ | `Analyze` / `ScanArchive` |
| تعارض کامل | ✅ | `full_contradiction` |
| تعارض جزئی | ✅ | `partial_contradiction` |
| هم‌پوشانی بدون تعارض | ✅ | `overlap_without_conflict` |
| نسخ (Supersede) | ✅ | `supersession` + ارجاع بند/شناسه |
| تقدم نظارتی بر داخلی | ✅ | `resolve` / `HierarchyLevel` |
| تقدم تاریخ صدور | ✅ | `issueDateAfter` (جلالی) |
| هم‌سطح هم‌تاریخ → ابهام | ✅ | `needs_review` |
| evidence متن دو بند | ✅ | `evidence` + `GET .../clauses/{n}` |
| اسکن دوره‌ای آرشیو | ✅ | `POST /scans/archive` + scheduler |
| UI حقوقی/تطبیق | ✅ | React RTL فارسی |
| Dockerfile | ✅ | `Dockerfile` + compose |
| README + روش تشخیص | ✅ | README + TECHNICAL_REPORT |
| Precision / Recall / F1 | ✅ | `cmd/evalconflict` → F1=1.0 (۹ TP) |
| گزارش کوتاه روش | ✅ | TECHNICAL_REPORT / FINAL_REPORT_FA |
| Bonus خلاصه ساده LLM | ✅ | `plain_language_summary` / `legal_summary` |
| ویدیوی دمو (≥۲ سناریو) | ⏭ صرف‌نظر | درخواست صریح کاربر |

## سناریوهای نمونه PDF

| سناریو | پوشش |
|---|---|
| سقف داخلی جدیدتر vs قدیمی | `pdf_newer_ceiling_change` |
| داخلی vs نظارتی | `pdf_supervisory_over_internal` |
| دو واحد، شرایط متفاوت دسته‌چک | `pdf_same_date_different_units_needs_review` |
| اصلاح جزئی فقط بخشی از بخشنامه | `pdf_partial_supersession_amendment` |
| تعارض پنهان آرشیو | `challenge_archive_designed_high_risk_conflicts` (BX-1005×1007/1002/1003) |

## یافته‌های اصلاح‌شده

1. پورت VPS `18080:8080` (قبلاً اشتباه به 8501)
2. volume state نوشتنی + Postgres
3. استخراج مبلغ ریال/تومان/میلیون/میلیارد
4. اصلاح «بند N بخشنامه X» فقط بند N
5. آستانه ماه فقط روی subject یکسان (بدون FP بین محصولات مختلف)
6. embedding عصبی روی gateway کاربر؛ classify جفت‌بند قطعی پیش‌فرض
7. alias OpenAPI `risk.level` / `risk.score`
8. eligibility ۵۴/۵۴ + cold-start ۵۸

## محدودیت‌های صادقانه (هم‌راستا در همه docs)

- **ویدیو:** انجام نشده (waived).
- **GT تعارض کمیته:** در بسته چالش منتشر نشده؛ GT داخلی بر اساس PDF + بخشنامه‌های BX.
- **Classify جفت‌بند:** پیش‌فرض قطعی (`LLM_CLASSIFY=0`)؛ chat LLM برای خلاصه/deep-review.
- **Embedding chat model:** `ag/gemini-3.6-flash-high` embeddings ندارد؛ از `gemini-embedding-001` استفاده می‌شود.

## دامنه دوم (Eligibility)

بسته چالش eligibility را هم الزام کرده → `ELIGIBILITY_ASSISTANT_FA.md` و `POST /assist`.
