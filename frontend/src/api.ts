export type RelationshipType =
  | "full_contradiction"
  | "partial_contradiction"
  | "overlap_without_conflict"
  | "supersession"
  | "neutral";

export type Circular = {
  id: string;
  title: string;
  raw_text: string;
  issuer_unit: string;
  circular_type: string;
  topic: string;
  issue_date: string;
  status: string;
  clauses: Clause[];
};

export type Clause = {
  id: string;
  circular_id: string;
  clause_number: string;
  original_text: string;
  subject: string;
  ruling_type: string;
};

export type DeepReview = {
  verdict: "conflict" | "partial_conflict" | "compatible" | "supersession" | "needs_human_review";
  severity: "critical" | "high" | "medium" | "low" | "none";
  plain_explanation: string;
  legal_reason: string;
  recommended_action: string;
  questions?: string[];
  generated_by_llm: boolean;
};

export type Relationship = {
  id: string;
  source_clause_id: string;
  target_clause_id: string;
  relationship_type: RelationshipType;
  confidence: number;
  rationale: string;
  evidence_json: Record<string, unknown>;
  resolver_status: string;
  winning_clause_id?: string;
  required_action: string;
  review_status?: string;
  reviewed_by?: string;
  review_note?: string;
  reviewed_at?: string;
  deep_review?: DeepReview;
};

export type Report = {
  circular_id: string;
  summary: Record<string, number>;
  plain_language_summary?: string[];
  summary_generated_by_llm: boolean;
  relationships: Relationship[];
};

export type CircularRequest = {
  id: string;
  title: string;
  text: string;
  issuer_unit: string;
  circular_type: string;
  issue_date: string;
  topic: string;
};

export type SearchItem = {
  score: number;
  clause: Clause;
};

export type SelfDeclared = {
  name?: string;
  age?: number;
  job_category?: string;
  purpose_category?: string;
  declared_monthly_income?: number;
  requested_amount?: number;
};

export type EvidenceRef = {
  circular_id: string;
  clause: string;
};

export type GapItem = {
  metric: string;
  current: unknown;
  required: unknown;
  unit?: string;
};

export type EligibilityItem = {
  product_id: string;
  product_name_fa: string;
  decision: "eligible" | "conditional" | "not_eligible";
  reason: string;
  evidence: EvidenceRef[];
  gap: GapItem[];
};

export type Offer = {
  product_id: string;
  product_name_fa: string;
  rank: number;
  why: string;
};

export type AssistResponse = {
  national_id: string;
  customer_status: "existing" | "new";
  risk?: { risk_level: string; risk_level_fa: string; risk_score: number; mode?: string; reason?: string };
  profile_summary?: Record<string, unknown>;
  intake?: { need_more_info: boolean; question: string; field: string; options?: string[] };
  eligibility: EligibilityItem[];
  offers?: Offer[];
  conversation?: string;
  legal_summary?: string;
  legal_summary_generated_by_llm: boolean;
  payment_impact?: string;
  trace: { agent: string; tool?: string; status?: unknown; detail?: unknown }[];
};

type Cached<T> = {
  at: number;
  value: T;
};

const ttlMs = 45_000;
const mem = new Map<string, Cached<unknown>>();

function readCache<T>(key: string): T | null {
  const hit = mem.get(key) as Cached<T> | undefined;
  if (hit && Date.now() - hit.at < ttlMs) return hit.value;
  const raw = localStorage.getItem(key);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Cached<T>;
    if (Date.now() - parsed.at < ttlMs) {
      mem.set(key, parsed);
      return parsed.value;
    }
  } catch {
    localStorage.removeItem(key);
  }
  return null;
}

function writeCache<T>(key: string, value: T): T {
  const payload: Cached<T> = { at: Date.now(), value };
  mem.set(key, payload);
  localStorage.setItem(key, JSON.stringify(payload));
  return value;
}

function dropCache(prefixes: string[]) {
  mem.clear();
  for (const key of Object.keys(localStorage)) {
    if (prefixes.some((prefix) => key.startsWith(prefix))) localStorage.removeItem(key);
  }
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  const data = (await res.json()) as T & { message?: string };
  if (!res.ok) throw new Error(data.message || res.statusText);
  return data;
}

export function cached<T>(key: string, load: () => Promise<T>, apply: (data: T, stale: boolean) => void) {
  const cachedValue = readCache<T>(key);
  if (cachedValue) apply(cachedValue, true);
  return load().then((fresh) => apply(writeCache(key, fresh), false));
}

export const api = {
  health: () => json<{ circulars: number; llm: { enabled: boolean; model: string } }>("/health"),
  circulars: () => json<{ count: number; items: Circular[] }>("/circulars"),
  circular: (id: string) => json<Circular>(`/circulars/${encodeURIComponent(id)}`),
  relationships: (actionable = true) =>
    json<{ count: number; items: Relationship[] }>(`/relationships?actionable=${actionable ? "true" : "false"}`),
  createCircular: (body: CircularRequest) => {
    dropCache(["cache:"]);
    return json<{ circular_id: string; clause_count: number }>("/circulars", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
  },
  updateCircular: (id: string, body: CircularRequest) => {
    dropCache(["cache:"]);
    return json<Circular>(`/circulars/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
  },
  deleteCircular: (id: string) => {
    dropCache(["cache:"]);
    return json<{ deleted: string }>(`/circulars/${encodeURIComponent(id)}`, { method: "DELETE" });
  },
  analyze: (id: string) => {
    dropCache(["cache:relationships"]);
    return json<Report>(`/circulars/${encodeURIComponent(id)}/analyze`, { method: "POST" });
  },
  archiveScan: () => {
    dropCache(["cache:relationships"]);
    return json<Report>("/scans/archive", { method: "POST" });
  },
  deepReview: (id: string) => {
    dropCache(["cache:relationships"]);
    return json<Relationship>(`/relationships/${encodeURIComponent(id)}/deep-review`, { method: "POST" });
  },
  review: (id: string, status: "accepted" | "needs_followup", note: string) => {
    dropCache(["cache:relationships"]);
    return json<Relationship>(`/relationships/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ status, by: "ui", note }),
    });
  },
  assist: (nationalID: string, selfDeclared?: SelfDeclared) =>
    json<AssistResponse>("/assist", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ national_id: nationalID, self_declared: selfDeclared }),
    }),
  intake: (nationalID: string, answers: SelfDeclared) =>
    json<AssistResponse | { need_more_info: boolean; question: string; field: string; options?: string[] }>("/assist/intake", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ national_id: nationalID, answers }),
    }),
  search: (q: string) => json<{ query: string; items: SearchItem[] }>(`/search?q=${encodeURIComponent(q)}`),
};
