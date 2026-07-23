import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";

const baseURL = process.env.BASE_URL || "http://127.0.0.1:18081";
const chromium = process.env.CHROMIUM || "/snap/bin/chromium";
const port = Number(process.env.CDP_PORT || 9229);
const profile = await mkdtemp(join(tmpdir(), "conflict-ui-"));
const cleanupIDs = [];
const browser = spawn(chromium, [
  "--headless",
  "--disable-gpu",
  "--no-sandbox",
  `--remote-debugging-port=${port}`,
  `--user-data-dir=${profile}`,
  "about:blank",
], { stdio: ["ignore", "ignore", "pipe"] });

try {
  await waitFor(async () => {
    const res = await fetch(`http://127.0.0.1:${port}/json/version`);
    if (!res.ok) throw new Error(`cdp status ${res.status}`);
    return true;
  }, "Chromium CDP did not start");

  const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json());
  const page = targets.find((target) => target.type === "page");
  if (!page) throw new Error("no Chromium page target found");
  const cdp = await connect(page.webSocketDebuggerUrl);
  await cdp.send("Runtime.enable");
  await cdp.send("Page.enable");
  await cdp.send("Page.navigate", { url: baseURL });
  await waitFor(() => evalValue(cdp, "document.readyState === 'complete'"), "page did not load");
  await evalValue(cdp, "localStorage.clear()");

  await clickText(cdp, "دستیار اهلیت");
  await waitFor(() => evalValue(cdp, "document.body.textContent.includes('کد ملی مشتری')"), "assistant view did not open");
  await clickText(cdp, "مدیر پردرآمد");
  await waitFor(() => evalValue(cdp, "document.body.textContent.includes('دسته‌چک') && document.body.textContent.includes('خلاصه حقوقی/تطبیق')"), "assistant existing customer did not render");
  await clickText(cdp, "مشتری جدید");
  await waitFor(() => evalValue(cdp, "document.body.textContent.includes('اطلاعات خوداظهاری مشتری جدید')"), "assistant intake did not render");
  await clickText(cdp, "تکمیل ارزیابی Cold-start");
  await waitFor(() => evalValue(cdp, "document.body.textContent.includes('Cold-start') && document.body.textContent.includes('آفرهای پیشنهادی')"), "assistant cold-start result did not render");

  await clickText(cdp, "اسکن آرشیو");
  await waitFor(() => evalValue(cdp, "document.body.textContent.includes('اسکن آرشیو کامل شد.')"), "archive scan did not complete");
  const titleBefore = await evalValue(cdp, "document.querySelector('.topbar h2')?.textContent");
  if (titleBefore !== "گزارش تحلیل") throw new Error(`expected report view, got title=${titleBefore}`);
  await clickText(cdp, "همه موارد");
  const actionableCount = await relationshipCount(cdp);
  if (actionableCount < 1) {
    throw new Error(`expected archive report relationships in all-items filter, got count=${actionableCount}`);
  }

  await evalValue(cdp, "document.querySelector('.switch input')?.click()");
  await waitFor(async () => (await relationshipCount(cdp)) > actionableCount, "compatible toggle did not add rows");
  const titleAfter = await evalValue(cdp, "document.querySelector('.topbar h2')?.textContent");
  if (titleAfter !== titleBefore) throw new Error(`compatible toggle changed view: ${titleBefore} -> ${titleAfter}`);

  await evalValue(cdp, "document.querySelector('.switch input')?.click()");
  await waitFor(async () => (await relationshipCount(cdp)) === actionableCount, "compatible toggle did not return to actionable rows");
  await clickText(cdp, "فقط حل‌نشده‌ها");

  const reviewSUP = `UI-SMOKE-SUP-${Date.now()}`;
  const reviewNEW = `UI-SMOKE-NEW-${Date.now()}`;
  cleanupIDs.push(reviewNEW, reviewSUP);
  await pageFetch(cdp, "/circulars", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      id: reviewSUP,
      title: "بخشنامه موقت منع دسته‌چک",
      text: "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
      issuer_unit: "واحد آزمون",
      circular_type: "supervisory",
      issue_date: "1404/02/01",
      topic: "دسته‌چک",
    }),
  });
  await pageFetch(cdp, "/circulars", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      id: reviewNEW,
      title: "بخشنامه موقت مجوز دسته‌چک",
      text: "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
      issuer_unit: "واحد آزمون",
      circular_type: "internal",
      issue_date: "1404/02/02",
      topic: "دسته‌چک",
    }),
  });
  const tempReport = await pageFetch(cdp, `/circulars/${encodeURIComponent(reviewNEW)}/analyze`, { method: "POST" });
  const tempRel = tempReport.relationships.find((rel) => rel.relationship_type !== "overlap_without_conflict");
  if (!tempRel) throw new Error("temporary conflict relationship was not created");

  await cdp.send("Page.reload");
  await waitFor(() => evalValue(cdp, "document.readyState === 'complete'"), "page did not reload after temporary relationship");
  await clickText(cdp, "روابط");
  await waitFor(() => evalValue(cdp, `!!document.querySelector('[data-relationship-id="${tempRel.id}"]')`), "temporary relationship not listed");
  const tempCountBeforeAccept = await relationshipCount(cdp);
  await clickRelationshipAction(cdp, tempRel.id, "accepted");
  await waitFor(async () => (await relationshipCount(cdp)) === tempCountBeforeAccept - 1, "accepted item was not hidden from unresolved list");
  await clickText(cdp, "همه موارد");
  await waitFor(() => evalValue(cdp, `document.querySelector('[data-relationship-id="${tempRel.id}"] [data-review-status="accepted"]') !== null`), "accepted status not visible in all-items filter");
  await clickText(cdp, "فقط حل‌نشده‌ها");
  await waitFor(() => evalValue(cdp, `!document.querySelector('[data-relationship-id="${tempRel.id}"]')`), "accepted status still visible in unresolved filter");
  await clickText(cdp, "همه موارد");
  await clickRelationshipAction(cdp, tempRel.id, "needs_followup");
  await clickText(cdp, "فقط حل‌نشده‌ها");
  await waitFor(() => evalValue(cdp, `document.querySelector('[data-relationship-id="${tempRel.id}"] [data-review-status="needs_followup"]') !== null`), "follow-up status not rendered in unresolved filter");

  const tempID = `UI-SMOKE-${Date.now()}`;
  cleanupIDs.push(tempID);
  const originalText = "بند 1) این بخشنامه موقت برای آزمون مشاهده، ویرایش و حذف ساخته شده است.";
  const editedTitle = "بخشنامه موقت ویرایش‌شده";
  const editedText = "بند 1) متن ویرایش‌شده بخشنامه موقت برای آزمون ذخیره شده است.";
  await pageFetch(cdp, "/circulars", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      id: tempID,
      title: "بخشنامه موقت آزمون رابط",
      text: originalText,
      issuer_unit: "واحد آزمون",
      circular_type: "internal",
      issue_date: "1404/02/01",
      topic: "آزمون رابط",
    }),
  });
  await cdp.send("Page.reload");
  await waitFor(() => evalValue(cdp, "document.readyState === 'complete'"), "page did not reload");
  await clickText(cdp, "بخشنامه‌ها");
  await waitFor(() => evalValue(cdp, `!!document.querySelector('[data-circular-id="${tempID}"]')`), "temporary circular not listed");
  await clickCircularAction(cdp, tempID, "view");
  await waitFor(() => evalValue(cdp, `document.querySelector('.circular-detail')?.textContent.includes(${JSON.stringify(originalText)})`), "circular text was not shown");
  await clickCircularAction(cdp, tempID, "edit");
  await waitFor(() => evalValue(cdp, `document.querySelector('[data-field="id"]')?.value === "${tempID}"`), "circular was not loaded into edit form");
  await setField(cdp, "title", editedTitle);
  await setField(cdp, "text", editedText);
  await clickText(cdp, "ذخیره تغییرات");
  await waitFor(() => evalValue(cdp, "document.body.textContent.includes('بخشنامه ذخیره شد.')"), "edited circular was not saved");
  await waitFor(() => evalValue(cdp, `document.querySelector('[data-circular-id="${tempID}"]')?.textContent.includes(${JSON.stringify(editedTitle)})`), "edited circular title not listed");
  await evalValue(cdp, "window.confirm = () => true");
  await clickCircularAction(cdp, tempID, "delete");
  await waitFor(() => evalValue(cdp, `!document.querySelector('[data-circular-id="${tempID}"]')`), "deleted circular still listed");
  console.log("UI smoke passed: assistant, archive scan, compatible filter, unresolved/all filter, accepted hide, follow-up stays, circular view/edit/delete");
} finally {
  browser.kill("SIGTERM");
  await cleanupSmokeCirculars(cleanupIDs);
  await rm(profile, { recursive: true, force: true });
}

function connect(url) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(url);
    let nextID = 1;
    const pending = new Map();
    ws.addEventListener("open", () => {
      resolve({
        send(method, params = {}) {
          const id = nextID++;
          ws.send(JSON.stringify({ id, method, params }));
          return new Promise((ok, fail) => pending.set(id, { ok, fail }));
        },
      });
    });
    ws.addEventListener("message", (event) => {
      const msg = JSON.parse(event.data);
      if (!msg.id) return;
      const p = pending.get(msg.id);
      if (!p) return;
      pending.delete(msg.id);
      if (msg.error) p.fail(new Error(msg.error.message));
      else p.ok(msg.result);
    });
    ws.addEventListener("error", reject);
  });
}

async function evalValue(cdp, expression) {
  const res = await cdp.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
  if (res.exceptionDetails) throw new Error(res.exceptionDetails.text);
  return res.result.value;
}

async function clickText(cdp, text) {
  const clicked = await evalValue(cdp, `
    (() => {
      const el = [...document.querySelectorAll('button')].find((b) => b.textContent.includes(${JSON.stringify(text)}));
      if (!el) return false;
      el.click();
      return true;
    })()
  `);
  if (!clicked) throw new Error(`button not found: ${text}`);
}

async function clickCircularAction(cdp, id, action) {
  const clicked = await evalValue(cdp, `
    (() => {
      const card = document.querySelector('[data-circular-id="${id}"]');
      const button = card?.querySelector('[data-action="${action}"]');
      if (!button) return false;
      button.click();
      return true;
    })()
  `);
  if (!clicked) throw new Error(`circular action not found: ${id} ${action}`);
}

async function clickRelationshipAction(cdp, id, action) {
  const clicked = await evalValue(cdp, `
    (() => {
      const card = document.querySelector('[data-relationship-id="${id}"]');
      const button = card?.querySelector('[data-review-action="${action}"]');
      if (!button) return false;
      button.click();
      return true;
    })()
  `);
  if (!clicked) throw new Error(`relationship action not found: ${id} ${action}`);
}

async function setField(cdp, field, value) {
  const changed = await evalValue(cdp, `
    (() => {
      const el = document.querySelector('[data-field="${field}"]');
      if (!el) return false;
      const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, ${JSON.stringify(value)});
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    })()
  `);
  if (!changed) throw new Error(`field not found: ${field}`);
}

async function pageFetch(cdp, path, init) {
  return evalValue(cdp, `
    fetch(${JSON.stringify(path)}, ${JSON.stringify(init)}).then(async (res) => {
      if (!res.ok) throw new Error(await res.text());
      return res.json();
    })
  `);
}

async function cleanupSmokeCirculars(ids) {
  await Promise.all([...new Set(ids)].map(async (id) => {
    try {
      await fetch(`${baseURL}/circulars/${encodeURIComponent(id)}`, { method: "DELETE" });
    } catch {
      // best-effort cleanup only
    }
  }));
}

function relationshipCount(cdp) {
  return evalValue(cdp, "document.querySelectorAll('.relationship').length");
}

async function waitFor(fn, message, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    try {
      if (await fn()) return;
    } catch (err) {
      last = err;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(last ? `${message}: ${last.message}` : message);
}
