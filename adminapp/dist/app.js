(function () {
  const dom = {
    statTotalTenants: document.getElementById("statTotalTenants"),
    statActive24: document.getElementById("statActive24"),
    statMessages: document.getElementById("statMessages"),
    statAIRequests: document.getElementById("statAIRequests"),
    statFailures: document.getElementById("statFailures"),
    statApiError: document.getElementById("statApiError"),
    tenantGrid: document.getElementById("tenantGrid"),
    integrationList: document.getElementById("integrationList"),
    onboardingCompletion: document.getElementById("onboardingCompletion"),
    onboardingTime: document.getElementById("onboardingTime"),
    onboardingLoss: document.getElementById("onboardingLoss"),
    manualInterventions: document.getElementById("manualInterventions"),
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

  let dashboardData = null;

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
      const response = await fetch(endpoints.dashboard, {
        headers: { Accept: "application/json" },
      });
      if (!response.ok) {
        throw new Error("failed to load dashboard");
      }
      dashboardData = await response.json();
      renderDashboard(dashboardData);
      if (dom.lastUpdated) {
        dom.lastUpdated.textContent = new Date().toLocaleTimeString();
      }
    } catch (error) {
      console.error("dashboard load failed", error);
    }
  }

  async function fetchStats() {
    try {
      const response = await fetch(endpoints.stats, {
        headers: { Accept: "application/json" },
      });
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
    if (!window.EventSource) {
      return;
    }
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
      console.warn("SSE connection closed");
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
    if (!payload) {
      return;
    }
    renderTenants(payload.Tenants);
    renderOnboarding(payload.Onboarding);
    renderIntegrations(payload.Integrations);
    renderMessaging(payload.Messaging);
    renderBilling(payload.Billing);
    renderAudit(payload.Audit, payload.Suspensions);
    renderEscalations(payload.Escalations);
    updateStatusBadge(payload.Integrations);
  }

  function renderTenants(tenants) {
    if (!dom.tenantGrid) {
      return;
    }
    if (!tenants?.length) {
      dom.tenantGrid.innerHTML = "<p class=\"muted-text\">No tenant metadata available.</p>";
      return;
    }
    dom.tenantGrid.innerHTML = tenants
      .map((tenant) => {
        const statusButton = tenant.Status === "suspended" ? "Reactivate tenant" : "Suspend tenant";
        const lastSeen = tenant.LastActivity || "No activity recorded";
        const usageLabel = tenant.UsagePercent ? `${tenant.UsagePercent.toFixed(1)}% usage` : "Usage data pending";
        return `
          <article class="tenant-card" data-tenant-id="${tenant.ID}" data-tenant-status="${tenant.Status}">
            <div class="tenant-header">
              <strong>${tenant.Name}</strong>
              <span>#${tenant.ID}</span>
            </div>
            <div class="tenant-meta">
              <span>Plan • ${tenant.PlanType || "unknown"}</span>
              <span>Status • ${capitalize(tenant.Status)}</span>
            </div>
            <p class="muted-text">Onboarding · ${tenant.OnboardingStep || "—"}</p>
            <p class="muted-text">Platforms · ${tenant.Platforms.join(" • ") || "None connected"}</p>
            <p class="muted-text">Last activity · ${lastSeen}</p>
            <p class="muted-text">${usageLabel}</p>
            <div class="tenant-actions">
              <button data-action="toggle-status">${statusButton}</button>
              <button data-action="reset-onboarding">Reset onboarding</button>
              <button data-action="refresh-tokens">Refresh tokens</button>
            </div>
          </article>`;
      })
      .join("");
  }

  function renderOnboarding(onboarding) {
    if (!onboarding) return;
    dom.onboardingCompletion && (dom.onboardingCompletion.textContent = `${onboarding.CompletionRate.toFixed(1)}%`);
    dom.onboardingTime && (dom.onboardingTime.textContent = `${Object.keys(onboarding.StepTotals).length} steps`);
    dom.manualInterventions && (dom.manualInterventions.textContent = `${onboarding.ManualInterventions}`);
    if (dom.onboardingLoss) {
      dom.onboardingLoss.innerHTML =
        onboarding.DropOffs.length > 0
          ? onboarding.DropOffs.map((drop) => `<li><strong>${drop.Step}</strong>${drop.Count} tenants (${drop.Percent.toFixed(1)}%)</li>`).join("")
          : "<li>No drop-offs detected</li>";
    }
  }

  function renderIntegrations(integrations) {
    if (!dom.integrationList || !integrations) return;
    dom.integrationList.innerHTML = integrations
      .map(
        (integration) => `
          <div class="status-item ${integration.Status === "Warning" ? "warning" : ""}">
            <div>
              <strong>${integration.Name}</strong>
              <p class="muted-text">${integration.Connected}/${integration.Total} connected</p>
              <p class="muted-text">${integration.ExpiresSoon} expiring soon</p>
            </div>
            <div class="muted-text">
              <span>${integration.ErrorRate.toFixed(1)}% errors</span>
            </div>
          </div>
        `
      )
      .join("");
  }

  function renderMessaging(messaging) {
    if (!messaging) return;
    dom.platformBars && (dom.platformBars.innerHTML = messaging.PlatformVolumes
      .map(
        (platform) => `
          <div class="bar">
            <div class="bar-label">${platform.Platform}</div>
            <div class="bar-meter">
              <span style="width:${Math.min(platform.Count, 100)}%"></span>
            </div>
            <div>${platform.Count}</div>
          </div>
        `
      )
      .join(""));
    dom.aiRatio && (dom.aiRatio.textContent = `${messaging.AiPercent.toFixed(1)}%`);
    dom.humanRatio && (dom.humanRatio.textContent = `${messaging.HumanPercent.toFixed(1)}%`);
    dom.failedDeliveries && (dom.failedDeliveries.textContent = `${messaging.FailedDeliveries}`);
    dom.messageLatency && (dom.messageLatency.textContent = `${messaging.LatencySeconds.toFixed(1)}s`);
  }

  function renderBilling(billing) {
    if (!billing) return;
    dom.usageBars && (dom.usageBars.innerHTML = billing.Usage
      .map(
        (item) => `
          <div class="usage-bar">
            <span>${item.Name}</span>
            <div class="meter">
              <span style="width:${Math.min(item.Percent, 100)}%"></span>
            </div>
            <span>${item.Percent.toFixed(1)}%</span>
          </div>
        `
      )
      .join(""));
    dom.overageSignal && (dom.overageSignal.textContent = billing.OverageWatch);
    dom.failedPayments && (dom.failedPayments.textContent = `${billing.FailedPayments}`);
    dom.creditsApplied && (dom.creditsApplied.textContent = `${billing.CreditsApplied}`);
    dom.statFailures && (dom.statFailures.textContent = `${billing.FailedPayments}`);
  }

  function renderAudit(audit, suspensions) {
    if (dom.auditEntries) {
      dom.auditEntries.innerHTML =
        audit.length > 0
          ? audit.map((entry) => `<li>${entry}</li>`).join("")
          : "<li>No recent admin actions</li>";
    }
    if (dom.suspensionEntries) {
      dom.suspensionEntries.innerHTML =
        suspensions.length > 0
          ? suspensions.map((entry) => `<li>${entry}</li>`).join("")
          : "<li>No recent suspensions</li>";
    }
  }

  function renderEscalations(data) {
    if (!data) return;
    if (dom.providerEscalations) {
      dom.providerEscalations.innerHTML = data.Provider.map((entry) => `<li>${entry}</li>`).join("");
    }
    if (dom.complianceEscalations) {
      dom.complianceEscalations.innerHTML = data.Compliance.map((entry) => `<li>${entry}</li>`).join("");
    }
    if (dom.billingEscalations) {
      dom.billingEscalations.innerHTML = data.Billing.map((entry) => `<li>${entry}</li>`).join("");
    }
  }

  function updateStatusBadge(integrations = []) {
    if (!dom.statusBadge) return;
    const averageError =
      integrations.reduce((sum, integration) => sum + integration.ErrorRate, 0) /
      Math.max(integrations.length, 1);
    if (averageError > 1.5) {
      dom.statusBadge.textContent = "Status: Warning";
      dom.statusBadge.style.color = "#fbbf24";
      dom.statusBadge.style.borderColor = "rgba(251, 191, 36, 0.4)";
    } else {
      dom.statusBadge.textContent = "Status: Stable";
      dom.statusBadge.style.color = "#34d399";
      dom.statusBadge.style.borderColor = "rgba(52, 211, 153, 0.35)";
    }
    dom.statApiError && (dom.statApiError.textContent = `${averageError.toFixed(1)}%`);
  }

  function handleTenantAction(event) {
    const button = event.target.closest("button[data-action]");
    if (!button) return;
    const card = button.closest(".tenant-card");
    if (!card) return;
    const tenantId = card.dataset.tenantId;
    const action = button.dataset.action;
    if (!tenantId || !action) {
      return;
    }
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
        console.error("tenant action failed", error);
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
    if (value === null || value === undefined) {
      return "0";
    }
    return value.toLocaleString();
  }

  function percentage(part, total) {
    if (!total) {
      return 0;
    }
    return (part / total) * 100;
  }

  function capitalize(value) {
    if (!value) return "";
    return value.charAt(0).toUpperCase() + value.slice(1);
  }

  init();
})();
