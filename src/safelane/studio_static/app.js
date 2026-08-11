"use strict";

const app = document.querySelector("#app");
const toastElement = document.querySelector("#toast");
const repositoryDialog = document.querySelector("#repository-dialog");
const repositoryForm = document.querySelector("#repository-form");
const repositoryInput = document.querySelector("#repository-source");
const repositoryError = document.querySelector("#repository-error");
const state = { dashboard: null, filter: "needs_review" };

const escapeHtml = (value) => String(value ?? "")
  .replaceAll("&", "&amp;")
  .replaceAll("<", "&lt;")
  .replaceAll(">", "&gt;")
  .replaceAll('"', "&quot;")
  .replaceAll("'", "&#039;");

async function api(path, options = {}) {
  const response = await fetch(path, { cache: "no-store", ...options });
  let body = {};
  try { body = await response.json(); } catch { /* server returned no JSON */ }
  if (!response.ok) {
    const error = new Error(body.message || body.error || `Request failed (${response.status})`);
    error.status = response.status;
    throw error;
  }
  return body;
}

function navigate(path) {
  history.pushState({}, "", path);
  renderRoute();
  window.scrollTo(0, 0);
}

function toast(message, type = "") {
  toastElement.textContent = message;
  toastElement.className = `toast ${type}`.trim();
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => toastElement.classList.add("hidden"), 3600);
}

function shell(content, active = "changes", narrow = false) {
  const dashboard = state.dashboard;
  const repository = dashboard?.repository || "Connecting…";
  const pending = dashboard?.counts?.needs_review ?? "–";
  return `<div class="shell">
    <aside class="sidebar">
      <a class="brand" href="/changes" data-nav aria-label="SafeLane Studio">
        <img class="brand-logo" src="/safelane-logo.svg" alt="SafeLane">
        <span class="brand-product">Studio</span>
      </a>
      <div class="nav-label">Workspace</div>
      <nav class="main-nav">
        <a class="nav-link ${active === "changes" ? "active" : ""}" href="/changes" data-nav>
          <span class="nav-icon">◇</span>Changes<span class="count">${pending}</span>
        </a>
        <a class="nav-link ${active === "profiles" ? "active" : ""}" href="/profiles" data-nav>
          <span class="nav-icon">▤</span>Profiles
        </a>
        <a class="nav-link ${active === "outcomes" ? "active" : ""}" href="/outcomes" data-nav>
          <span class="nav-icon">◎</span>Outcomes
        </a>
      </nav>
      <div class="repo-card"><span>Connected repository</span><strong>${escapeHtml(repository)}</strong><small>GitHub · open pull requests</small></div>
    </aside>
    <div class="workspace">
      <header class="topbar">
        <button class="project repository-switch" id="repository-switch" type="button" aria-haspopup="dialog">${escapeHtml(repository)} <span class="env">GitHub</span><span class="project-caret" aria-hidden="true">⌄</span></button>
        <div class="ai-status"><span class="online"></span>Source-bound local analysis</div>
      </header>
      <main class="content ${narrow ? "narrow" : ""}">${content}</main>
    </div>
  </div>`;
}

function loading(label = "Reading open pull requests…") {
  app.innerHTML = shell(`<section class="card loading-card"><span class="spinner" aria-hidden="true"></span>${escapeHtml(label)}</section>`);
}

function showError(message) {
  app.innerHTML = shell(`<section class="card empty"><strong>SafeLane could not load this view</strong><p>${escapeHtml(message)}</p><button class="button primary" id="retry">Try again</button></section>`);
  document.querySelector("#retry")?.addEventListener("click", renderRoute);
}

function formatUpdated(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const minutes = Math.max(0, Math.round((Date.now() - date.getTime()) / 60000));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  if (minutes < 1440) return `${Math.floor(minutes / 60)} hr ago`;
  return date.toLocaleDateString();
}

function row(change) {
  const tier = escapeHtml(change.tier);
  const profile = escapeHtml(change.profile);
  return `<a class="change-row" href="/changes/${change.number}" data-nav>
    <div class="change-copy">
      <div class="title-line"><span class="change-title">${escapeHtml(change.title)}</span><span class="row-risk ${tier}">${tier}</span></div>
      <span class="change-meta">PR #${change.number} · ${escapeHtml(change.head_ref)} · ${escapeHtml(change.author)} · ${escapeHtml(formatUpdated(change.updated_at))}</span>
      <div class="risk-line"><span class="category">${tier === "safe" ? "Bounded scope" : "Release risk"}</span><strong>${escapeHtml(change.reason)}</strong></div>
    </div>
    <div class="suggestion ${profile.toLowerCase()}"><span>Suggested lane</span><strong>${profile}</strong></div>
    <span class="arrow" aria-hidden="true">›</span>
  </a>`;
}

async function renderChanges() {
  loading();
  try {
    state.dashboard = await api("/api/dashboard");
  } catch (error) {
    showError(error.message === "repository_unavailable" ? "GitHub is unavailable or this repository cannot be read." : error.message);
    return;
  }
  const status = state.filter === "needs_review" ? "unresolved" : "resolved";
  const changes = state.dashboard.changes.filter((change) => status === "unresolved" ? change.review_status === "unresolved" : change.review_status !== "unresolved");
  const needs = state.dashboard.counts.needs_review;
  const resolved = state.dashboard.counts.resolved;
  const title = status === "unresolved" ? "Changes needing review" : "Resolved changes";
  const copy = status === "unresolved"
    ? "Guarded and risky pull requests wait here until their rollout is approved."
    : "Safe pull requests resolve automatically. Approved guarded and risky changes appear here too.";
  const empty = status === "unresolved"
    ? "No open pull requests need review."
    : "No open pull requests are resolved yet.";
  app.innerHTML = shell(`<header class="page-head"><div><div class="eyebrow">Change review</div><h1>${title}</h1><p>${copy}</p></div><span class="version">LIVE GITHUB DATA</span></header>
    <div class="tabs">
      <button class="tab ${status === "unresolved" ? "active" : ""}" data-filter="needs_review">Needs review · ${needs}</button>
      <button class="tab ${status === "resolved" ? "active" : ""}" data-filter="resolved">Resolved · ${resolved}</button>
    </div>
    <div class="list-head"><span>${changes.length} open PR${changes.length === 1 ? "" : "s"}</span><span>${status === "unresolved" ? "Risky first" : "Latest first"}</span></div>
    ${changes.length ? `<div class="change-list">${changes.map(row).join("")}</div>` : `<section class="card empty"><strong>${empty}</strong><p>SafeLane only shows changes after GitHub has an open pull request.</p></section>`}`);
  document.querySelectorAll("[data-filter]").forEach((button) => {
    button.addEventListener("click", () => { state.filter = button.dataset.filter; renderChanges(); });
  });
}

function tierBadge(tier) {
  return `<span class="tag ${escapeHtml(tier)}">${escapeHtml(tier)} tier</span>`;
}

function rolloutRail(profile) {
  return `<div class="stage-list">${profile.stages.map((stage, index) => `<div class="stage">
    <span class="stage-number">${String(index + 1).padStart(2, "0")}</span>
    <div class="stage-copy"><strong>${stage.exposure_pods} of ${profile.replicas} pods</strong><small>Weight ${stage.set_weight} · ${stage.analysis ? "trusted check" : "readiness"}</small></div>
  </div>`).join("")}</div>`;
}

function findingCards(assessment) {
  const findings = assessment.findings;
  if (!findings.length) {
    const status = assessment.evidence.ai_status;
    const evidenceState = {
      complete: {
        category: "AI assessment",
        title: "AI analysis completed — no source-verified finding",
        detail: "The local model returned no finding that survived exact changed-line verification.",
      },
      skipped_over_budget: {
        category: "Evidence limit",
        title: "AI analysis skipped — diff exceeded the evidence budget",
        detail: "SafeLane did not send this PR to the local model. The rollout lane comes from deterministic change-scope rules.",
      },
      skipped_invalid_diff: {
        category: "Unsupported evidence",
        title: "AI analysis skipped — diff is not valid UTF-8",
        detail: "The local model was not called because SafeLane could not safely decode the complete PR diff.",
      },
      skipped_binary_diff: {
        category: "Unsupported evidence",
        title: "AI analysis skipped — binary changes are unsupported",
        detail: "The local model was not called because the PR contains binary change evidence.",
      },
      unavailable: {
        category: "Evidence unavailable",
        title: "AI analysis unavailable",
        detail: "The local model could not be reached. SafeLane kept the deterministic policy floor.",
      },
      invalid: {
        category: "Evidence rejected",
        title: "AI response rejected",
        detail: "The local model responded, but its output did not satisfy SafeLane's bounded evidence contract.",
      },
      partial: {
        category: "Evidence rejected",
        title: "AI response rejected",
        detail: "At least one model claim failed exact source-reference validation, so it cannot affect the rollout lane.",
      },
    }[status] || {
      category: "Policy assessment",
      title: "No usable AI evidence",
      detail: "The rollout lane comes from deterministic change-scope and evidence rules.",
    };
    return `<section class="card main-risk neutral"><div class="risk-top"><span class="category">${evidenceState.category}</span><span class="evidence-code">${escapeHtml(status)}</span></div><h2>${evidenceState.title}</h2><p>${evidenceState.detail}</p></section>`;
  }
  return findings.map((finding) => `<section class="card main-risk">
    <div class="risk-top"><span class="category">${escapeHtml(finding.category)}</span><span class="verified">✓ AI finding · source references verified</span></div>
    <h2>${escapeHtml(finding.title)}</h2><p>${escapeHtml(finding.rationale)}</p>
    <div class="spans">${finding.spans.map((span) => `<div class="span-card ${escapeHtml(span.side)}"><div class="span-label"><span>${escapeHtml(span.side)}</span><span>${escapeHtml(span.file)}:${span.line}</span></div><code>${escapeHtml(span.text)}</code></div>`).join("")}</div>
  </section>`).join("");
}

function approvalPanel(assessment, token) {
  if (assessment.review.status !== "unresolved") {
    const automatic = assessment.review.resolution.type === "automatic";
    const rejected = assessment.review.status === "rejected";
    const compiler = rejected ? "" : `<div class="release-compiler"><label for="release-image">Immutable release image</label><input id="release-image" placeholder="ghcr.io/acme/service@sha256:…"><button class="button primary" id="compile">Compile Argo rollout</button></div>`;
    return `<section class="card approval"><div><div class="${rejected ? "rejected" : "approved"}">${rejected ? "✕ Rejected by reviewer" : `✓ ${automatic ? "Resolved automatically" : "Approved by reviewer"}`}</div>${compiler}<p id="approval-message" class="action-message"></p></div><div class="buttons"><a class="button" href="/changes" data-nav>Return to Changes</a></div></section>`;
  }
  const options = assessment.rollout_options.map((profile) => `<option value="${escapeHtml(profile.name)}">${escapeHtml(profile.name)}</option>`).join("");
  const only = assessment.rollout_options.length === 1 ? assessment.rollout_options[0].name : null;
  return `<section class="card approval"><div><h2>Review the backend proposal</h2><p>Approval authorizes compilation for this exact PR head. Rejection emits no rollout decision.</p></div><div class="buttons"><select id="selected-profile" aria-label="Selected rollout profile">${options}</select><button class="button primary" id="approve">${only === "Strict" ? "Approve Strict rollout" : "Approve selected rollout"}</button><button class="button danger" id="reject">Reject</button><a class="button" href="/changes" data-nav>Decide later</a></div><p id="approval-message" class="action-message"></p></section>`;
}

function rolloutPreview(profile, evidence, evidenceConfidence) {
  return `<section class="card rollout" id="rollout-preview"><div class="card-head"><div><h2>${escapeHtml(profile.name)} rollout</h2><p class="copy">The repository-owned policy defines this profile; backend safety floors select it.</p></div><span class="version">${profile.replicas} replicas</span></div>${rolloutRail(profile)}<div class="health"><div><span>Files</span><strong>${evidence.files_changed}</strong></div><div><span>Changed lines</span><strong>${evidence.lines_changed}</strong></div><div><span>AI evidence</span><strong>${escapeHtml(evidence.ai_status)}</strong></div><div><span>Evidence confidence</span><strong>${escapeHtml(evidenceConfidence)}</strong></div></div></section>`;
}

async function renderAssessment(number) {
  loading(`Opening PR #${number}…`);
  let snapshot;
  try { snapshot = await api(`/api/assessments/${number}`); }
  catch (error) { showError(error.message); return; }
  const assessment = snapshot.assessment;
  const change = assessment.change;
  const risk = assessment.risk;
  const profile = assessment.rollout_options[0];
  if (!state.dashboard) {
    state.dashboard = {
      repository: change.repository,
      approval_token: snapshot.approval_token,
      counts: { needs_review: "–", resolved: "–" },
      changes: [],
    };
  }
  const fresh = state.dashboard?.changes.find((item) => item.number === number)?.head_sha === change.head_sha;
  app.innerHTML = shell(`<nav class="breadcrumb"><a href="/changes" data-nav>Changes</a><span>›</span><span>${escapeHtml(state.dashboard?.repository || change.repository)} · PR #${number}</span></nav>
    <header class="assessment-head"><div><div class="eyebrow">Exact open PR revision</div><h1>${escapeHtml(change.title)}</h1><p>${escapeHtml(change.repository)} · ${escapeHtml(change.head_ref)} · ${escapeHtml(change.head_sha)}</p></div><div class="assessment-tags">${tierBadge(risk.tier)}</div></header>
    <div class="update">✓ This assessment covers ${fresh === false ? "the selected" : "the latest detected"} PR head. A new push requires a fresh assessment.</div>
    <section class="card decision"><div class="decision-value"><span>Backend proposal</span><strong>${escapeHtml(risk.minimum_profile)}</strong></div><div><h2>${assessment.review.status !== "unresolved" ? "This PR review is complete" : "Review required before this PR can be authorized"}</h2><p>${escapeHtml(risk.reason)}</p></div></section>
    ${findingCards(assessment)}
    <section class="card policy-note"><strong>Why the backend proposed this lane</strong><p>${escapeHtml(risk.reason)}</p><code>Policy ${escapeHtml(assessment.policy.version)} from base ${escapeHtml(assessment.policy.source_revision)}</code><code> · diff ${escapeHtml(assessment.evidence.git_diff_sha256)}</code></section>
    ${rolloutPreview(profile, assessment.evidence, risk.evidence_confidence)}
    ${approvalPanel(assessment, snapshot.approval_token)}`, "changes", true);
  const selector = document.querySelector("#selected-profile");
  if (selector) {
    selector.addEventListener("change", () => {
      const selected = assessment.rollout_options.find((option) => option.name === selector.value);
      const preview = document.querySelector("#rollout-preview");
      if (selected && preview) preview.outerHTML = rolloutPreview(selected, assessment.evidence, risk.evidence_confidence);
    });
  }
  document.querySelector("#approve")?.addEventListener("click", () => submitApproval(assessment, snapshot.approval_token));
  document.querySelector("#reject")?.addEventListener("click", () => submitResolution(assessment, snapshot.approval_token, "reject"));
  document.querySelector("#compile")?.addEventListener("click", () => submitCompilation(assessment, snapshot.approval_token));
}

async function submitCompilation(assessment, token) {
  const button = document.querySelector("#compile");
  const message = document.querySelector("#approval-message");
  button.disabled = true;
  message.textContent = "Compiling SHA-bound rollout…";
  try {
    const release = await api(`/api/assessments/${assessment.change.number}/compile`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-SafeLane-CSRF": token },
      body: JSON.stringify({ image: document.querySelector("#release-image").value.trim() }),
    });
    message.className = "action-message approved";
    message.textContent = `Validated Argo rollout written to ${release.path}`;
    toast("Argo rollout compiled and bound to this exact decision.");
  } catch (error) {
    message.className = "action-message error";
    message.textContent = error.message;
  } finally {
    button.disabled = false;
  }
}

async function submitApproval(assessment, token) {
  return submitResolution(assessment, token, "approve");
}

async function submitResolution(assessment, token, action) {
  const button = document.querySelector("#approve");
  const message = document.querySelector("#approval-message");
  button.disabled = true;
  message.textContent = action === "approve" ? "Recording approval…" : "Recording rejection…";
  try {
    await api(`/api/assessments/${assessment.change.number}/resolve`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-SafeLane-CSRF": token },
      body: JSON.stringify({
        action,
        actor: state.dashboard?.reviewer || "local-reviewer",
        selected_profile: action === "approve" ? document.querySelector("#selected-profile").value : null,
        assessment_id: assessment.assessment_id,
        assessment_result_sha256: assessment.assessment_result_sha256,
      }),
    });
    toast(action === "approve" ? "Rollout approved for compilation." : "Change rejected. No rollout decision was emitted.");
    await renderAssessment(assessment.change.number);
  } catch (error) {
    message.className = "action-message error";
    message.textContent = error.status === 409 ? "This PR changed. Reload its latest assessment." : "Approval could not be recorded safely.";
    button.disabled = false;
  }
}

async function renderProfiles() {
  loading("Reading built-in rollout profiles…");
  let result;
  try { result = await api("/api/profiles"); }
  catch (error) { showError(error.message); return; }
  state.dashboard = {
    ...state.dashboard,
    repository: result.repository,
    approval_token: result.approval_token,
    counts: state.dashboard?.counts || { needs_review: "–", resolved: "–" },
    changes: state.dashboard?.changes || [],
  };
  const descriptions = {
    Fast: "For safe changes. Kubernetes readiness is the only gate.",
    Guarded: "For changes that need one checkpoint before full rollout.",
    Strict: "For risky changes. Exposure grows after each health checkpoint.",
  };
  const colors = { Fast: ["#08784a", "#e9f8f0", "F"], Guarded: ["#a15c07", "#fff4e2", "G"], Strict: ["#b42318", "#feeeec", "S"] };
  const cards = result.profiles.map((profile) => {
    const [color, soft, initial] = colors[profile.name];
    const stages = profile.stages.map((stage) => stage.exposure_pods === profile.replicas ? "all" : stage.exposure_pods).join(" → ");
    return `<article class="profile-card" style="--profile:${color};--soft:${soft}"><div class="profile-icon">${initial}</div><h2>${profile.name}</h2><p>${descriptions[profile.name]}</p><code>${stages} · ${profile.stages.some((stage) => stage.analysis) ? "trusted analysis" : "readiness"}</code></article>`;
  }).join("");
  app.innerHTML = shell(`<header class="page-head"><div><div class="eyebrow">Repository safety contract</div><h1>Profiles</h1><p>The policy at the PR base SHA defines the rollout behavior. SafeLane's backend selects the minimum required care.</p></div><span class="version">POLICY ${escapeHtml(result.policy_version)}</span></header><section class="profiles">${cards}</section><section class="card policy-note"><strong>Profiles are repository-owned and read-only here.</strong><p>A pull request may propose a future policy change, but it cannot weaken the policy assessing itself.</p></section>`, "profiles");
}

async function renderOutcomes() {
  loading("Reading bound rollout receipts…");
  let result;
  try { result = await api("/api/outcomes"); }
  catch (error) { showError(error.message); return; }
  const tiers = ["safe", "guarded", "risky"];
  const cards = tiers.map((tier) => {
    const bucket = result.by_tier[tier] || { total: 0, succeeded: 0, failed_or_aborted: 0, incidents_within_24h: 0 };
    return `<article class="profile-card"><div class="profile-icon">${tier[0].toUpperCase()}</div><h2>${tier}</h2><p>${bucket.total} bound rollout${bucket.total === 1 ? "" : "s"}</p><code>${bucket.succeeded} succeeded · ${bucket.failed_or_aborted} failed/aborted · ${bucket.incidents_within_24h} incidents</code></article>`;
  }).join("");
  const calibrationRows = (label, buckets) => Object.entries(buckets).map(([id, bucket]) => `<tr><td>${escapeHtml(label)}</td><td><code>${escapeHtml(id)}</code></td><td>${bucket.total}</td><td>${bucket.failed_or_aborted}</td><td>${bucket.incidents_within_24h}</td></tr>`).join("");
  const calibration = calibrationRows("Rule", result.by_rule) + calibrationRows("Finding", result.by_finding);
  app.innerHTML = shell(`<header class="page-head"><div><div class="eyebrow">Observed releases</div><h1>Rollout outcomes</h1><p>Exact-decision receipts only. These counts describe outcomes; they are not a model accuracy score.</p></div><span class="version">${result.total} RECEIPTS</span></header><section class="profiles">${cards}</section><section class="card policy-note"><strong>Calibration without fake certainty</strong><p>Use these receipts to inspect where cautious lanes fail or remain clean. A successful Strict rollout is not automatically a false positive.</p></section><section class="card calibration"><h2>Rule and finding outcomes</h2>${calibration ? `<table><thead><tr><th>Kind</th><th>Identity</th><th>Runs</th><th>Failed / aborted</th><th>Incidents</th></tr></thead><tbody>${calibration}</tbody></table>` : `<p>No rule or finding receipts yet.</p>`}</section>`, "outcomes");
}

function openRepositoryDialog() {
  repositoryInput.value = state.dashboard?.repository || "";
  repositoryError.textContent = "";
  repositoryDialog.showModal();
  repositoryInput.focus();
  repositoryInput.select();
}

repositoryForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = document.querySelector("#connect-repository");
  const repository = repositoryInput.value.trim();
  repositoryError.textContent = "";
  button.disabled = true;
  button.textContent = "Checking GitHub…";
  try {
    const result = await api("/api/connect", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-SafeLane-CSRF": state.dashboard?.approval_token || "",
      },
      body: JSON.stringify({ repository }),
    });
    state.dashboard = result;
    state.filter = "needs_review";
    repositoryDialog.close();
    toast(`Connected to ${result.repository}.`);
    navigate("/changes");
  } catch (error) {
    repositoryError.textContent = error.message;
  } finally {
    button.disabled = false;
    button.textContent = "Connect repository";
  }
});

repositoryDialog.addEventListener("click", (event) => {
  if (event.target === repositoryDialog) repositoryDialog.close();
});

async function renderRoute() {
  const detail = location.pathname.match(/^\/changes\/(\d+)$/);
  if (detail) return renderAssessment(Number(detail[1]));
  if (location.pathname === "/profiles") return renderProfiles();
  if (location.pathname === "/outcomes") return renderOutcomes();
  return renderChanges();
}

document.addEventListener("click", (event) => {
  if (event.target.closest("#repository-switch")) {
    openRepositoryDialog();
    return;
  }
  if (event.target.closest("[data-close-dialog]")) {
    repositoryDialog.close();
    return;
  }
  const link = event.target.closest("a[data-nav]");
  if (!link) return;
  event.preventDefault();
  navigate(link.getAttribute("href"));
});
addEventListener("popstate", renderRoute);
renderRoute();
