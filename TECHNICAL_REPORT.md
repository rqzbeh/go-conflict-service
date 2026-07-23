# گزارش فنی سرویس Go

## معماری

```
UI (React, RTL, فارسی)
        │
        ▼
   conflictd (Go HTTP)
   ├── Conflict Engine
   │    ├── Parser / Feature Extraction
   │    ├── Embedding سبک + Semantic Search
   │    ├── Deterministic Classifier (+ optional LLM)
   │    ├── Resolver (hierarchy + jalali date)
   │    └── Store (memory / Postgres JSONB)
   └── Eligibility Assistant
        ├── Profile / Intake / Cold-start
        ├── Matching Engine (deterministic)
        ├── Rule extraction from circular text
        ├── Offer / Explainer / Legal summary
        └── Mock Identity/Financial/RBCI APIs
```

## تصمیم‌های طراحی

- تصمیم‌های عددی و حقوقی قطعی‌اند. LLM فقط برای خلاصه‌سازی/بازبینی کمکی است و با guardrail نمی‌تواند تعارض قطعی را خنثی کند.
- Embedding بدون سرویس خارجی برای بازیابی اولیه.
- Eligibility ground truth رسمی (`eval/ground_truth.json` بسته چالش) معیار صحت Matching Engine است.
- Conflict ground truth داخل `go-conflict-service/eval/ground_truth.json` برای Precision/Recall/F1.

## پایداری داده

- بدون `DATABASE_URL`: حافظه + اختیاری `STATE_PATH`
- با `DATABASE_URL`: Postgres و ذخیره وضعیت در JSONB
- seed بخشنامه‌ها از `DATA_DIR/circulars*` در استارتاپ

## امنیت عملیاتی

- کلید LLM فقط از env
- JSON decoder با `DisallowUnknownFields`
- محدودیت اندازه body
- عدم افشای API key در `/health`

## اجرا و استقرار

- Compose: `compose.go-conflict.yml`
- سرویس: `go-conflict` روی `:8080` و publish `18080`
- مسیر VPS: `/opt/circular-conflict`
- Health: `GET /health`

## نتایج تست (محلی)

- `go test ./...` ✅ (۵۷)
- `evalconflict` روی ۸ سناریوی PDF: F1 = **1.0** (۷ TP / ۰ FP / ۰ FN) ✅
- ground truth ۵۴ زوج eligibility ✅
- cold-start BX-1008 student/small_credit/age28 → score **۵۸** medium ✅

## رفع باگ اخیر

- اصلاح «بند N بخشنامه X» دیگر با بندهای غیرهدف همان بخشنامه `partial_contradiction` مبلغی تولید نمی‌کند.
- `isCandidate` / `thresholdConflict` / `sameScope` از `amendmentTargetsOtherClause` عبور می‌کنند.
