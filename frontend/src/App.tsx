import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  BrainCircuit,
  CheckCircle2,
  DatabaseZap,
  Eye,
  FileSearch,
  Files,
  HandCoins,
  Pencil,
  RefreshCw,
  Search,
  ShieldCheck,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import {
  api,
  cached,
  type AssistResponse,
  type Circular,
  type CircularRequest,
  type Relationship,
  type Report,
  type SelfDeclared,
} from "./api";

type View = "assistant" | "relationships" | "circulars" | "report" | "search";
type Busy = "idle" | "archive" | "submit" | "relationships" | "circulars" | "search" | "assist" | string;
type ReviewFilter = "open" | "all";

const relLabel: Record<string, string> = {
  full_contradiction: "مغایرت کامل",
  partial_contradiction: "مغایرت جزئی",
  overlap_without_conflict: "هم‌پوشانی سازگار",
  supersession: "نسخ یا جایگزینی",
  neutral: "خنثی",
};

const severityLabel: Record<string, string> = {
  critical: "بحرانی",
  high: "زیاد",
  medium: "متوسط",
  low: "کم",
  none: "بدون ریسک",
};

const emptyForm: CircularRequest = {
  id: "",
  title: "بخشنامه آزمایشی تغییر سقف دسته‌چک",
  text: "بند 1) دسته‌چک به مشتریانی قابل ارائه است که حداقل ۳ ماه از تاریخ افتتاح حساب آن‌ها گذشته باشد.\nبند 2) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند در صورت معرفی ضامن معتبر.",
  issuer_unit: "اداره اعتبارات و تسهیلات",
  circular_type: "internal",
  issue_date: "1404/01/10",
  topic: "دسته‌چک",
};

const emptySelfDeclared: SelfDeclared = {
  name: "مشتری جدید",
  age: 28,
  job_category: "student",
  purpose_category: "small_credit",
  declared_monthly_income: 12000000,
  requested_amount: 80000000,
};

const scenarios = [
  { id: "12345678", label: "خانه‌دار ۴۰ ساله" },
  { id: "23456789", label: "مدیر پردرآمد" },
  { id: "34567890", label: "کارمند" },
  { id: "90123456", label: "چک برگشتی/عدم پرداخت" },
  { id: "99999991", label: "مشتری جدید" },
];

function evidenceText(rel: Relationship, key: "source_text" | "target_text") {
  const value = rel.evidence_json?.[key];
  return typeof value === "string" ? value : "";
}

function relationClass(type: string) {
  if (type === "full_contradiction") return "danger";
  if (type === "partial_contradiction" || type === "supersession") return "warning";
  if (type === "overlap_without_conflict") return "success";
  return "muted";
}

function App() {
  const [form, setForm] = useState(emptyForm);
  const [view, setView] = useState<View>("relationships");
  const [busy, setBusy] = useState<Busy>("idle");
  const [notice, setNotice] = useState("در حال بارگذاری داده‌ها...");
  const [stale, setStale] = useState(false);
  const [health, setHealth] = useState<{ circulars: number; llm: { enabled: boolean; model: string } } | null>(null);
  const [circulars, setCirculars] = useState<Circular[]>([]);
  const [relationships, setRelationships] = useState<Relationship[]>([]);
  const [showCompatible, setShowCompatible] = useState(false);
  const [reviewFilter, setReviewFilter] = useState<ReviewFilter>("open");
  const [report, setReport] = useState<Report | null>(null);
  const [selectedCircular, setSelectedCircular] = useState<Circular | null>(null);
  const [editingCircularID, setEditingCircularID] = useState<string | null>(null);
  const [nationalID, setNationalID] = useState("12345678");
  const [selfDeclared, setSelfDeclared] = useState<SelfDeclared>(emptySelfDeclared);
  const [assistResult, setAssistResult] = useState<AssistResponse | null>(null);
  const [query, setQuery] = useState("ریسک زیاد دسته چک");
  const [searchItems, setSearchItems] = useState<{ score: number; clause: { id: string; original_text: string } }[]>([]);

  const visibleReport = useMemo(() => {
    if (!report) return null;
    return { ...report, relationships: filterRelationships(report.relationships, showCompatible, reviewFilter) };
  }, [report, showCompatible, reviewFilter]);

  const visibleRelationships = useMemo(
    () => filterRelationships(relationships, showCompatible, reviewFilter),
    [relationships, showCompatible, reviewFilter],
  );

  const stats = useMemo(() => {
    const base = view === "report" && visibleReport ? visibleReport.relationships : visibleRelationships;
    const critical = base.filter((r) => r.relationship_type === "full_contradiction").length;
    const pending = base.filter((r) => !r.review_status).length;
    const ai = base.filter((r) => r.deep_review?.generated_by_llm).length;
    return { critical, pending, ai };
  }, [relationships, view, visibleRelationships, visibleReport]);

  useEffect(() => {
    void bootstrap();
  }, []);

  async function bootstrap() {
    setBusy("relationships");
    try {
      await Promise.all([
        api.health().then(setHealth),
        cached("cache:circulars", api.circulars, (data, isStale) => {
          setCirculars(data.items);
          setStale(isStale);
        }),
        loadRelationships(showCompatible),
      ]);
      setNotice("داده‌ها آماده است.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در دریافت داده‌ها");
    } finally {
      setBusy("idle");
    }
  }

  async function loadRelationships(includeCompatible = showCompatible) {
    setBusy("relationships");
    const key = `cache:relationships:${includeCompatible}`;
    try {
      await cached(key, () => api.relationships(!includeCompatible), (data, isStale) => {
        setRelationships(data.items);
        setStale(isStale);
      });
      setView("relationships");
      setNotice(includeCompatible ? "همه روابط بارگذاری شد." : "فقط موارد قابل اقدام نمایش داده می‌شود.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در دریافت فهرست روابط");
    } finally {
      setBusy("idle");
    }
  }

  async function loadCirculars() {
    setBusy("circulars");
    try {
      await cached("cache:circulars", api.circulars, (data, isStale) => {
        setCirculars(data.items);
        setStale(isStale);
      });
      setView("circulars");
      setNotice("فهرست بخشنامه‌ها آماده است.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در دریافت بخشنامه‌ها");
    } finally {
      setBusy("idle");
    }
  }

  async function submitCircular() {
    setBusy("submit");
    try {
      let circularID: string;
      let updatedCircular: Circular | null = null;
      if (editingCircularID) {
        updatedCircular = await api.updateCircular(editingCircularID, form);
        setCirculars((items) => upsertCircular(items, updatedCircular));
        setSelectedCircular(updatedCircular);
        setEditingCircularID(null);
        setView("circulars");
        setStale(false);
        setNotice("بخشنامه ذخیره شد. برای تحلیل دوباره از دکمه تحلیل استفاده کنید.");
        return;
      } else {
        const created = await api.createCircular(form);
        circularID = created.circular_id;
      }
      const analyzed = await api.analyze(circularID);
      setReport(analyzed);
      setRelationships(analyzed.relationships);
      setEditingCircularID(null);
      setSelectedCircular(null);
      setView("report");
      setStale(false);
      setNotice(editingCircularID ? "بخشنامه ذخیره و دوباره تحلیل شد." : "بخشنامه ثبت و تحلیل شد.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در ثبت بخشنامه");
    } finally {
      setBusy("idle");
    }
  }

  async function scanArchive() {
    setBusy("archive");
    try {
      const scanned = await api.archiveScan();
      setReport(scanned);
      setRelationships(scanned.relationships);
      setView("report");
      setStale(false);
      setNotice("اسکن آرشیو کامل شد.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در اسکن آرشیو");
    } finally {
      setBusy("idle");
    }
  }

  async function analyzeCircular(id: string) {
    setBusy(`analyze:${id}`);
    try {
      const analyzed = await api.analyze(id);
      setReport(analyzed);
      setRelationships(analyzed.relationships);
      setView("report");
      setNotice("تحلیل بخشنامه کامل شد.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در تحلیل بخشنامه");
    } finally {
      setBusy("idle");
    }
  }

  async function runDeepReview(rel: Relationship) {
    setBusy(`deep:${rel.id}`);
    try {
      const updated = await api.deepReview(rel.id);
      setRelationships((items) => items.map((item) => (item.id === updated.id ? updated : item)));
      setReport((current) =>
        current
          ? { ...current, relationships: current.relationships.map((item) => (item.id === updated.id ? updated : item)) }
          : current,
      );
      setNotice(updated.deep_review?.generated_by_llm ? "بررسی عمیق با AI کامل شد." : "بررسی عمیق قاعده‌محور ثبت شد.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در بررسی عمیق");
    } finally {
      setBusy("idle");
    }
  }

  async function viewCircular(circular: Circular) {
    setBusy(`view:${circular.id}`);
    try {
      const fresh = await api.circular(circular.id);
      setSelectedCircular(fresh);
      setView("circulars");
      setNotice("متن بخشنامه آماده است.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در دریافت بخشنامه");
    } finally {
      setBusy("idle");
    }
  }

  function editCircular(circular: Circular) {
    setEditingCircularID(circular.id);
    setForm(circularToForm(circular));
    setSelectedCircular(circular);
    setNotice("بخشنامه برای ویرایش در فرم سمت راست بارگذاری شد.");
  }

  async function deleteCircular(id: string) {
    if (!window.confirm("این بخشنامه و روابط وابسته حذف شوند؟")) return;
    setBusy(`delete:${id}`);
    try {
      await api.deleteCircular(id);
      setCirculars((items) => items.filter((item) => item.id !== id));
      setRelationships((items) => items.filter((rel) => !relationshipIncludesCircular(rel, id)));
      setReport((current) =>
        current ? { ...current, relationships: current.relationships.filter((rel) => !relationshipIncludesCircular(rel, id)) } : current,
      );
      if (selectedCircular?.id === id) setSelectedCircular(null);
      if (editingCircularID === id) {
        setEditingCircularID(null);
        setForm(emptyForm);
      }
      setNotice("بخشنامه حذف شد.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در حذف بخشنامه");
    } finally {
      setBusy("idle");
    }
  }

  async function review(rel: Relationship, status: "accepted" | "needs_followup") {
    setBusy(`review:${rel.id}`);
    try {
      const updated = await api.review(rel.id, status, "");
      setRelationships((items) => items.map((item) => (item.id === updated.id ? updated : item)));
      setReport((current) =>
        current
          ? { ...current, relationships: current.relationships.map((item) => (item.id === updated.id ? updated : item)) }
          : current,
      );
      setNotice(
        status === "needs_followup"
          ? "مورد برای پیگیری باز ماند."
          : "مورد تأیید شد و در حالت «فقط حل‌نشده‌ها» پنهان می‌شود.",
      );
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در ثبت بازبینی");
    } finally {
      setBusy("idle");
    }
  }

  async function search() {
    if (!query.trim()) return;
    setBusy("search");
    try {
      const data = await api.search(query);
      setSearchItems(data.items);
      setView("search");
      setNotice("نتایج جست‌وجو آماده است.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در جست‌وجو");
    } finally {
      setBusy("idle");
    }
  }

  async function runAssist(id = nationalID, includeSelfDeclared = false) {
    setBusy("assist");
    try {
      const data = await api.assist(id, includeSelfDeclared ? selfDeclared : undefined);
      setAssistResult(data);
      setNationalID(id);
      setView("assistant");
      setNotice(data.intake ? data.intake.question : "تحلیل اهلیت و آفر آماده است.");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "خطا در تحلیل اهلیت");
    } finally {
      setBusy("idle");
    }
  }

  function update<K extends keyof CircularRequest>(key: K, value: CircularRequest[K]) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  return (
    <main className="app-shell">
      <aside className="sidebar" aria-label="ثبت و عملیات">
        <div className="brand">
          <div className="brand-mark" aria-hidden="true">
            <ShieldCheck size={26} />
          </div>
          <div>
            <h1>مغایرت بخشنامه‌ها</h1>
            <p>تحلیل تطبیقی بندها و تعیین اقدام حقوقی</p>
          </div>
        </div>

        <div className="status-row" aria-live="polite">
          <span className={health?.llm.enabled ? "dot ok" : "dot warn"} />
          <span>{health?.llm.enabled ? `LLM فعال: ${health.llm.model}` : "LLM غیرفعال"}</span>
        </div>

        <form className="form" onSubmit={(event) => event.preventDefault()}>
          <label>
            شناسه
            <input
              data-field="id"
              value={form.id}
              onChange={(event) => update("id", event.target.value)}
              disabled={Boolean(editingCircularID)}
            />
          </label>
          <label>
            عنوان
            <input data-field="title" value={form.title} onChange={(event) => update("title", event.target.value)} required />
          </label>
          <div className="field-grid">
            <label>
              نوع
              <select
                data-field="circular_type"
                value={form.circular_type}
                onChange={(event) => update("circular_type", event.target.value)}
              >
                <option value="internal">داخلی</option>
                <option value="supervisory">نظارتی/بالادستی</option>
              </select>
            </label>
            <label>
              تاریخ صدور
              <input
                data-field="issue_date"
                value={form.issue_date}
                onChange={(event) => update("issue_date", event.target.value)}
                required
              />
            </label>
          </div>
          <label>
            واحد صادرکننده
            <input data-field="issuer_unit" value={form.issuer_unit} onChange={(event) => update("issuer_unit", event.target.value)} />
          </label>
          <label>
            موضوع
            <input data-field="topic" value={form.topic} onChange={(event) => update("topic", event.target.value)} />
          </label>
          <label>
            متن بخشنامه
            <textarea data-field="text" value={form.text} onChange={(event) => update("text", event.target.value)} required />
          </label>
          <button className="primary" type="button" onClick={submitCircular} disabled={busy === "submit"}>
            <FileSearch size={18} />
            {busy === "submit" ? "در حال پردازش..." : editingCircularID ? "ذخیره تغییرات" : "ثبت و تحلیل"}
          </button>
          {editingCircularID && (
            <button
              className="ghost"
              type="button"
              onClick={() => {
                setEditingCircularID(null);
                setForm(emptyForm);
                setNotice("ویرایش لغو شد.");
              }}
            >
              <X size={18} />
              لغو ویرایش
            </button>
          )}
        </form>
      </aside>

      <section className="workspace" aria-label="داشبورد مغایرت‌ها">
        <header className="topbar">
          <div>
            <p className="eyebrow">داشبورد عملیاتی تطبیق</p>
            <h2>{viewTitle(view)}</h2>
          </div>
          <div className="actions">
            <button type="button" onClick={scanArchive} disabled={busy === "archive"}>
              <DatabaseZap size={18} />
              {busy === "archive" ? "اسکن..." : "اسکن آرشیو"}
            </button>
            <button type="button" onClick={() => setView("assistant")} disabled={busy === "assist"}>
              <HandCoins size={18} />
              دستیار اهلیت
            </button>
            <button type="button" onClick={() => void loadRelationships(showCompatible)} disabled={busy === "relationships"}>
              <RefreshCw size={18} />
              روابط
            </button>
            <button type="button" onClick={loadCirculars} disabled={busy === "circulars"}>
              <Files size={18} />
              بخشنامه‌ها
            </button>
          </div>
        </header>

        <div className="notice" aria-live="polite">
          <span>{notice}</span>
          {stale && <strong>نمایش cache، در حال به‌روزرسانی</strong>}
        </div>

        <section className="metrics" aria-label="شاخص‌ها">
          <Metric label="بخشنامه" value={health?.circulars ?? circulars.length} tone="ink" />
          <Metric label="مغایرت کامل" value={stats.critical} tone="danger" />
          <Metric label="در انتظار بازبینی" value={stats.pending} tone="warning" />
          <Metric label="بررسی AI" value={stats.ai} tone="success" />
        </section>

        <div className="control-band">
          <div className="segmented" aria-label="فیلتر وضعیت بازبینی">
            <button
              type="button"
              className={reviewFilter === "open" ? "active" : ""}
              aria-pressed={reviewFilter === "open"}
              onClick={() => setReviewFilter("open")}
            >
              فقط حل‌نشده‌ها
            </button>
            <button
              type="button"
              className={reviewFilter === "all" ? "active" : ""}
              aria-pressed={reviewFilter === "all"}
              onClick={() => setReviewFilter("all")}
            >
              همه موارد
            </button>
          </div>
          <label className="switch">
            <input
              type="checkbox"
              checked={showCompatible}
              onChange={(event) => {
                setShowCompatible(event.target.checked);
                if (view === "relationships") void loadRelationships(event.target.checked);
              }}
            />
            نمایش هم‌پوشانی‌های سازگار
          </label>
          <div className="searchbar">
            <Search size={18} />
            <label className="sr-only" htmlFor="semantic-search">
              جست‌وجوی معنایی
            </label>
            <input
              id="semantic-search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void search();
              }}
            />
            <button type="button" onClick={search} disabled={busy === "search"}>
              جست‌وجو
            </button>
          </div>
        </div>

        {view === "relationships" && (
          <RelationshipList items={visibleRelationships} busy={busy} onDeepReview={runDeepReview} onReview={review} />
        )}
        {view === "assistant" && (
          <AssistantView
            nationalID={nationalID}
            selfDeclared={selfDeclared}
            result={assistResult}
            busy={busy}
            onNationalID={setNationalID}
            onSelfDeclared={setSelfDeclared}
            onRun={runAssist}
          />
        )}
        {view === "report" && visibleReport && (
          <ReportView report={visibleReport} busy={busy} onDeepReview={runDeepReview} onReview={review} />
        )}
        {view === "circulars" && (
          <>
            {selectedCircular && <CircularDetail circular={selectedCircular} onClose={() => setSelectedCircular(null)} />}
            <CircularList
              items={circulars}
              busy={busy}
              onAnalyze={analyzeCircular}
              onView={viewCircular}
              onEdit={editCircular}
              onDelete={deleteCircular}
            />
          </>
        )}
        {view === "search" && <SearchResults items={searchItems} />}
      </section>
    </main>
  );
}

function filterRelationships(items: Relationship[], includeCompatible: boolean, reviewFilter: ReviewFilter) {
  return items.filter((rel) => {
    if (!includeCompatible && relationClass(rel.relationship_type) === "success") return false;
    if (reviewFilter === "open" && !isUnresolvedReview(rel)) return false;
    return true;
  });
}

function isUnresolvedReview(rel: Relationship) {
  return rel.review_status !== "accepted";
}

function relationshipIncludesCircular(rel: Relationship, circularID: string) {
  return rel.source_clause_id.startsWith(`${circularID}#`) || rel.target_clause_id.startsWith(`${circularID}#`);
}

function circularToForm(circular: Circular): CircularRequest {
  return {
    id: circular.id,
    title: circular.title,
    text: circular.raw_text,
    issuer_unit: circular.issuer_unit,
    circular_type: circular.circular_type,
    issue_date: circular.issue_date,
    topic: circular.topic,
  };
}

function upsertCircular(items: Circular[], circular: Circular | null) {
  if (!circular) return items;
  const exists = items.some((item) => item.id === circular.id);
  return exists ? items.map((item) => (item.id === circular.id ? circular : item)) : [...items, circular];
}

function viewTitle(view: View) {
  if (view === "assistant") return "دستیار اهلیت و آفر";
  if (view === "circulars") return "فهرست بخشنامه‌ها";
  if (view === "report") return "گزارش تحلیل";
  if (view === "search") return "نتایج جست‌وجو";
  return "فهرست مغایرت‌ها";
}

function Metric({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className={`metric ${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function AssistantView({
  nationalID,
  selfDeclared,
  result,
  busy,
  onNationalID,
  onSelfDeclared,
  onRun,
}: {
  nationalID: string;
  selfDeclared: SelfDeclared;
  result: AssistResponse | null;
  busy: Busy;
  onNationalID: (id: string) => void;
  onSelfDeclared: (data: SelfDeclared) => void;
  onRun: (id?: string, includeSelfDeclared?: boolean) => void;
}) {
  const needSelf = result?.intake || nationalID === "99999991";
  return (
    <div className="assistant-panel">
      <section className="assistant-card">
        <div className="assistant-form">
          <label>
            کد ملی مشتری
            <input value={nationalID} onChange={(event) => onNationalID(event.target.value)} />
          </label>
          <button type="button" onClick={() => onRun(nationalID, false)} disabled={busy === "assist"}>
            <HandCoins size={18} />
            {busy === "assist" ? "در حال تحلیل..." : "شروع دستیار"}
          </button>
        </div>
        <div className="scenario-row">
          {scenarios.map((scenario) => (
            <button type="button" key={scenario.id} onClick={() => onRun(scenario.id, false)}>
              {scenario.label}
            </button>
          ))}
        </div>
      </section>

      {needSelf && (
        <section className="assistant-card">
          <h3>اطلاعات خوداظهاری مشتری جدید</h3>
          {result?.intake && <p className="rationale">پرسش بعدی: {result.intake.question}</p>}
          <div className="intake-grid">
            <label>
              نام
              <input value={selfDeclared.name || ""} onChange={(event) => onSelfDeclared({ ...selfDeclared, name: event.target.value })} />
            </label>
            <label>
              سن
              <input
                type="number"
                value={selfDeclared.age ?? ""}
                onChange={(event) => onSelfDeclared({ ...selfDeclared, age: Number(event.target.value) })}
              />
            </label>
            <label>
              شغل
              <select
                value={selfDeclared.job_category}
                onChange={(event) => onSelfDeclared({ ...selfDeclared, job_category: event.target.value })}
              >
                <option value="government_or_reputable">کارمند دولت/شرکت معتبر</option>
                <option value="retired_with_pension">بازنشسته با حقوق</option>
                <option value="private_sector">کارمند بخش خصوصی</option>
                <option value="student">دانشجو</option>
                <option value="homemaker_or_no_formal_job">خانه‌دار/بدون شغل رسمی</option>
                <option value="self_employed">خوداشتغال</option>
                <option value="unemployed">بیکار</option>
              </select>
            </label>
            <label>
              هدف مراجعه
              <select
                value={selfDeclared.purpose_category}
                onChange={(event) => onSelfDeclared({ ...selfDeclared, purpose_category: event.target.value })}
              >
                <option value="account_or_deposit_only">افتتاح حساب/سپرده</option>
                <option value="small_credit">اعتبار کوچک</option>
                <option value="large_credit">اعتبار بزرگ</option>
              </select>
            </label>
            <label>
              درآمد ماهانه
              <input
                type="number"
                value={selfDeclared.declared_monthly_income ?? ""}
                onChange={(event) => onSelfDeclared({ ...selfDeclared, declared_monthly_income: Number(event.target.value) })}
              />
            </label>
            <label>
              مبلغ درخواستی
              <input
                type="number"
                value={selfDeclared.requested_amount ?? ""}
                onChange={(event) => onSelfDeclared({ ...selfDeclared, requested_amount: Number(event.target.value) })}
              />
            </label>
          </div>
          <button className="primary inline-primary" type="button" onClick={() => onRun(nationalID, true)} disabled={busy === "assist"}>
            تکمیل ارزیابی Cold-start
          </button>
        </section>
      )}

      {result && result.eligibility.length > 0 && (
        <>
          <section className="assistant-card">
            <div className="risk-strip">
              <strong>{result.customer_status === "existing" ? "مشتری موجود" : "مشتری جدید"}</strong>
              <span>ریسک: {result.risk?.risk_level_fa || result.risk?.risk_level}</span>
              <span>امتیاز: {result.risk?.risk_score}</span>
              <span>{result.risk?.mode === "cold_start" ? "Cold-start" : "Lookup"}</span>
            </div>
            <p className="conversation">{result.conversation}</p>
            {result.payment_impact && <p className="payment-impact">{result.payment_impact}</p>}
          </section>

          <section className="assistant-card">
            <h3>آفرهای پیشنهادی</h3>
            <div className="offer-grid">
              {result.offers?.map((offer) => (
                <article className="offer" key={offer.product_id}>
                  <span>#{offer.rank}</span>
                  <h4>{offer.product_name_fa}</h4>
                  <p>{offer.why}</p>
                </article>
              ))}
            </div>
          </section>

          <section className="assistant-card">
            <h3>خلاصه حقوقی/تطبیق</h3>
            <p className="legal-summary">{result.legal_summary}</p>
            <span className="pill">{result.legal_summary_generated_by_llm ? "تولید شده با LLM" : "خلاصه قاعده‌محور"}</span>
          </section>

          <div className="eligibility-grid">
            {result.eligibility.map((item) => (
              <article className={`eligibility ${item.decision}`} key={item.product_id}>
                <span className="pill">{decisionLabel(item.decision)}</span>
                <h3>{item.product_name_fa}</h3>
                <p>{item.reason}</p>
                {(item.gap || []).length > 0 && (
                  <dl>
                    {(item.gap || []).map((gap) => (
                      <div key={`${gap.metric}-${String(gap.required)}`}>
                        <dt>{gap.metric}</dt>
                        <dd>
                          فعلی: {String(gap.current)} / لازم: {String(gap.required)} {gap.unit || ""}
                        </dd>
                      </div>
                    ))}
                  </dl>
                )}
                <div className="rel-meta">
                  {(item.evidence || []).map((ev) => (
                    <span key={`${ev.circular_id}-${ev.clause}`}>
                      {ev.circular_id}#{ev.clause}
                    </span>
                  ))}
                </div>
              </article>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function decisionLabel(decision: string) {
  if (decision === "eligible") return "مجاز";
  if (decision === "conditional") return "مشروط";
  return "غیرمجاز";
}

function RelationshipList({
  items,
  busy,
  onDeepReview,
  onReview,
}: {
  items: Relationship[];
  busy: Busy;
  onDeepReview: (rel: Relationship) => void;
  onReview: (rel: Relationship, status: "accepted" | "needs_followup") => void;
}) {
  if (items.length === 0) return <EmptyState text="مورد قابل اقدامی ثبت نشده است." />;
  return (
    <div className="relationship-list">
      {items.map((rel) => (
        <article className={`relationship ${relationClass(rel.relationship_type)}`} key={rel.id} data-relationship-id={rel.id}>
          <div className="rel-head">
            <div>
              <span className="pill">{relLabel[rel.relationship_type] ?? rel.relationship_type}</span>
              <h3>{rel.source_clause_id} ⇄ {rel.target_clause_id}</h3>
            </div>
            <strong className="confidence">{Math.round(rel.confidence * 100)}٪</strong>
          </div>
          <p className="rationale">{rel.rationale}</p>
          <div className="evidence">
            <blockquote>{evidenceText(rel, "source_text")}</blockquote>
            <blockquote>{evidenceText(rel, "target_text")}</blockquote>
          </div>
          <div className="rel-meta">
            <span>وضعیت: {rel.resolver_status}</span>
            <span>بند معتبر: {rel.winning_clause_id || "نیازمند بررسی"}</span>
            <span>اقدام: {rel.required_action}</span>
            <span data-review-status={rel.review_status || "none"}>بازبینی: {reviewLabel(rel.review_status)}</span>
          </div>
          {rel.deep_review && <DeepReviewBox rel={rel} />}
          <div className="rel-actions">
            <button type="button" onClick={() => onDeepReview(rel)} disabled={busy === `deep:${rel.id}`}>
              <BrainCircuit size={18} />
              {busy === `deep:${rel.id}` ? "بررسی..." : "بررسی عمیق با AI"}
            </button>
            <button
              type="button"
              data-review-action="accepted"
              onClick={() => onReview(rel, "accepted")}
              disabled={busy === `review:${rel.id}`}
            >
              <CheckCircle2 size={18} />
              تأیید
            </button>
            <button
              type="button"
              data-review-action="needs_followup"
              onClick={() => onReview(rel, "needs_followup")}
              disabled={busy === `review:${rel.id}`}
            >
              <AlertTriangle size={18} />
              پیگیری
            </button>
          </div>
        </article>
      ))}
    </div>
  );
}

function reviewLabel(status?: string) {
  if (status === "accepted") return "تأیید شده";
  if (status === "needs_followup") return "نیازمند پیگیری";
  return "ثبت نشده";
}

function DeepReviewBox({ rel }: { rel: Relationship }) {
  const review = rel.deep_review;
  if (!review) return null;
  return (
    <section className="deep-review" aria-label="نتیجه بررسی عمیق">
      <div className="deep-head">
        <Sparkles size={18} />
        <strong>{review.generated_by_llm ? "تحلیل AI" : "تحلیل قاعده‌محور"}</strong>
        <span>{severityLabel[review.severity]}</span>
      </div>
      <p>{review.plain_explanation}</p>
      <dl>
        <div>
          <dt>استدلال حقوقی</dt>
          <dd>{review.legal_reason}</dd>
        </div>
        <div>
          <dt>اقدام پیشنهادی</dt>
          <dd>{review.recommended_action}</dd>
        </div>
      </dl>
      {review.questions && review.questions.length > 0 && (
        <ul>
          {review.questions.map((q) => (
            <li key={q}>{q}</li>
          ))}
        </ul>
      )}
    </section>
  );
}

function ReportView({
  report,
  busy,
  onDeepReview,
  onReview,
}: {
  report: Report;
  busy: Busy;
  onDeepReview: (rel: Relationship) => void;
  onReview: (rel: Relationship, status: "accepted" | "needs_followup") => void;
}) {
  return (
    <div className="report-view">
      {report.plain_language_summary && report.plain_language_summary.length > 0 && (
        <section className="summary-panel">
          <h3>خلاصه ساده برای حقوقی/تطبیق</h3>
          <ul>
            {report.plain_language_summary.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
          <span>{report.summary_generated_by_llm ? "تولید شده با LLM" : "خلاصه قاعده‌محور"}</span>
        </section>
      )}
      <RelationshipList items={report.relationships} busy={busy} onDeepReview={onDeepReview} onReview={onReview} />
    </div>
  );
}

function CircularDetail({ circular, onClose }: { circular: Circular; onClose: () => void }) {
  return (
    <section className="circular-detail" aria-label="متن بخشنامه">
      <div className="detail-head">
        <div>
          <span>{circular.id}</span>
          <h3>{circular.title}</h3>
        </div>
        <button type="button" onClick={onClose} aria-label="بستن متن بخشنامه">
          <X size={18} />
        </button>
      </div>
      <div className="rel-meta">
        <span>{circular.issuer_unit || "واحد نامشخص"}</span>
        <span>{circular.issue_date || "بدون تاریخ"}</span>
        <span>{circular.circular_type}</span>
        <span>{circular.clauses?.length ?? 0} بند</span>
      </div>
      <pre>{circular.raw_text}</pre>
    </section>
  );
}

function CircularList({
  items,
  busy,
  onAnalyze,
  onView,
  onEdit,
  onDelete,
}: {
  items: Circular[];
  busy: Busy;
  onAnalyze: (id: string) => void;
  onView: (circular: Circular) => void;
  onEdit: (circular: Circular) => void;
  onDelete: (id: string) => void;
}) {
  if (items.length === 0) return <EmptyState text="بخشنامه‌ای ثبت نشده است." />;
  return (
    <div className="circular-grid">
      {items.map((circular) => (
        <article className="circular" key={circular.id} data-circular-id={circular.id}>
          <span>{circular.id}</span>
          <h3>{circular.title}</h3>
          <p>{circular.issuer_unit || "واحد نامشخص"} · {circular.issue_date || "بدون تاریخ"}</p>
          <div className="rel-meta">
            <span>{circular.circular_type}</span>
            <span>{circular.status}</span>
            <span>{circular.clauses?.length ?? 0} بند</span>
          </div>
          <div className="circular-actions">
            <button
              type="button"
              data-action="view"
              onClick={() => onView(circular)}
              disabled={busy === `view:${circular.id}`}
            >
              <Eye size={18} />
              مشاهده
            </button>
            <button type="button" data-action="edit" onClick={() => onEdit(circular)}>
              <Pencil size={18} />
              ویرایش
            </button>
            <button type="button" onClick={() => onAnalyze(circular.id)} disabled={busy === `analyze:${circular.id}`}>
              <FileSearch size={18} />
              {busy === `analyze:${circular.id}` ? "تحلیل..." : "تحلیل"}
            </button>
            <button
              type="button"
              data-action="delete"
              className="danger-button"
              onClick={() => onDelete(circular.id)}
              disabled={busy === `delete:${circular.id}`}
            >
              <Trash2 size={18} />
              حذف
            </button>
          </div>
        </article>
      ))}
    </div>
  );
}

function SearchResults({ items }: { items: { score: number; clause: { id: string; original_text: string } }[] }) {
  if (items.length === 0) return <EmptyState text="نتیجه‌ای برای نمایش نیست." />;
  return (
    <div className="relationship-list">
      {items.map((item) => (
        <article className="relationship muted" key={item.clause.id}>
          <div className="rel-head">
            <h3>{item.clause.id}</h3>
            <strong className="confidence">{Math.round(item.score * 100)}٪</strong>
          </div>
          <blockquote>{item.clause.original_text}</blockquote>
        </article>
      ))}
    </div>
  );
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="empty">
      <ShieldCheck size={28} />
      <p>{text}</p>
    </div>
  );
}

export default App;
