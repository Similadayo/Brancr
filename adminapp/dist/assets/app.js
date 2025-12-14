(function () {
  const dom = {
    statTotalTenants: document.getElementById("statTotalTenants"),
    statActive24: document.getElementById("statActive24"),
    statMessages: document.getElementById("statMessages"),
    statAIRequests: document.getElementById("statAIRequests"),
    statFailures: document.getElementById("statFailures"),
    statApiError: document.getElementById("statApiError"),
    tenantGrid: document.getElementById("tenantGrid"),
    integrations: document.getElementById("integrations"),
    billing: document.getElementById("billing"),
    platformBars: document.getElementById("platformBars"),
    aiRatio: document.getElementById("aiRatio"),
    humanRatio: document.getElementById("humanRatio"),
    failedDeliveries: document.getElementById("failedDeliveries"),
    messageLatency: document.getElementById("messageLatency"),
    usageBars: document.getElementById("usageBars"),
    overageSignal: document.getElementById("overageSignal"),
    failedPayments: document.getElementById("failedPayments"),
    creditsApplied: document.getElementById("creditsApplied"),
    auditEntries: document.getElementById("auditEntries"),
    suspensionEntries: document.getElementById("suspensionEntries"),
    providerEscalations: document.getElementById("providerEscalations"),
    complianceEscalations: document.getElementById("complianceEscalations"),
    billingEscalations: document.getElementById("billingEscalations"),
    heroClock: document.getElementById("heroClock"),
    statusBadge: document.getElementById("statusBadge"),
    lastUpdated: document.getElementById("lastUpdated"),
    refreshLive: document.getElementById("refreshLive"),
  };

  const endpoints = {
    dashboard: "/admin/api/dashboard",
    stats: "/admin/api/stats",
    events: "/admin/api/events",
  };

  function init() {
    setupSSE();
    loadDashboard();
    fetchStats();
    dom.refreshLive?.addEventListener("click", async () => {
      dom.refreshLive.disabled = true;
      await loadDashboard();
      dom.refreshLive.disabled = false;
    });
    dom.tenantGrid?.addEventListener("click", handleTenantAction);
    setInterval(updateClock, 1000);
    updateClock();
  }

  async function loadDashboard() {
    try {
      const response = await fetch(endpoints.dashboard, { headers: { Accept: "application/json" } });
      if (!response.ok) {
        throw new Error("failed to load dashboard");
      }
      const data = await response.json();
      renderDashboard(data);
      if (dom.lastUpdated) {
        dom.lastUpdated.textContent = new Date().toLocaleTimeString();
      }
    } catch (error) {
      console.error("dashboard load failed", error);
    }
  }

  async function fetchStats() {
    try {
      const response = await fetch(endpoints.stats, { headers: { Accept: "application/json" } });
      if (!response.ok) {
        throw new Error("stats load failed");
      }
      const data = await response.json();
      renderStats(data.Stats);
    } catch (error) {
      console.error("stats fetch failed", error);
    }
  }

  function setupSSE() {
    if (!window.EventSource) return;
    const source = new EventSource(endpoints.events);
    source.addEventListener("stats", (event) => {
      try {
        const parsed = JSON.parse(event.data);
        renderStats(parsed.Stats);
      } catch (error) {
        console.error("invalid stats event", error);
      }
    });
    source.onerror = () => {
      source.close();
    };
  }

  function renderStats(stats) {
    dom.statTotalTenants && (dom.statTotalTenants.textContent = formatNumber(stats.TotalTenants));
    dom.statActive24 && (dom.statActive24.textContent = formatNumber(stats.ActiveTenants));
    dom.statMessages && (dom.statMessages.textContent = formatNumber(stats.EventsToday));
    dom.statAIRequests && (dom.statAIRequests.textContent = formatNumber(stats.OpenRouterUsage));
  }

  function renderDashboard(payload) {
    if (!payload) return;
    renderTenants(payload.Tenants);
    renderIntegrations(payload.Integrations);
    renderMessaging(payload.Messaging);
    renderBilling(payload.Billing);
    renderAudit(payload.Audit, payload.Suspensions);
    renderEscalations(payload.Escalations);
    updateStatusBadge(payload.Integrations);
    renderPlatformChart(payload.Messaging.PlatformVolumes);
    renderCountryList(payload.Billing.Usage);
  }

  function renderTenants(tenants) {
    if (!dom.tenantGrid) return;
    if (!tenants?.length) {
      dom.tenantGrid.innerHTML = "<p class=\"muted-text\">No tenant metadata available.</p>";
      return;
    }
    dom.tenantGrid.innerHTML = tenants
      .map((tenant) => {
        const statusButton = tenant.Status === "suspended" ? "Reactivate" : "Suspend";
        const lastSeen = tenant.LastActivity || "No activity recorded";
        const usageLabel = tenant.UsagePercent ? `${tenant.UsagePercent.toFixed(1)}% usage` : "Usage pending";
        return `
          <article class="tenant-card" data-tenant-id="${tenant.ID}" data-tenant-status="${tenant.Status}">
            <div class="tenant-header">
              <strong>${escapeHtml(tenant.Name)}</strong>
              <span>#${tenant.ID}</span>
            </div>
            <div class="tenant-meta muted-text">
              <span>Plan: ${escapeHtml(tenant.PlanType || "unknown")}</span>
              <span>Status: ${escapeHtml(capitalize(tenant.Status))}</span>
            </div>
            <p class="muted-text">Onboarding: ${escapeHtml(tenant.OnboardingStep || "-")}</p>
            <p class="muted-text">Platforms: ${(tenant.Platforms || []).join(" • ") || "None"}</p>
            <p class="muted-text">Last activity: ${escapeHtml(lastSeen)}</p>
            <p class="muted-text">${escapeHtml(usageLabel)}</p>
            <div class="tenant-actions">
              <button data-action="toggle-status">${statusButton}</button>
              <button data-action="reset-onboarding">Reset onboarding</button>
              <button data-action="refresh-tokens">Refresh tokens</button>
            </div>
          </article>`;
      })
      .join("");
  }

  function renderIntegrations(integrations) {
    if (!dom.integrations || !integrations) return;
    dom.integrations.innerHTML = integrations
      .map(
        (integration) =>
          `<li><strong>${escapeHtml(integration.Name)}</strong>: ${escapeHtml(integration.Status)} • ${integration.ErrorRate.toFixed(
            1
          )}% errors • ${integration.Connected}/${integration.Total} connected</li>`
      )
      .join("");
  }

  function renderMessaging(messaging) {
    if (!messaging) return;
    dom.aiRatio && (dom.aiRatio.textContent = `${messaging.AiPercent.toFixed(1)}%`);
    dom.humanRatio && (dom.humanRatio.textContent = `${messaging.HumanPercent.toFixed(1)}%`);
    dom.failedDeliveries && (dom.failedDeliveries.textContent = `${messaging.FailedDeliveries}`);
    dom.messageLatency && (dom.messageLatency.textContent = `${messaging.LatencySeconds.toFixed(1)}s`);
  }

  function renderBilling(billing) {
    if (!billing) return;
    dom.usageBars &&
      (dom.usageBars.innerHTML = billing.Usage.map(
        (item) => `
          <div class="usage-bar">
            <span>${escapeHtml(item.Name)}</span>
            <div class="meter">
              <span style="width:${Math.min(item.Percent, 100)}%"></span>
            </div>
            <span>${item.Percent.toFixed(1)}%</span>
          </div>
        `
      ).join(""));
    dom.overageSignal && (dom.overageSignal.textContent = billing.OverageWatch);
    dom.failedPayments && (dom.failedPayments.textContent = `${billing.FailedPayments}`);
    dom.creditsApplied && (dom.creditsApplied.textContent = `${billing.CreditsApplied}`);
    dom.statFailures && (dom.statFailures.textContent = `${billing.FailedPayments}`);
  }

  function renderAudit(audit, suspensions) {
    if (dom.auditEntries) {
      dom.auditEntries.innerHTML =
        audit.length > 0 ? audit.map((entry) => `<li>${escapeHtml(entry)}</li>`).join("") : "<li>No recent admin actions</li>";
    }
    if (dom.suspensionEntries) {
      dom.suspensionEntries.innerHTML =
        suspensions.length > 0
          ? suspensions.map((entry) => `<li>${escapeHtml(entry)}</li>`).join("")
          : "<li>No recent suspensions</li>";
    }
  }

  function renderEscalations(data) {
    if (!data) return;
    dom.providerEscalations && (dom.providerEscalations.innerHTML = data.Provider.map((e) => `<li>${escapeHtml(e)}</li>`).join(""));
    dom.complianceEscalations &&
      (dom.complianceEscalations.innerHTML = data.Compliance.map((e) => `<li>${escapeHtml(e)}</li>`).join(""));
    dom.billingEscalations && (dom.billingEscalations.innerHTML = data.Billing.map((e) => `<li>${escapeHtml(e)}</li>`).join(""));
  }

  function updateStatusBadge(integrations = []) {
    if (!dom.statusBadge) return;
    const averageError =
      integrations.reduce((sum, integration) => sum + integration.ErrorRate, 0) / Math.max(integrations.length, 1);
    if (averageError > 1.5) {
      dom.statusBadge.textContent = "Status: Warning";
      dom.statusBadge.style.color = "#fbbf24";
      dom.statusBadge.style.borderColor = "rgba(251, 191, 36, 0.4)";
    } else {
      dom.statusBadge.textContent = "Status: Stable";
      dom.statusBadge.style.color = "#059669";
      dom.statusBadge.style.borderColor = "rgba(52, 211, 153, 0.35)";
    }
    dom.statApiError && (dom.statApiError.textContent = `${averageError.toFixed(1)}%`);
  }

  function renderPlatformChart(platformVolumes = []) {
    if (!dom.platformBars) return;
    const chart = dom.platformBars.querySelector(".bar-chart");
    if (!chart) return;
    const maxCount = Math.max(...platformVolumes.map((volume) => volume.Count), 1);
    chart.innerHTML = platformVolumes
      .map(
        (platform) =>
          `<span title="${escapeHtml(platform.Platform)} • ${platform.Count}" style="height:${(platform.Count / maxCount) * 170}px"></span>`
      )
      .join("");
  }

  function renderCountryList(usage = []) {
    if (!dom.billing) return;
    if (!usage.length) {
      dom.billing.innerHTML = "<li>No usage data</li>";
      return;
    }
    dom.billing.innerHTML = usage
      .map((item, index) => `<li><strong>${escapeHtml(item.Tenant || `Tenant ${index + 1}`)}</strong> – ${item.Percent.toFixed(1)}%</li>`)
      .join("");
  }

  function handleTenantAction(event) {
    const button = event.target.closest("button[data-action]");
    if (!button) return;
    const card = button.closest(".tenant-card");
    if (!card) return;
    const tenantId = card.dataset.tenantId;
    const action = button.dataset.action;
    if (!tenantId || !action) return;

    button.disabled = true;
    const originalText = button.textContent;
    button.textContent = "Processing…";

    let promise;
    if (action === "toggle-status") {
      const currentStatus = card.dataset.tenantStatus;
      const targetStatus = currentStatus === "suspended" ? "active" : "suspended";
      promise = postJson(`/admin/api/tenants/${tenantId}/status`, { status: targetStatus });
    } else if (action === "reset-onboarding") {
      promise = postJson(`/admin/api/tenants/${tenantId}/onboarding/reset`, {});
    } else if (action === "refresh-tokens") {
      promise = postJson(`/admin/api/tenants/${tenantId}/tokens/refresh`, { force: true });
    }

    if (!promise) {
      button.disabled = false;
      button.textContent = originalText;
      return;
    }

    promise
      .then(() => loadDashboard())
      .catch((error) => {
        window.alert("Action failed: " + (error.message || "please try again"));
      })
      .finally(() => {
        button.disabled = false;
        button.textContent = originalText;
      });
  }

  async function postJson(url, body) {
    const response = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!response.ok) {
      throw new Error(`request failed (${response.status})`);
    }
    return response.json();
  }

  function updateClock() {
    if (dom.heroClock) {
      dom.heroClock.textContent = new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    }
  }

  function formatNumber(value) {
    if (value === null || value === undefined) return "0";
    return value.toLocaleString();
  }

  function capitalize(value) {
    if (!value) return "";
    return value.charAt(0).toUpperCase() + value.slice(1);
  }

  function escapeHtml(value) {
    return String(value || "").replace(/[&<>"']/g, (char) => {
      const map = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
      return map[char] || char;
    });
  }

  init();
})();
