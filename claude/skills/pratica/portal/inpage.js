// Fills a portal pratica inside the Claude browser pane from a steps file, with no model
// call per field: run.py page writes install.js (this file and the procedure's steps, no
// personal data) and run.js (the person's values), each run in the pane with one call.
//
// The portal reloads on every save, which would end a script running in it, so the
// runner stays in the top page and drives the portal in a same-origin frame over it (the
// portal sends no X-Frame-Options or frame-ancestors). It waits on PrimeFaces' own ajax
// queue instead of fixed pauses, sets dropdowns through their widgets (the same change
// event and server call a click makes), reads every field back after each save and
// again on a fresh load of the draft before it says `filled`, and never opens a
// declaration or clicks what is the person's: payment, signing, sending.
// A file is attached by building it in the page and handing it to the file box as a
// drop would (DataTransfer): the pane can't type a path into one, and a public page
// can't fetch from localhost, so its bytes come in run.js (run.py says how big), or a big
// one by an https link the page fetches, checked against its sha256.
//
//   __pv.start(procedure, values[, resume])  starts a run, returns at once. resume picks a
//                                  stopped run up: {url, from, kept, fixes} (run.py --resume);
//                                  a stop on the portal's own error goes on by itself (`retries`)
//   await __pv.wait(ms)            the run's report once it ends, or after ms
//   __pv.last()                    the last run's report, kept in the tab (sessionStorage: it
//                                  outlives a reload of the runner, not the tab)
//   __pv.resume({fixes, url})      the last stopped run again on its draft, from where it
//                                  stopped, with the same values and any fixes
//   __pv.startSealed(procedure, sealed)  as start, with the values sealed under a code the
//                                  person types into a box on the page (run.py page, not --plain)
//   __pv.look()                    the page with no value in it: each visible box's id, label
//                                  and whether it holds anything, the portal's messages. A
//                                  stopped run's report carries it as `snapshot`
//   __pv.held()                    what the reports leave out (the values each section typed,
//                                  what a failed read-back wanted and found, the declarations'
//                                  own text): for run.py to write where only the person reads
//                                  it, never for Claude
//   __pv.review()                  draws what the person reads before they tick over the portal:
//                                  the declarations' own text and every value typed, from memory
//                                  (a filled run draws it itself); "Show the draft" removes it
//   await __pv.previewPdf()        the portal's own PDF of the draft, fetched when a run is filled
//                                  (base64, or null): for run.py to keep for the review page. The
//                                  document the person declares is the portal's rendering, not our
//                                  list of values
//
// No report holds a value: a failed read-back says why ("empty", "different"), an error
// names the box and never what was typed, and a portal message that echoes a value is
// scrubbed. So Claude can recover a stopped run from the report and look() without reading
// or photographing a filled page (docs/north-star.md, the privacy contract).
//   await __pv.reopen(name)        a draft opened from Pratiche in bozza by its name, when its
//                                  address has expired: its address, for resume({url})
//   __pv.open()                    loads the frame's page as the top page, for the person
(() => {
  class StepError extends Error {}
  const fail = (m) => { throw new StepError(m); };
  // The pane slows timers to a few ticks a second; message events aren't slowed.
  const channel = new MessageChannel(), waiting = [];
  channel.port1.onmessage = () => waiting.shift()();
  const yieldTask = () => new Promise((r) => { waiting.push(r); channel.port2.postMessage(0); });
  const sleep = async (ms) => { const end = performance.now() + ms; do await yieldTask(); while (performance.now() < end); };
  const norm = (s) => (s || "").normalize("NFKD").replace(/[^\x00-\x7f]/g, "")
    .replace(/\s+/g, " ").trim().toLowerCase();

  let steps, frame, report, kept, secrets = [], details = [], declared = [], sealedRun = null, previewBlob = null;

  // The person's values, as patterns to scrub from anything a report says. Four characters
  // or more, in the text as given and as norm() would show it (the portal's messages are
  // normalized); a file counts by its name, not its bytes.
  const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  function hide(values) {
    const out = new Set();
    const add = (v) => {
      if (typeof v === "string") { if (norm(v).length >= 4) out.add(v.trim()); }
      else if (Array.isArray(v)) v.forEach(add);
      else if (v && typeof v === "object") add(v.name);
    };
    add([values.wizard, values.sections]);
    secrets = [...out].flatMap((s) => [s, norm(s)]).filter(Boolean).sort((a, b) => b.length - a.length)
      .map((t) => new RegExp(esc(t).replace(/\s+/g, "\\s+"), "gi"));
  }
  const scrub = (s) => secrets.reduce((t, rx) => t.replace(rx, "‹value›"), String(s));
  // Why a read-back failed, without what it wanted or found.
  const redact = (c) => ({ label: c.label, why: c.got === null ? "missing" : c.got === "" || c.got === "unchecked" ? "empty" : "different" });
  function publicReport() {
    const out = { ...report };
    delete out.kept;
    if (out.error) out.error = scrub(out.error);
    if (out.retries) out.retries = out.retries.map((r) => ({ ...r, error: scrub(r.error) }));
    out.sections = (out.sections || []).map((s) => ({ ...s, errors: s.errors.map(scrub) }));
    return out;
  }

  // --- sealed values (run.py: seal) ---------------------------------------------------
  // run.js can carry the values sealed under a code that the person types into a box here, so
  // they never pass through the conversation. As run.py builds it: PBKDF2-HMAC-SHA256 of the
  // code, an HMAC-SHA256 keystream, encrypt-then-MAC (WebCrypto has both primitives on any
  // https page). A stopped run's typed values are sealed again under the same code before they
  // are kept in the tab, so none sits in storage in the clear.
  const utf8 = new TextEncoder(), utf8d = new TextDecoder();
  const fromB64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
  const toB64 = (u) => {
    let s = "";
    for (let i = 0; i < u.length; i += 0x8000) s += String.fromCharCode(...u.subarray(i, i + 0x8000));
    return btoa(s);
  };
  const hmacKey = (raw, usages) => crypto.subtle.importKey("raw", raw, { name: "HMAC", hash: "SHA-256" }, false, usages);
  const hmac = async (key, data) => new Uint8Array(await crypto.subtle.sign("HMAC", key, data));
  async function sealKeys(code, sealed) {
    const base = await crypto.subtle.importKey("raw", utf8.encode(code), "PBKDF2", false, ["deriveBits"]);
    const master = await crypto.subtle.deriveBits(
      { name: "PBKDF2", hash: "SHA-256", salt: fromB64(sealed.s), iterations: sealed.i }, base, 256);
    const m = await hmacKey(master, ["sign"]);
    return { enc: await hmacKey(await hmac(m, utf8.encode("pv-enc")), ["sign"]),
      mac: await hmacKey(await hmac(m, utf8.encode("pv-mac")), ["sign", "verify"]), s: sealed.s, i: sealed.i };
  }
  async function keystream(keys, nonce, n) {
    const blocks = Math.ceil(n / 32), out = new Uint8Array(blocks * 32);
    for (let b = 0; b < blocks; b += 256) {
      const part = await Promise.all(Array.from({ length: Math.min(256, blocks - b) }, (_, j) => {
        const msg = new Uint8Array(20);
        msg.set(nonce);
        new DataView(msg.buffer).setUint32(16, b + j);
        return hmac(keys.enc, msg);
      }));
      part.forEach((blk, j) => out.set(blk, (b + j) * 32));
    }
    return out.subarray(0, n);
  }
  const xor = (a, b) => a.map((x, i) => x ^ b[i]);
  function tagInput(s, nonce, ct) { // "pv1", iterations, salt, nonce, ciphertext
    const salt = fromB64(s.s), out = new Uint8Array(7 + salt.length + nonce.length + ct.length);
    out.set(utf8.encode("pv1"));
    new DataView(out.buffer).setUint32(3, s.i);
    out.set(salt, 7);
    out.set(nonce, 7 + salt.length);
    out.set(ct, 7 + salt.length + nonce.length);
    return out;
  }
  async function openSealed(keys, sealed) {
    const nonce = fromB64(sealed.n), ct = fromB64(sealed.c);
    if (!(await crypto.subtle.verify("HMAC", keys.mac, fromB64(sealed.t), tagInput(sealed, nonce, ct)))) throw new Error("wrong code");
    return xor(ct, await keystream(keys, nonce, ct.length));
  }
  async function sealAgain(keys, bytes) {
    const nonce = crypto.getRandomValues(new Uint8Array(16)), ct = xor(bytes, await keystream(keys, nonce, bytes.length));
    const out = { v: 1, i: keys.i, s: keys.s, n: toB64(nonce), c: toB64(ct) };
    out.t = toB64(await hmac(keys.mac, tagInput(out, nonce, ct)));
    return out;
  }
  // The box for the code. It is on the top page, not in the portal's frame, takes a masked
  // input (a read of the page shows no characters) and goes the moment the code is complete.
  // 15 characters, as run.py's SEAL_CODE_LENGTH; Crockford base32, so O reads as 0, I and L as 1.
  function askCode(note) {
    return new Promise((resolve) => {
      const box = document.createElement("div");
      box.dataset.pv = "code";
      box.style.cssText = "position:fixed;inset:0;z-index:2147483647;background:#fff;display:flex;flex-direction:column;" +
        "align-items:center;justify-content:center;gap:12px;font:16px system-ui,sans-serif";
      const msg = document.createElement("div");
      msg.textContent = (note ? note + " " : "") + "Type the code shown in the pvault window on your screen.";
      const input = document.createElement("input");
      input.type = "password";
      input.autocomplete = "off";
      input.spellcheck = false;
      input.maxLength = 24;
      input.style.cssText = "font:20px monospace;letter-spacing:2px;padding:6px;width:18em";
      box.append(msg, input);
      document.body.append(box);
      input.focus();
      const clean = () => input.value.toUpperCase().replace(/[\s-]/g, "").replace(/O/g, "0").replace(/[IL]/g, "1");
      const done = () => { const code = clean(); input.value = ""; box.remove(); resolve(code); };
      input.addEventListener("input", () => { if (clean().length >= 15) done(); });
      input.addEventListener("keydown", (e) => { if (e.key === "Enter" && clean()) done(); });
    });
  }
  async function unlock(sealed) {
    let note = "";
    for (let tries = 0; tries < 5; tries++) {
      const keys = await sealKeys(await askCode(note), sealed);
      try { return { keys, plain: JSON.parse(utf8d.decode(await openSealed(keys, sealed))) }; }
      catch (e) { note = "That code didn't open it."; }
    }
    throw new StepError("the code didn't open the values");
  }

  const win = () => frame.contentWindow;
  const doc = () => frame.contentDocument;
  const el = (id) => doc().querySelector(`[id$="dynaForm:${id}"]`);
  const widgets = () => Object.values((win().PrimeFaces || {}).widgets || {}); // none: a plain form
  const widgetOf = (e) => widgets().find((w) => w && w.input && w.input[0] === e);

  const busy = () => {
    const w = win(), d = doc();
    return !d || d.readyState !== "complete" || (w.jQuery && w.jQuery.active > 0) ||
      !!(w.PrimeFaces && w.PrimeFaces.ajax && !w.PrimeFaces.ajax.Queue.isEmpty());
  };

  async function idle(ms = 20000) {
    const end = performance.now() + ms;
    for (let quiet = 0; quiet < 3;) {
      if (performance.now() > end) fail("the portal stayed busy");
      quiet = busy() ? 0 : quiet + 1;
      await sleep(30);
    }
  }

  // Runs a click and waits for what it starts, until 300 ms pass with no ajax in flight
  // and no page loading: a load can start another (the last wizard screen redirects).
  async function act(click, ms = 60000) {
    const end = performance.now() + ms;
    let loads = 0, leaving = null;
    const onload = () => { loads++; };
    const watch = () => win().addEventListener("beforeunload", () => { leaving = loads; });
    frame.addEventListener("load", onload);
    watch();
    click();
    for (let quiet = 0; quiet < 6;) {
      if (performance.now() > end) fail("the portal didn't answer");
      await sleep(50);
      if (leaving !== null) {
        while (loads <= leaving) {
          if (performance.now() > end) fail("the page didn't load");
          await sleep(30);
        }
        leaving = null;
        watch();
        quiet = 0;
        continue;
      }
      quiet = busy() ? 0 : quiet + 1;
    }
    frame.removeEventListener("load", onload);
  }

  function button(text) {
    const b = [...doc().querySelectorAll("button, a, input[type=submit]")]
      .find((e) => e.offsetParent && norm(e.textContent || e.value) === norm(text));
    if (!b) fail(`no ${text} button`);
    if (steps.forbidden.test([b.textContent, b.value, b.title].join(" "))) {
      fail(`refused to click ${text}: that step is the person's`);
    }
    return b;
  }

  // ".error-message-text" is the portal's message under a field ("Il valore è obbligatorio").
  // `theirs`: a section's fields the person fills (a statement in their name); the
  // portal's "required" under one of them isn't the run's error.
  const errors = (theirs = []) => [...doc().querySelectorAll(".ui-message-error, .ui-messages-error, .ui-growl-message-error, [id*=\"dynaForm\"] .error-message-text")]
    .filter((e) => e.getClientRects().length && norm(e.textContent))
    .filter((e) => { const box = e.closest("td") && e.closest("td").querySelector("input, select, textarea");
      return !(box && theirs.some((t) => box.id.endsWith("dynaForm:" + t) ||
        win().eval(`(${steps.label_js})`)(box).some((x) => norm(x).includes(norm(t))))); })
    .map((e) => norm(e.textContent));

  function checkLabel(e, label) {
    if (!label) return;
    const texts = win().eval(`(${steps.label_js})`)(e);
    if (!texts.some((t) => norm(t).includes(norm(label)))) {
      fail(`expected the label ${label} by this field, found ${JSON.stringify(texts.slice(0, 3))}`);
    }
  }

  function fire(e) {
    for (const t of ["input", "change"]) e.dispatchEvent(new (win().Event)(t, { bubbles: true }));
  }

  // A script's click reaches e even under a banner, a dialog or a re-render's leftovers,
  // where a person's couldn't: the page is then in a state the steps file doesn't know,
  // so stop there. e must still be in the page and be what's on top at its centre.
  function onTop(e, what) {
    if (!e.isConnected) fail(`${what}: the page changed under it`);
    e.scrollIntoView({ block: "center", inline: "nearest" });
    const r = e.getBoundingClientRect();
    if (!r.width || !r.height) fail(`${what}: not visible`);
    // A label that wraps is several line boxes: the middle of the whole can fall between two
    // lines, on its parent. Any line's middle on it will do.
    const at = (b) => doc().elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    const mine = (h) => h && (h === e || e.contains(h) || h.control === e);
    const lines = [...e.getClientRects()].filter((b) => b.width && b.height);
    const hit = at(r);
    if (!mine(hit) && !lines.some((b) => mine(at(b))) && hit) {
      const name = hit.id ? `#${hit.id}` : hit.className ? `.${String(hit.className).split(" ")[0]}` : "";
      fail(`${what}: covered by ${hit.tagName.toLowerCase()}${name} "${norm(hit.innerText).slice(0, 60)}"`);
    }
  }

  function press(e, what) {
    onTop(e, what);
    e.click();
  }

  // wizard

  async function tree(item) {
    // A name, or {leaf, nth} for a name the tree has under several parents (Napoli's SUAP
    // lists "Acconciatore, barbiere, ..." twice).
    const { leaf, nth } = typeof item === "string" ? { leaf: item, nth: 0 } : item;
    const node = () => {
      const s = [...doc().querySelectorAll(".ui-tree .tree-content > span")].filter((x) => x.textContent.trim() === leaf);
      if (nth ? s.length < nth : s.length !== 1) fail(`tree: ${s.length} nodes named ${leaf}`);
      return s[nth ? nth - 1 : 0];
    };
    // Open its ancestors, outermost first, then select it.
    const keys = [];
    for (let li = node().closest("li.ui-treenode"); (li = li.parentElement && li.parentElement.closest("li.ui-treenode"));) {
      keys.unshift(li.dataset.rowkey);
    }
    for (const k of keys) {
      const c = doc().querySelector(`li[data-rowkey="${k}"] > .ui-treenode-content`);
      if (c.getAttribute("aria-expanded") !== "true") {
        press(c.querySelector(".ui-treenode-label"), `tree: ${leaf}`);
        await idle();
      }
    }
    if (!node().closest("li.ui-treenode").classList.contains("ui-treenode-selected")) {
      press(node(), `tree: ${leaf}`);
      await idle();
    }
    if (!node().closest("li.ui-treenode").classList.contains("ui-treenode-selected")) {
      fail(`tree: ${leaf} didn't stay selected`);
    }
  }

  async function wizard(step, value, wi) {
    report.at = { phase: "wizard", index: wi, screen: step.screen };
    if (!norm(doc().body.innerText).includes(norm(step.screen))) fail(`expected the screen ${step.screen}`);
    if (step.fill) {
      const e = doc().querySelector(step.fill.css) || fail(`no field ${step.fill.css}`);
      e.focus();
      e.value = value;
      fire(e);
      e.blur();
      await idle();
    }
    for (const leaf of [].concat(step.tree || [])) await tree(leaf);
    // A Si/No question in the tree ("Le opere ricadono nell'ambito della Sanatoria ...?").
    for (const [question, answer] of Object.entries(step.answers || {})) {
      const q = [...doc().querySelectorAll('table[id$=":sinCond"]')]
        .filter((t) => norm(t.closest("li, tr, div").innerText).includes(norm(question)));
      if (q.length !== 1) fail(`${q.length} Si/No questions ${question}`);
      const td = [...q[0].querySelectorAll("td")].find((c) => norm(c.innerText) === norm(answer)) || fail(`${question}: no ${answer}`);
      press(td.querySelector(".ui-radiobutton-box") || td.querySelector("input"), `${question}: ${answer}`);
      await idle();
    }
    // The page's text lists every screen in its menu, so a screen is told by its title: the
    // same title after CONFERMA means the portal refused it.
    const title = () => norm((doc().querySelector(".content-title") || {}).innerText);
    const before = title();
    await act(() => press(button(step.confirm), step.confirm));
    if (before && title() === before) {
      const why = [...doc().querySelectorAll("span, div")].filter((e) => !e.children.length && e.offsetParent && /obbligatori|errore/i.test(e.textContent));
      fail(`still on ${step.screen} after ${step.confirm}: ${why.map((e) => norm(e.textContent)).join("; ") || "no message"}`);
    }
  }

  // form sections

  // A street's suggestion list can open again after its pick and lie over the next boxes.
  async function closeLists() {
    const open = [...doc().querySelectorAll(".ui-autocomplete-panel")].filter((p) => p.offsetParent);
    for (const p of open) {
      const w = widgets().find((x) => x && x.panel && x.panel[0] === p);
      if (w && w.hide) w.hide();
      p.style.display = "none"; // PrimeFaces 11's hide() only starts a fade, still over the next box
    }
    if (open.length) await idle();
  }

  async function text(f, value) {
    const e = el(f.id) || fail(`${f.id}: no field`);
    checkLabel(e, f.label);
    await closeLists();
    onTop(e, f.label || f.id);
    const w = widgetOf(e);
    e.focus();
    if (w && typeof w.setValue === "function") w.setValue(value);
    else e.value = value;
    fire(e);
    e.blur();
    await idle();
    return value;
  }

  async function select(f, value) {
    const sel = el(f.id + ":combo_input") || fail(`${f.id}: no dropdown`);
    checkLabel(el(f.id + ":combo_label"), f.label);
    const opts = [...sel.options];
    const i = opts.findIndex((o) => o.value === value || o.text.trim().toUpperCase() === value.toUpperCase());
    if (i < 0) fail(`${f.id}: none of its ${opts.length} options is the value`);
    if (sel.value !== opts[i].value) {
      const w = widgetOf(sel) || fail(`${f.id}: no dropdown widget`);
      if (!w.items) { // its list is built when it first opens
        w.callHandleMethod(null, null);
        await idle();
      }
      // selectValue is silent in PrimeFaces 11; selectItem fires the change the portal
      // listens to (a province loads its comuni).
      w.selectItem(w.items.eq(i));
      await idle();
    }
    if (el(f.id + ":combo_input").value !== opts[i].value) fail(`${f.id}: the chosen option didn't stay selected`);
    return opts[i].value;
  }

  async function tick(f) {
    const e = el(f.id) || fail(`${f.id}: no box`);
    checkLabel(e, f.label);
    if (!e.checked) {
      const wrap = e.closest(".ui-radiobutton, .ui-chkbox");
      press(wrap && wrap.querySelector(".ui-radiobutton-box, .ui-chkbox-box") || e, f.label || f.id);
      await idle();
    }
    // A box that opens rows renumbers itself: find a label-named one again.
    const now = el(f.id) || (f.spec && el(resolve(f.spec).id));
    if (!now || !now.checked) fail(`${f.id}: didn't stay ticked`);
    return "checked";
  }

  async function lookup(f, value) {
    const parts = value.split(/\s+/);
    const [dug, street] = parts.length > 1 && steps.dug.includes(parts[0].toUpperCase())
      ? [parts[0].toUpperCase(), parts.slice(1).join(" ")] : ["ALTRO", value];
    const words = street.toUpperCase().split(/\W+/).filter((w) => w.length > 2);
    const e = el(f.id) || fail(`${f.id}: no field`);
    checkLabel(e, f.label);
    // The longest word first; the list shows only the first 20 streets, so a common one
    // ("Francesco") may leave the street out: then the next word.
    let lis = [], items = [], hits = [];
    for (const query of [...words].sort((a, b) => b.length - a.length)) {
      e.value = query;
      (widgetOf(e) || fail(`${f.id}: no lookup widget`)).search(query);
      await idle();
      lis = [...el(f.id.replace(/_input$/, "_panel")).querySelectorAll("li")];
      items = lis.map((l) => norm(l.textContent).toUpperCase());
      hits = items.filter((t) => t.split(" ")[0] === dug && words.every((w) => t.split(" ").includes(w)));
      // "Via Foria" is also inside "Via Santa Maria Avvocata a Foria": the street named exactly wins.
      const exact = hits.filter((t) => t === norm(`${dug} ${street}`).toUpperCase());
      if (hits.length > 1 && exact.length === 1) hits = exact;
      if (hits.length) break;
    }
    if (hits.length !== 1) fail(`${f.id}: the value matches ${hits.length} of ${items.length} listed`);
    press(lis[items.indexOf(hits[0])], `${f.id}: ${hits[0]}`);
    await idle();
    // The list can stay open a moment over the next boxes: wait for it, else close it.
    const panel = el(f.id.replace(/_input$/, "_panel"));
    for (const end = performance.now() + 3000; performance.now() < end && panel.offsetParent;) await sleep(50);
    if (panel.offsetParent) { widgetOf(e).hide(); await idle(); }
    return hits[0];
  }

  // A file too big for run.js comes by link ({name, type, url, size, sha256}): fetched here,
  // in the portal's page, and held to the size and hash run.py read from the person's copy.
  async function fetched(v) {
    let res;
    try { res = await fetch(v.url, { credentials: "omit", cache: "no-store" }); }
    catch (e) { fail(`${v.name}: couldn't fetch ${v.url} (${e.message})`); }
    if (!res.ok) fail(`${v.name}: ${v.url} answered ${res.status}`);
    const bytes = new Uint8Array(await res.arrayBuffer());
    const sha = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)))
      .map((b) => b.toString(16).padStart(2, "0")).join("");
    if (bytes.length !== v.size || sha !== v.sha256) fail(`${v.name}: the link served other bytes (${bytes.length} B)`);
    return bytes;
  }

  // v is {name, type, b64} or a link (fetched above). The portal's own change handler then
  // uploads it, as when a person picks the file.
  async function attach(f, v) {
    const e = (f.css ? doc().querySelector(f.css) : el(f.id)) || fail(`${f.css || f.id}: no file box`);
    if (e.type !== "file") fail(`${f.css || f.id}: not a file box`);
    checkLabel(e, f.label);
    const w = win(), bytes = v.url ? await fetched(v) : Uint8Array.from(w.atob(v.b64), (c) => c.charCodeAt(0));
    const dt = new w.DataTransfer();
    dt.items.add(new w.File([bytes], v.name, { type: v.type }));
    e.files = dt.files;
    fire(e);
    await idle();
    // An upload that isn't jQuery's doesn't show as busy: give it until its list names
    // the file. A portal that names it only after Salva costs these 10 s once.
    for (const end = performance.now() + 10000; performance.now() < end &&
      !norm(doc().body.innerText).includes(norm(v.name));) await sleep(100);
    // Some slots ("Allegati liberi") want a description next to the uploaded file: a box
    // with no id of its own in the slot's cell.
    if (f.describe) {
      const box = [...(el(f.id) || e).closest("td").querySelectorAll("input[type=text], textarea")].find((x) => !/dropzone/.test(x.id));
      if (!box) fail(`${f.label}: no description box`);
      box.focus(); box.value = f.describe; fire(box); box.blur();
      await idle();
    }
    return v.name;
  }

  function readBack(f) {
    if (f.spec) f = { ...f, id: resolve(f.spec).id }; // rows added since may have renumbered it
    else if (f.id && f.label && !el(f.kind === "select" ? f.id + ":combo_input" : f.id)) {
      f = { ...f, id: resolve({ kind: f.kind, label: f.label, nth: f.nth }).id }; // as when it was filled
    }
    if (f.kind === "file") {
      // An upload widget empties its box once it has sent the file; its list names it.
      const got = norm(doc().body.innerText).includes(norm(f.typed)) ? f.typed : null;
      return { label: f.label || f.css || f.id, want: f.typed, got, ok: got !== null };
    }
    if (f.kind === "radio" || f.kind === "checkbox") {
      const got = el(f.id) && el(f.id).checked ? "checked" : "unchecked";
      return { label: f.label, want: "checked", got, ok: got === "checked" };
    }
    const e = el(f.kind === "select" ? f.id + ":combo_input" : f.id);
    const got = e ? e.value : null;
    let ok = norm(got) === norm(f.typed);
    if (!ok && f.kind === "number") ok = parseFloat(got) === parseFloat(f.typed);
    return { label: f.label || f.id, want: f.typed, got, ok };
  }

  // A field named by its label instead of an id: the portal numbers ids by row, so they
  // shift when a choice or a "+" adds rows. `anchor` + `block` pick the block-th repeat of
  // a group (the first field of each repeat carries the anchor label); `nth` the n-th field
  // with that label inside it. Returns the id, as the steps file's `id` would be.
  function shown(e) {
    return !!(e.offsetParent || (e.closest(".ui-radiobutton, .ui-chkbox, .ui-selectonemenu") || {}).offsetParent);
  }
  function kindOf(e) {
    if (e.tagName === "SELECT") return "select";
    if (e.type === "radio" || e.type === "checkbox" || e.type === "file") return e.type === "checkbox" ? "checkbox" : e.type;
    if (/txtws_input$/.test(e.id)) return "lookup";
    return "text";
  }
  function inputs() {
    return [...doc().querySelectorAll('[id*="dynaForm:"]')].filter((e) => /^(INPUT|SELECT|TEXTAREA)$/.test(e.tagName) &&
      e.type !== "hidden" && !/combo_focus$|combo_filter$|editor_file_dropzone$/.test(e.id) && shown(e));
  }
  // A box's validation message ("Il valore è obbligatorio", as its title and in its cell) is
  // not its label.
  const MESSAGE = /^(il valore e obbligatorio|deve essere selezionata)/;
  // Where label is among the box's first texts (0: its own), or -1.
  function labelRank(e, label) {
    const texts = win().eval(`(${steps.label_js})`)(e);
    if (e.title) texts.unshift(e.title);
    // Only the messages in front: an empty cell keeps its place, or the label of the box to
    // the left would count as this box's.
    while (texts.length && MESSAGE.test(norm(texts[0]))) texts.shift();
    // A text said twice (the title "mq" and its cell "mq") counts once, so the third is the
    // label beside it ("Superficie di somministrazione").
    return texts.filter((t, i) => !t || texts.indexOf(t) === i).slice(0, 3).findIndex((t) => norm(t).startsWith(norm(label)));
  }
  const labelled = (e, label) => labelRank(e, label) >= 0;
  function resolve(f) {
    if (f.id || f.css) return f;
    let list = inputs();
    if (f.anchor) {
      const starts = list.map((e, i) => (labelled(e, f.anchor) ? i : -1)).filter((i) => i >= 0);
      const b = (f.block || 1) - 1;
      if (b >= starts.length) fail(`${f.label}: no block ${f.block} of ${f.anchor} (found ${starts.length})`);
      list = list.slice(starts[b], starts[b + 1]);
    }
    const want = f.kind === "number" || f.kind === "date" ? "text" : f.kind;
    let hits = list.filter((e) => kindOf(e) === want && labelled(e, f.label));
    // A box is counted by its own label (its title, else its cell), as the crawl counts it: a
    // "Si" whose nearby texts include the "No" before it is not a "No" (three Si/No pairs in a
    // row); the beds total under a shared header is not the rooms total.
    const ownLabel = (e) => [e.title, ...win().eval(`(${steps.label_js})`)(e)].find((t) => norm(t) && !MESSAGE.test(norm(t)));
    const own = hits.filter((e) => norm(ownLabel(e)).startsWith(norm(f.label)));
    if (own.length >= (f.nth || 1)) hits = own; // else a box labelled from above ("Provincia" of birth) counts too
    else if (hits.length > 1) {
      // Else the boxes that have the label first among their texts, and nth counts those: in a
      // row of boxes the last column's label is the one before each in the page ("Ragione
      // sociale" would count as the firm's second "codice fiscale").
      const best = Math.min(...hits.map((e) => labelRank(e, f.label)));
      hits = hits.filter((e) => labelRank(e, f.label) === best);
    }
    const e = hits[(f.nth || 1) - 1] || fail(`${f.label}: ${hits.length} fields with this label${f.anchor ? ` in block ${f.block || 1}` : ""}`);
    return { ...f, id: e.id.split("dynaForm:")[1].replace(/:combo_input$/, "") };
  }
  async function addBlock(f) {
    const count = () => inputs().filter((e) => labelled(e, f.anchor)).length;
    const before = count();
    // `block`: the block this "+" is for. A resumed draft may have it already.
    if (f.block && before >= f.block) return `block ${f.block}`;
    const plus = [...doc().querySelectorAll('[id*="dynaForm"] button')].filter((b) => shown(b) && b.textContent.trim() === "+");
    if (!plus.length) fail(`no + to add a block of ${f.anchor}`);
    press(plus[plus.length - 1], `+ ${f.anchor}`);
    await idle();
    if (count() !== before + 1) fail(`+ didn't add a block of ${f.anchor}`);
    return `block ${before + 1}`;
  }

  // The open section has no link: a reopened draft shows the one it was left on.
  const isOpen = (name) => {
    const cur = doc().querySelector("li#corrente");
    return !!cur && norm(cur.innerText).startsWith(norm(name));
  };
  const linkTo = (name) => [...doc().querySelectorAll("a.linkmenu")].find((x) => norm(x.textContent).startsWith(norm(name)));
  // Opens a section by its link. The Scheda anagrafica's reloads the whole page, and act()
  // can come back before the new page shows it (a fresh load read the old one): wait until
  // the section is the open one, its link gone from the menu and nothing loading (a page
  // that marks no open section, li#corrente, only the loading).
  async function openSection(a, name, ms = 20000) {
    await act(() => press(a, name));
    const end = performance.now() + ms;
    while (busy() || (doc().querySelector("li#corrente") && linkTo(name) && !isOpen(name))) {
      if (performance.now() > end) fail(`${name}: the section didn't open`);
      await sleep(100);
    }
    await idle();
  }

  async function section(sec, values, si) {
    const t0 = performance.now();
    report.at = { phase: "section", index: si, section: sec.section };
    if (steps.declarations.test(sec.section)) fail(`refused to open ${sec.section}: the person's declarations`);
    if (sec.go) {
      // A screen after the form (Pagamento oneri, Allega documenti): reached with its button,
      // unless a resumed draft is already on it.
      const here = () => norm((doc().querySelector(".content-title") || {}).innerText).includes(norm(sec.section));
      if (!here()) await act(() => press(button(sec.go), sec.go));
      if (!here()) fail(`expected the screen ${sec.section}`);
    } else if (sec.open !== false) {
      const a = linkTo(sec.section);
      if (a) await openSection(a, sec.section);
      else if (!isOpen(sec.section)) fail(`no section ${sec.section}`);
    }
    const done = [];
    for (let [i, f] of sec.fields.entries()) {
      const v = values[i];
      if (v === false) continue; // its `when` is false for this pratica
      report.at.field = i;
      report.at.label = f.label || f.id || f.css;
      let typed;
      if (f.kind === "add") { await addBlock(f); continue; }
      // An id a combination of choices moved (one the crawl didn't try together) is found
      // again by its label, held to the same checks.
      if (f.id && f.label && !el(f.kind === "select" ? f.id + ":combo_input" : f.id)) {
        const { id, ...byLabel } = f;
        f = byLabel;
      }
      if (!f.id && !f.css && f.if_present) {
        // A box only some pratiche show: skipped where this one doesn't.
        try { f = { ...resolve(f), spec: f }; } catch (e) { if (e instanceof StepError) continue; throw e; }
      }
      f = f.id || f.css ? f : { ...resolve(f), spec: f };
      if (f.kind === "radio" || f.kind === "checkbox") typed = await tick(f);
      else if (v === null || v === "") continue;
      else if (f.kind === "select") typed = await select(f, v);
      else if (f.kind === "lookup") typed = await lookup(f, v);
      else if (f.kind === "file") typed = await attach(f, v);
      else typed = await text(f, v);
      done.push({ ...f, kind: f.kind === "date" ? "text" : f.kind, typed, field: i });
    }
    delete report.at.field;
    delete report.at.label;
    // An attachments screen may keep each file as it uploads, with no Salva (save: false).
    if (sec.save !== false) await act(() => press(button(sec.save || "Salva"), sec.save || "Salva"));
    const errs = errors(sec.theirs);
    const checks = done.map(readBack);
    const bad = checks.filter((c) => !c.ok);
    details.push(...bad.map((c) => ({ section: sec.section, ...c })));
    const gone = bad.map(redact);
    report.sections.push({ section: sec.section, seconds: +((performance.now() - t0) / 1000).toFixed(1), errors: errs, bad: gone });
    if (errs.length || bad.length) fail(`${sec.section}: portal errors ${JSON.stringify(errs)}, not kept ${JSON.stringify(gone)}`);
    keep(si, done);
  }

  // What each finished section typed, in the report too: a run resumed after a stop hands
  // it back, so the fresh load of the draft still checks every section, not only its own.
  function keep(si, done) {
    kept = kept.filter((k) => k.sec !== steps.sections[si]).concat([{ sec: steps.sections[si], done }]);
    report.kept = (report.kept || []).filter((k) => k.section !== si)
      .concat([{ section: si, fields: done.map((d) => [d.field, d.typed]) }]);
  }

  // Each read-back above saw its section right after its own Salva. A later section can
  // re-render an earlier one, and a portal can show a value it never stored, so the
  // draft is loaded again from its address and every value read once more.
  async function verify() {
    const t0 = performance.now();
    // Going to the same address plus a #section only scrolls, so drop that part; a route
    // after # (#/…, #!…) is the page itself, so reload it instead.
    const u = new (win().URL)(doc().location.href);
    const route = /^#[!/]/.test(u.hash);
    if (!route) u.hash = "";
    await act(() => (route ? win().location.reload() : win().location.replace(u.href)));
    // Every section is saved by now: the person can open the draft from Pratiche in bozza.
    if (/\/cmd\/error/.test(doc().location.href)) fail("fresh load of the draft: the portal's error page (the sections are saved; open the draft from Pratiche in bozza)");
    const bad = [];
    for (const { sec, done } of kept.filter((k) => !k.sec.go)) {
      const a = linkTo(sec.section);
      if (a) await openSection(a, sec.section);
      else if (sec.open !== false && !isOpen(sec.section)) fail(`fresh load of the draft: no section ${sec.section}`);
      for (const c of done.map(readBack)) if (!c.ok) bad.push({ section: sec.section, ...c });
    }
    report.verify_seconds = +((performance.now() - t0) / 1000).toFixed(1);
    if (bad.length) {
      details.push(...bad);
      fail(`fresh load of the draft: not kept ${JSON.stringify(bad.map(({ section, ...c }) => ({ section, ...redact(c) })))}`);
    }
  }

  // The declarations are the person's to tick, so they should read the portal's own words, not
  // a summary of them. Each declarations section is opened (a link, as a person opens it) and
  // its text copied, with every tick box's label and whether it is ticked. Nothing in it is
  // clicked, typed or saved. A failure is a note in the report, never a stop: this can't stand
  // between a saved draft and the person. The text may carry their name, so it is held like the
  // values (held(), not the report) and run.py writes it only for the review page.
  async function readDeclarations() {
    const names = [...doc().querySelectorAll("a.linkmenu")].map((a) => a.textContent.replace(/\s+/g, " ").trim())
      .filter((n) => steps.declarations.test(n));
    const back = kept.filter((k) => !k.sec.go).map((k) => k.sec.section).pop();
    declared = [];
    try {
      for (const name of names) {
        await openSection(linkTo(name), name, 8000);
        const cur = doc().querySelector("li#corrente");
        if (!cur) fail(`${name}: no open section to read`);
        // A box's own <label> first, then a radio's (PrimeFaces keeps it beside the button, in a div
        // with a title), then the steps' label_js, which finds the portal's label in the row.
        const label = (e) => {
          if (e.labels && e.labels[0] && e.labels[0].innerText.trim()) return e.labels[0].innerText;
          const own = e.type === "radio" && e.closest("div[title]") && e.closest("div[title]").querySelector("label");
          if (own && own.innerText.trim()) return own.innerText;
          try { return win().eval(`(${steps.label_js})`)(e)[0] || ""; } catch (x) { return ""; }
        };
        // The portal's Salva and Annulla are its buttons, not the declaration's words.
        const buttons = new Set([...cur.querySelectorAll("button, input[type=submit]")]
          .map((b) => (b.innerText || b.value || "").trim()).filter(Boolean));
        const text = cur.innerText.split("\n").filter((l) => !buttons.has(l.trim())).join("\n");
        declared.push({ section: name, text: text.replace(/\n{3,}/g, "\n\n").trim().slice(0, 20000),
          // A radio is two inputs (the real one and its `_clone`): one box to the person.
          boxes: [...cur.querySelectorAll("input[type=checkbox], input[type=radio]")].filter((e) => !/_clone$/.test(e.id))
            .map((e) => ({ label: label(e).replace(/\s+/g, " ").trim().slice(0, 300), kind: e.type, ticked: e.checked })) });
      }
      report.declarations_read = declared.length;
    } catch (e) {
      report.declarations_read = declared.length;
      report.declarations_note = scrub(e instanceof StepError ? e.message : `${e.name}: ${e.message}`);
    }
    // Back on the section the steps left open, whatever happened above.
    try {
      const a = names.length && back && linkTo(back);
      if (a) await openSection(a, back, 8000);
    } catch (e) { report.declarations_note = report.declarations_note || scrub(`could not go back to ${back}: ${e.message}`); }
  }

  // The portal's own PDF of the draft (what "Visualizza anteprima" shows). A plain GET with the
  // person's session once a draft is open: pressing the button instead makes the whole page
  // navigate to it, which would end this run's page and the review drawn over it.
  async function fetchPreview() {
    previewBlob = null;
    delete report.preview_note;
    try {
      const r = await fetch(new URL(steps.preview_url || "/fnsuap/preview.do", doc().location.href).href, { credentials: "include" });
      if (!r.ok) fail(`the portal answered ${r.status}`);
      const bytes = new Uint8Array(await r.arrayBuffer());
      if (String.fromCharCode(...bytes.slice(0, 5)) !== "%PDF-") fail("what it sent isn't a PDF");
      previewBlob = new Blob([bytes], { type: "application/pdf" });
    } catch (e) {
      report.preview_note = scrub(e instanceof StepError ? e.message : `${e.name}: ${e.message}`);
    }
    report.preview_pdf = !!previewBlob;
  }

  // What the person reads before they tick, drawn over the portal in this page from what the run
  // holds in memory (the declarations' text, every value typed): none of it passes through Claude,
  // and they read it in the window where they will tick and sign. Text goes in with textContent
  // only. "Show the draft" takes it away; so does the next start().
  function showReview() {
    document.querySelectorAll('[data-pv="review"]').forEach((e) => e.remove());
    const el = (tag, css, text, ...kids) => {
      const e = document.createElement(tag);
      if (css) e.style.cssText = css;
      if (text != null) e.textContent = text;
      e.append(...kids);
      return e;
    };
    const btn = (text, on) => {
      const b = el("button", "font:inherit;padding:6px 14px;margin:0 8px 8px 0;cursor:pointer", text);
      b.type = "button";
      b.addEventListener("click", on);
      return b;
    };
    // In a shadow root: the portal's own CSS (red headings, styled buttons) must not reach it.
    const host = el("div", "all:initial;position:fixed;inset:0;z-index:2147483647");
    host.dataset.pv = "review";
    const shadow = host.attachShadow({ mode: "open" });
    shadow.append(el("style", null, "*{box-sizing:border-box} button,summary{font:inherit} h1,h2,p,ul,table{margin:0}"));
    const root = el("div", "position:absolute;inset:0;overflow:auto;background:Canvas;color:CanvasText;" +
      "font:15px/1.5 -apple-system,system-ui,sans-serif;padding:24px 16px");
    const page = el("div", "max-width:820px;margin:0 auto");
    const note = el("p", "color:GrayText;margin:0 0 8px");
    const say = (t) => { note.textContent = t; };
    page.append(el("h1", "font-size:22px;margin:0 0 4px", "Draft saved on the portal"),
      el("p", "margin:0 0 12px", "Nothing has been declared, signed, paid or sent. Read the declarations below as the portal words " +
        "them before you tick any: you make them in your own name, and a false one is a crime (DPR 445/2000, art. 76)."),
      btn("Show the draft", () => host.remove()),
      note);
    page.append(el("h2", "font-size:17px;margin:24px 0 8px", "The declarations you will tick"));
    if (!declared.length) {
      page.append(el("p", "color:GrayText", "Not copied" + (report.declarations_note ? ` (${report.declarations_note})` : "") +
        ": open each declarations section in the portal and read it there."));
    }
    for (const d of declared) {
      const lines = d.boxes.map((b) => el("li", b.ticked ? "color:#c62828;font-weight:600" : "",
        `${b.ticked ? "☑ already ticked: " : "☐ "}${b.label}`));
      page.append(el("details", "border:1px solid GrayText;border-radius:8px;margin:8px 0", null,
        el("summary", "cursor:pointer;padding:8px 12px;font-weight:600", d.section),
        el("div", "white-space:pre-wrap;padding:4px 12px 10px;overflow-wrap:anywhere", d.text),
        el("ul", "list-style:none;margin:0;padding:0 12px 10px", null, ...lines)));
      page.lastChild.open = true;
    }
    page.append(el("h2", "font-size:17px;margin:24px 0 8px", "The portal's preview"));
    if (previewBlob) {
      const href = URL.createObjectURL(previewBlob);
      const dl = el("a", "display:inline-block;margin:0 0 8px", "Download the PDF");
      dl.href = href;
      dl.download = "anteprima.pdf";
      const pdf = el("iframe", "width:100%;height:80vh;border:1px solid GrayText;border-radius:8px");
      pdf.src = href;
      page.append(el("p", "color:GrayText;margin:0 0 4px", "The document the portal will file, as it renders it."), dl, pdf);
    } else {
      page.append(el("p", "color:GrayText", `Not copied${report.preview_note ? ` (${report.preview_note})` : ""}: press Visualizza anteprima in the portal and read it before you sign.`));
    }
    page.append(el("h2", "font-size:17px;margin:24px 0 8px", "Every value we typed"));
    for (const { sec, done } of kept.filter((k) => !k.sec.go)) {
      const rows = done.map((f) => el("tr", "", null, el("td", "color:GrayText;padding:3px 12px 3px 0;width:42%", f.label || f.id || f.css || ""),
        el("td", "padding:3px 0;overflow-wrap:anywhere", f.kind === "radio" || f.kind === "checkbox" ? "chosen" : String(f.typed ?? ""))));
      page.append(el("details", "border:1px solid GrayText;border-radius:8px;margin:8px 0", null,
        el("summary", "cursor:pointer;padding:8px 12px;font-weight:600", sec.section),
        el("table", "width:100%;border-collapse:collapse;padding:0 12px", null, ...rows)));
    }
    root.append(page);
    shadow.append(root);
    document.body.append(host);
  }

  async function run(values, resume) {
    const t0 = performance.now();
    const secs = () => +((performance.now() - t0) / 1000).toFixed(1);
    try {
      frame = document.createElement("iframe");
      frame.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;border:0;z-index:2147483647;background:#fff";
      frame.dataset.pv = "runner";
      const loaded = new Promise((r) => frame.addEventListener("load", r, { once: true }));
      // A resumed run opens the draft it stopped on; a new one starts a new pratica.
      frame.src = resume.url || steps.start;
      document.body.append(frame);
      await loaded;
      await idle();
      if (!doc().location.href.startsWith(steps.logged_in)) fail("not logged in to the portal");
      if (/\/cmd\/error/.test(doc().location.href)) fail("the draft's address has expired: open it again from Pratiche in bozza");
      const from = resume.from || {};
      for (const k of resume.kept || []) {
        keep(k.section, k.fields.map(([field, typed]) => {
          const def = steps.sections[k.section].fields[field];
          return { ...def, typed, field, ...(def.id || def.css ? {} : { spec: def }) };
        }));
      }
      if (from.section === undefined) {
        for (const [i, w] of steps.wizard.entries()) if (i >= (from.wizard || 0)) await wizard(w, values.wizard[i], i);
      }
      report.wizard_seconds = secs();
      for (const [i, s] of steps.sections.entries()) {
        if (!s.go && i >= (from.section || 0)) await section(s, values.sections[i], i);
      }
      if (steps.sections.some((s, i) => !s.go && i >= (from.section || 0)) || !steps.sections.some((s) => s.go)) {
        await verify();
        await readDeclarations();
      }
      for (const [i, s] of steps.sections.entries()) {
        if (s.go && i >= (from.section || 0)) await section(s, values.sections[i], i);
      }
      // Before `filled`: whoever polls the report (run.py, then previewPdf) must find it done.
      await fetchPreview();
      report.result = "filled";
      report.at = null;
      try { showReview(); } catch (e) { /* the draft is saved; the person can still read it in the portal */ }
    } catch (e) {
      report.result = "stopped";
      report.error = e instanceof StepError ? e.message : `${e.name}: ${e.message}`;
      try { report.snapshot = look(); } catch (x) { /* the page is gone */ }
    } finally {
      report.total_seconds = secs();
      report.url = frame && doc() ? doc().location.href : null;
      try {
        sessionStorage.setItem("pv-report", JSON.stringify(publicReport()));
        // Only a stopped run is resumed, and only it needs what its sections typed.
        if (report.result === "filled") { sessionStorage.removeItem("pv-run"); sessionStorage.removeItem("pv-held"); }
        else {
          const held = { kept: report.kept || [] };
          sessionStorage.setItem("pv-held", JSON.stringify(
            sealedRun ? await sealAgain(sealedRun.keys, utf8.encode(JSON.stringify(held))) : held));
        }
      } catch (e) { /* storage off */ }
    }
  }

  // The page in the frame with no value in it: enough to find a moved id or a renamed label
  // (each visible box, its label, whether it holds anything) without reading what it holds.
  function look() {
    if (!frame || !doc()) return null;
    const label = (e) => {
      try { return scrub(norm(win().eval(`(${steps.label_js})`)(e)[0] || "")).slice(0, 80); } catch (x) { return ""; }
    };
    const boxes = [...doc().querySelectorAll("input, select, textarea")]
      .filter((e) => e.type !== "hidden" && (e.type === "file" || shown(e))).slice(0, 150)
      .map((e) => {
        const kind = kindOf(e);
        const state = kind === "radio" || kind === "checkbox" ? (e.checked ? "checked" : "unchecked")
          : kind === "file" ? "file" : e.value ? "filled" : "empty";
        return { id: e.id.replace(/^.*dynaForm:/, ""), label: label(e), kind, state,
          ...(kind === "select" ? { options: e.options.length } : {}) };
      });
    const open = doc().querySelector("li#corrente");
    return { section: open ? scrub(norm(open.textContent)).slice(0, 80) : null, boxes,
      messages: errors().map(scrub).slice(0, 10) };
  }

  // The portal's own hiccups: its error page ("errore inatteso"), a section slow to open or
  // to show its boxes, a busy queue. A run stopped by one picks itself up on the same draft,
  // as a person would by reloading (at most `retries` times, 2 by default); anything else (a
  // value the portal refuses or doesn't keep, a label it renamed) still stops, so a real
  // change is never retried into a misfill. Each retry is in the report's `retries`.
  const TRANSIENT = /didn't open|stayed busy|fields with this label|expected the screen|no section |error page|has expired/;
  async function attempt(values, resume) {
    for (let n = 0; ; n++) {
      await run(values, resume);
      const prev = report, at = prev.at, onError = /\/cmd\/error/.test(prev.url || "");
      if (prev.result !== "stopped" || !at || n >= (resume.retries ?? 2) || !(onError || TRANSIENT.test(prev.error || ""))) return;
      // Still running for wait(): the retry is part of this run.
      const retries = (prev.retries || []).concat([{ error: prev.error, at }]);
      report = { ...prev, result: "running", error: undefined, snapshot: undefined, retries, resumed: true };
      await sleep(resume.retry_wait_ms ?? 10000);
      let url = null;
      if (at.phase === "section") {
        // A draft whose address is still good goes on from there; one on the error page is
        // opened again from Pratiche in bozza by its name.
        url = onError ? null : prev.url;
        if (!url) {
          try {
            url = await reopen(values.wizard[0], location.origin + "/fnsuap/cmd/home");
          } catch (e) {
            report = { ...prev, retries, error: `${prev.error}; then reopening the draft: ${e.message}` };
            try { sessionStorage.setItem("pv-report", JSON.stringify(report)); } catch (_) { /* storage off */ }
            return;
          }
        }
      }
      document.querySelectorAll('iframe[data-pv="runner"]').forEach((f) => f.remove());
      if (url) {
        resume = { ...resume, url, from: { section: at.index }, kept: prev.kept || [] };
      } else {
        // Stopped in the wizard: a new pratica, from the start.
        resume = { ...resume, url: undefined, from: {}, kept: [] };
        kept = [];
        report.sections = [];
        report.kept = [];
      }
    }
  }

  // The draft named name, opened from the list of Pratiche in bozza (ten a page; the newest
  // first). Its address then is the home's, with its own execution key.
  async function reopen(name, home) {
    document.querySelectorAll('iframe[data-pv="runner"]').forEach((f) => f.remove());
    frame = document.createElement("iframe");
    frame.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;border:0;z-index:2147483647;background:#fff";
    frame.dataset.pv = "runner";
    const loaded = new Promise((r) => frame.addEventListener("load", r, { once: true }));
    frame.src = home;
    document.body.append(frame);
    await loaded;
    await idle();
    const row = () => [...doc().querySelectorAll("tr")].find((t) => norm(t.innerText).includes(norm(name)));
    for (let p = 2; !row(); p++) {
      const next = [...doc().querySelectorAll(".ui-paginator-page")].find((a) => a.textContent.trim() === String(p));
      if (!next) fail(`no draft named ${name} in Pratiche in bozza`);
      next.click();
      await idle();
    }
    const open = [...row().querySelectorAll("a, button")].find((b) => norm(b.title || b.textContent) === "apri") || fail(`${name}: no Apri`);
    await act(() => open.click());
    for (const end = performance.now() + 20000; !doc().querySelector("li#corrente");) {
      if (performance.now() > end) fail(`${name}: the draft didn't open`);
      await sleep(200);
    }
    await idle();
    return doc().location.href;
  }

  window.__pv = {
    start(procedure, values, resume = {}, sealed = null) {
      const raw = localStorage.getItem("pv-steps:" + procedure) || fail(`no steps installed for ${procedure}`);
      // One frame at a time: a run that ended left its own over the page, maybe from a
      // runner loaded before this one (and its review, drawn over the portal).
      document.querySelectorAll('iframe[data-pv="runner"], [data-pv="review"]').forEach((f) => f.remove());
      if (!sealed) sealedRun = null;
      try { sessionStorage.setItem("pv-run", JSON.stringify(sealed ? { procedure, sealed } : { procedure, values })); }
      catch (e) { /* storage off */ }
      steps = JSON.parse(raw);
      steps.forbidden = new RegExp(steps.forbidden, "i");
      steps.declarations = new RegExp(steps.declarations, "i");
      hide(values);
      details = [];
      declared = [];
      // A fix is what Claude found the page really has for one field (a moved id, a renamed
      // label): this run uses it, and run.py writes it down for the steps file.
      for (const x of resume.fixes || []) Object.assign(steps.sections[x.section].fields[x.field], x.set);
      report = { procedure, result: "running", sections: [], at: null, resumed: !!resume.url };
      kept = [];
      attempt(values, resume);
      return "started";
    },
    // The values sealed under a code (run.py page): the run starts once the person has typed it.
    startSealed(procedure, sealed) {
      report = { procedure, result: "waiting", sections: [], at: null,
        waiting: "for the person to type the code shown in the pvault window into the portal page" };
      (async () => {
        try {
          const { keys, plain } = await unlock(sealed);
          sealedRun = { sealed, keys, vals: plain.vals };
          this.start(procedure, plain.vals, plain.resume || {}, sealed);
        } catch (e) {
          report.result = "stopped";
          report.error = e instanceof StepError ? e.message : `${e.name}: ${e.message}`;
        }
      })();
      return "waiting for the code";
    },
    async wait(ms = 25000) {
      const end = performance.now() + ms;
      while (report && (report.result === "running" || report.result === "waiting") && performance.now() < end) await sleep(200);
      return report && publicReport();
    },
    open() {
      // The draft's own address: a preview that loaded in the frame would have moved it.
      location.href = (this.last() || {}).url || doc().location.href;
    },
    // The review over the portal again (run() draws it when a run is filled).
    review() {
      showReview();
      return "shown";
    },
    // The portal's PDF kept in memory by a filled run, as base64: for run.py, never for Claude.
    async previewPdf() {
      if (!previewBlob) return null;
      return new Promise((ok, no) => {
        const f = new FileReader();
        f.onload = () => ok(String(f.result).split(",")[1]);
        f.onerror = () => no(f.error);
        f.readAsDataURL(previewBlob);
      });
    },
    last() {
      return report ? publicReport() : JSON.parse(sessionStorage.getItem("pv-report") || "null");
    },
    look,
    // What the reports leave out. run.py writes it for the person; Claude doesn't read it.
    held() {
      return { kept: report ? report.kept || [] : [], details, declarations: declared };
    },
    resume({ fixes = [], url } = {}) {
      // As run.py's resumption(): the draft, the section it stopped in, what came before.
      const last = this.last(), run = JSON.parse(sessionStorage.getItem("pv-run") || "null");
      if (!last || !run || !last.at || !(url || last.url)) fail("no stopped run in this tab to resume");
      const from = { [last.at.phase === "section" ? "section" : "wizard"]: last.at.index };
      const stored = JSON.parse(sessionStorage.getItem("pv-held") || "null");
      const go = (values, kept) => this.start(run.procedure, values, { url: url || last.url, from, kept, fixes }, run.sealed || null);
      if (!run.sealed) return go(run.values, (report && report.kept) || (stored && stored.kept) || []);
      if (sealedRun) return go(sealedRun.vals, (report && report.kept) || []);
      // The runner was reloaded and the values are sealed again: ask for the code again.
      report = { procedure: run.procedure, result: "waiting", sections: [], at: null,
        waiting: "for the person to type the code shown in the pvault window into the portal page" };
      (async () => {
        try {
          const { keys, plain } = await unlock(run.sealed);
          sealedRun = { sealed: run.sealed, keys, vals: plain.vals };
          go(plain.vals, stored ? JSON.parse(utf8d.decode(await openSealed(keys, stored))).kept : []);
        } catch (e) {
          report.result = "stopped";
          report.error = e instanceof StepError ? e.message : `${e.name}: ${e.message}`;
        }
      })();
      return "waiting for the code";
    },
    async reopen(name, home = location.origin + "/fnsuap/cmd/home") {
      return reopen(name, home);
    },
  };
})();
