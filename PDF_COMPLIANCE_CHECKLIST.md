# چک‌لیست انطباق با PDF (Circular Conflict)

متن PDF به `EligibilityAssistant&IntelligentBankingOffer.md` تبدیل شده و خط‌به‌خط
با پیاده‌سازی Go تطبیق داده شده است.

| الزام PDF | وضعیت | محل پیاده‌سازی |
|---|---|---|
| ثبت بخشنامه جدید | ✅ | `POST /circulars` |
| Parsing/Chunking بندها | ✅ | `ExtractClauses`, `ParseCircular` |
| ابرداده نوع/واحد/تاریخ/موضوع | ✅ | مدل `Circular` |
| Semantic/Embedding search | ✅ | `BuildEmbedding` + `SearchClauses` + `isCandidate` |
| مقایسه بندبه‌بند | ✅ | `Analyze` / `ScanArchive` |
| تعارض کامل | ✅ | `full_contradiction` |
| تعارض جزئی | ✅ | `partial_contradiction` (سقف/آستانه/استثنا) |
| هم‌پوشانی بدون تعارض | ✅ | `overlap_without_conflict` |
| نسخ (Supersede) | ✅ | `supersession` + ارجاع صریح `BX-...` |
| تقدم بالادستی/نظارتی | ✅ | `resolve` با `HierarchyLevel` |
| تقدم تاریخ صدور | ✅ | `issueDateAfter` / Jalali |
| ابهام هم‌سطح هم‌تاریخ | ✅ | `needs_review` |
| evidence دقیق متن دو بند | ✅ | `evidence_json` + `GET .../clauses/{n}` |
| اسکن آرشیو برای تعارض پنهان | ✅ | `POST /scans/archive` |
| UI حقوقی/تطبیق | ✅ | UI فارسی RTL |
| مستندسازی و کد | ✅ | README + docs فارسی |
| تست ورودی/خروجی | ✅ | parser/analyzer/server/eval |
| Bonus: خلاصه ساده LLM برای حقوقی | ✅ | `plain_language_summary` |
| Precision/Recall/F1 | ✅ | `cmd/evalconflict` روی `eval/ground_truth.json` |
| Docker | ✅ | `Dockerfile` + `compose.go-conflict.yml` |
| پایداری Postgres | ✅ | `DATABASE_URL` + JSONB state |
| اسکن زمان‌بندی‌شده | ✅ | `SCAN_INTERVAL_SECONDS` |

## یافته‌های منطقی که اصلاح شد

1. **پورت اشتباه روی VPS**: کانتینر قبلی `18080->8501` و HTML اپ دیگر سرو می‌کرد؛ compose به `18080:8080` اصلاح شد.
2. **Volume state فقط‌خواندنی** بود؛ برای persistence نوشتنی شد.
3. **DATA_DIR داخل image** بیک‌این شد تا بدون mount چالش هم سرویس بالا بیاید.
4. **استثنای محصول پایه** (سپرده/قرض‌الحسنه) دیگر به‌اشتباه با ممنوعیت اعتباری بالادستی هم‌خانواده نمی‌شود.
5. **alias قرارداد OpenAPI** برای `risk.level/score` اضافه شد.
6. **استخراج قواعد از متن بخشنامه** در `/eligibility/rules` پیاده شد.
7. **استخراج مبلغ** (ریال/تومان/میلیون/میلیارد/واژه‌عدد) برای تعارض سقف.
8. **اصلاح جزئی «بند N بخشنامه X»** فقط همان بند N را نسخ می‌کند (`referencedClauseNumber` + `amendmentTargetsOtherClause`) تا FP با بندهای هم‌بخشنامه ایجاد نشود.
9. **طبقه‌بندی پیش‌فرض قطعی** (`LLM_CLASSIFY=0`)؛ LLM اختیاری برای borderline/خلاصه.

## ارزیابی تعارض (۸ سناریوی PDF)

`go run ./cmd/evalconflict eval/ground_truth.json` → Precision/Recall/F1 = **1.0** (۷ TP، ۰ FP/FN).

| سناریو | انتظار |
|---|---|
| نظارتی > داخلی | full_contradiction، win=supervisory |
| سقف جدیدتر | partial_contradiction |
| آستانه ماه جدیدتر | partial_contradiction |
| هم‌سطح هم‌تاریخ | needs_review |
| اصلاح جزئی بند ۲ | supersession فقط روی #2 |
| لغو صریح BX | supersession |
| تعارض پنهان آرشیو BX-1005/1007 | full_contradiction |
| هم‌پوشانی سازگار | بدون تعارض |

## محدودیت‌های صادقانه

- **Embedding عصبی پیش‌فرض روشن** وقتی `OPENAI_BASE_URL`+`OPENAI_API_KEY` هست: مدل `gemini-embedding-001` (۳۰۷۲ بعدی) از همان endpoint سازگار با OpenAI. Fallback محلی hashed فقط اگر API خاموش/خراب باشد (`OPENAI_EMBEDDING=0`).
- مدل chat (`ag/gemini-3.6-flash-high`) برای embeddings پشتیبانی نمی‌شود؛ جدا از مدل embedding است.
- طبقه‌بندی تعارض جفت‌به‌جفت پیش‌فرض **قطعی** است (`LLM_CLASSIFY=0`) تا Analyze هنگ نکند؛ chat LLM برای خلاصه/deep-review.
- ویدیوی دمو طبق درخواست کاربر حذف/صرف‌نظر شده است.
- GT تعارض کمیته رسمی ندارد؛ GT داخلی بر اساس سناریوهای PDF ساخته شده.

## دامنه دوم PDF/بسته (Eligibility)

با اینکه عنوان PDF اصلی «تعارض بخشنامه» است، بسته همراه چالش Eligibility را هم
الزام کرده. هر دو در همین سرویس پوشش داده شده‌اند؛ جزئیات Eligibility در
`ELIGIBILITY_ASSISTANT_FA.md`.
