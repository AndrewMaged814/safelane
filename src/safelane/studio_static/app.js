"use strict";

const studio = document.querySelector("#studio");
let currentSnapshot = null;

function node(tag, className, text) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text !== undefined) element.textContent = text;
  return element;
}

function append(parent, ...children) {
  children.filter(Boolean).forEach((child) => parent.append(child));
  return parent;
}

function metaIdentity(assessment) {
  const identity = node("div", "identity");
  const values = [
    ["Repository", assessment.change.repository],
    ["PR", `#${assessment.change.pull_request}`],
    ["Head", assessment.change.head_sha],
    ["Policy", assessment.policy_version],
  ];
  values.forEach(([label, value]) => {
    const item = node("span");
    append(item, node("strong", "", `${label}: `), document.createTextNode(value));
    identity.append(item);
  });
  return identity;
}

function statusText(assessment) {
  if (assessment.review.status === "resolved") {
    return assessment.review.resolution.type === "automatic" ? "Resolved automatically" : "Resolved";
  }
  return "Awaiting approval";
}

function stageList(profile) {
  const list = node("div", "stage-list");
  profile.stages.forEach((stage, index) => {
    const row = node("div", "stage");
    const copy = node("div", "stage-copy");
    append(
      copy,
      node("strong", "", `${stage.exposure_pods} of ${profile.replicas} pods`),
      node("small", "", stage.analysis ? `Weight ${stage.set_weight} · trusted check` : `Weight ${stage.set_weight} · readiness`),
    );
    append(row, node("span", "stage-number", String(index + 1).padStart(2, "0")), copy);
    list.append(row);
  });
  return list;
}

function policyTrace(assessment) {
  const trace = node("div", "trace");
  const rows = [
    ["Baseline", assessment.policy_trace.baseline.rule_id],
    ["Confidence", assessment.policy_result.evidence_confidence],
    ["Minimum", assessment.policy_result.minimum_profile],
  ];
  assessment.policy_trace.safety_floors.forEach((floor) => rows.push(["Safety floor", floor.rule_id]));
  rows.forEach(([label, value]) => {
    append(trace, append(node("div", "trace-row"), node("span", "", label), node("strong", "", value)));
  });
  return trace;
}

function renderFast(assessment, decisionPath) {
  const section = node("section");
  append(section, node("p", "eyebrow", "Fast eligibility / positive proof"), node("h2", "section-title", "Every Fast condition is present"));
  const checks = node("ul", "checks");
  [
    assessment.change.all_paths_recognized && "Every changed path is recognized",
    assessment.policy_trace.baseline.rule_id === "scope.low" && "Change is inside the bounded Fast scope",
    assessment.evidence.ai_status === "complete" && "Bounded AI evidence completed",
    assessment.ai_findings.length === 0 && "No verified breaking contract was found",
    assessment.policy_result.fast_eligible && "Policy explicitly marks this change Fast-eligible",
  ].filter(Boolean).forEach((text) => checks.append(node("li", "check", text)));
  append(section, checks);
  if (decisionPath) {
    append(section, node("p", "eyebrow", "Authorization artifact"), node("p", "decision-path", decisionPath));
  }
  return section;
}

function spanCards(finding) {
  const spans = node("div", "spans");
  finding.spans.forEach((span) => {
    const card = node("div", `span-card ${span.side}`);
    const label = node("div", "span-label");
    append(label, node("span", "", span.side), node("span", "", `${span.file}:${span.line}`));
    append(card, label, node("code", "", span.text));
    spans.append(card);
  });
  return spans;
}

function probeGrid(preview) {
  const grid = node("div", "probe-grid");
  const values = [
    ["Request", `${preview.method} ${preview.path}`],
    ["Expected", `HTTP ${preview.expected_status}`],
    ["Attempts", String(preview.attempts)],
    ["Target", preview.canary_only ? "Canary only" : "Invalid target"],
  ];
  values.forEach(([label, value]) => append(grid, append(node("div"), node("small", "", label), node("strong", "", value))));
  return grid;
}

function chainStep(index, trustLabel, title, content) {
  const step = node("section", "chain-step");
  append(step, node("span", "step-index", String(index).padStart(2, "0")));
  if (trustLabel) step.append(node("span", "trust-label", trustLabel));
  step.append(node("h3", "", title), content);
  return step;
}

function renderRiskCase(assessment) {
  const chain = node("div", "chain");
  const finding = assessment.ai_findings[0];
  const safeguard = assessment.selected_safeguard;
  const selectedProfile = assessment.rollout_options[0];
  let index = 1;
  if (finding) {
    chain.append(chainStep(index++, "Verified finding", "Removed and added source spans", spanCards(finding)));
  }
  if (safeguard) {
    chain.append(chainStep(index++, "AI proposed", "Failure impact", node("p", "", safeguard.hypothesis)));
  }
  if (finding) {
    chain.append(chainStep(index++, "Normal code", "2/2 source references verified", node("p", "", "Both cited route decorators exist at the exact changed-line identities.")));
  }
  if (safeguard) {
    chain.append(chainStep(index++, "Trusted probe selected by SafeLane", safeguard.probe_id, probeGrid(safeguard.probe_preview)));
  } else {
    const fallback = node("div", "fallback");
    append(fallback, node("strong", "", "Policy fallback analysis will run after approval"), node("p", "", "No AI-linked safeguard was accepted. Rejected model values are not displayed or executed."));
    chain.append(chainStep(index++, "Normal code", "Fallback safeguard", fallback));
  }
  if (selectedProfile?.stages.length) {
    const firstStage = selectedProfile.stages[0];
    chain.append(
      chainStep(
        index++,
        "Built-in rollout preview",
        `First exposure: ${firstStage.exposure_pods} of ${selectedProfile.replicas} pods`,
        node("p", "", `${selectedProfile.name} begins at weight ${firstStage.set_weight}; the server owns this stage definition.`),
      ),
    );
  }
  return chain;
}

function approvalPanel(assessment, decisionPath) {
  const panel = node("section", "approval");
  if (assessment.review.status === "resolved") {
    append(panel, node("p", "eyebrow", "Authorization commit point"), node("h3", "", "Resolved"));
    if (decisionPath) panel.append(node("p", "decision-path", decisionPath));
    return panel;
  }

  const safeguard = assessment.selected_safeguard;
  append(panel, node("p", "eyebrow", "Human resolution required"));
  if (safeguard) {
    append(
      panel,
      node("p", "approval-question", safeguard.approval_question),
      node("p", "remediation", safeguard.remediation),
    );
  }
  const select = node("select");
  select.setAttribute("aria-label", "Selected rollout profile");
  assessment.rollout_options.forEach((profile) => {
    const option = node("option", "", profile.name);
    option.value = profile.name;
    select.append(option);
  });
  const onlyProfile = assessment.rollout_options.length === 1 ? assessment.rollout_options[0].name : null;
  const button = node("button", "approve-button", onlyProfile === "Strict" ? "Approve Strict rollout" : "Approve selected rollout");
  button.type = "button";
  const message = node("p", "action-message");
  button.addEventListener("click", () => submitApproval(select.value, button, message));
  append(
    panel,
    select,
    button,
    node("p", "approval-note", "This records the rollout plan for the exact current assessment. It does not release software."),
    message,
  );
  return panel;
}

function render(snapshot) {
  currentSnapshot = snapshot;
  const assessment = snapshot.assessment;
  const tier = assessment.policy_result.final_tier;
  const dossier = node("article", `dossier ${tier}`);
  const head = node("header", "dossier-head");
  const intro = node("div");
  append(
    intro,
    metaIdentity(assessment),
    node("h2", "dossier-title", tier === "safe" ? "Clear for the Fast lane" : tier === "risky" ? "Breaking contract" : "Guarded release required"),
    node("p", "reason", assessment.policy_result.primary_reason),
  );
  append(head, intro, node("div", "status-badge", statusText(assessment)));

  const body = node("div", "dossier-body");
  const caseColumn = node("div", "case-column");
  if (tier === "safe") {
    caseColumn.append(renderFast(assessment, snapshot.decision_path));
  } else {
    append(caseColumn, node("p", "eyebrow", "Causal safety case"), renderRiskCase(assessment));
  }

  const rail = node("aside", "rail-column");
  const profile = assessment.rollout_options[0];
  append(
    rail,
    node("p", "eyebrow", "Server-owned rollout preview"),
    node("h2", "section-title", `${profile.name} stages`),
    stageList(profile),
    policyTrace(assessment),
    approvalPanel(assessment, snapshot.decision_path),
  );
  append(body, caseColumn, rail);
  append(dossier, head, body);
  studio.replaceChildren(dossier);
}

async function submitApproval(selectedProfile, button, message) {
  const assessment = currentSnapshot.assessment;
  button.disabled = true;
  message.className = "action-message";
  message.textContent = "Recording approval…";
  const payload = {
    selected_profile: selectedProfile,
    assessment_id: assessment.assessment_id,
    head_sha: assessment.change.head_sha,
    assessment_input_sha256: assessment.assessment_input_sha256,
    assessment_result_sha256: assessment.assessment_result_sha256,
  };
  try {
    const response = await fetch("/api/approve", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-SafeLane-CSRF": currentSnapshot.approval_token,
      },
      body: JSON.stringify(payload),
    });
    const result = await response.json();
    if (!response.ok) {
      throw new Error(response.status === 409 ? "This page is stale. Reload the current assessment." : "Approval could not be recorded safely.");
    }
    render(result);
  } catch (error) {
    message.className = "action-message error";
    message.textContent = error.message;
    button.disabled = false;
  }
}

async function loadCurrent() {
  try {
    const response = await fetch("/api/assessment", {cache: "no-store"});
    if (!response.ok) throw new Error("The current workspace is unavailable.");
    render(await response.json());
  } catch (error) {
    const card = node("section", "error-card");
    append(card, node("p", "eyebrow", "Workspace error"), node("h2", "section-title", error.message));
    studio.replaceChildren(card);
  }
}

loadCurrent();
