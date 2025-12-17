(function () {
  // Lightweight Admin UI utilities (no framework) exposed as window.AdminUI
  function el(tag, attrs, children) {
    const e = document.createElement(tag);
    if (attrs) Object.keys(attrs).forEach(k => e.setAttribute(k, attrs[k]));
    if (children) children.forEach(c => e.appendChild(typeof c === 'string' ? document.createTextNode(c) : c));
    return e;
  }

  // Reauth modal
  function ensureReauthModal() {
    let modal = document.getElementById('adminui-reauth-modal');
    if (modal) return modal;
    modal = el('div', { id: 'adminui-reauth-modal', 'aria-hidden': 'true' });
    modal.innerHTML = `
      <div class="adminui-modal-backdrop">
        <div class="adminui-modal" role="dialog" aria-modal="true" aria-labelledby="adminui-reauth-title">
          <h3 id="adminui-reauth-title">Re-authenticate</h3>
          <p>Enter your admin password to view sensitive content.</p>
          <input id="adminui-reauth-password" type="password" autocomplete="current-password" placeholder="Password" />
          <div id="adminui-reauth-error" class="adminui-error" style="display:none;color:var(--danger);margin-top:8px;"></div>
          <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:12px;">
            <button id="adminui-reauth-cancel" class="btn-secondary">Cancel</button>
            <button id="adminui-reauth-submit" class="btn-primary">Submit</button>
          </div>
        </div>
      </div>`;
    document.body.appendChild(modal);
    const cancel = modal.querySelector('#adminui-reauth-cancel');
    cancel.addEventListener('click', () => { modal.style.display = 'none'; modal.setAttribute('aria-hidden','true'); });
    return modal;
  }

  function showReauthModal() {
    const modal = ensureReauthModal();
    const pw = modal.querySelector('#adminui-reauth-password');
    const err = modal.querySelector('#adminui-reauth-error');
    pw.value = '';
    err.style.display = 'none';
    modal.style.display = 'block';
    modal.setAttribute('aria-hidden','false');
    return new Promise((resolve, reject) => {
      const submit = modal.querySelector('#adminui-reauth-submit');
      const onSubmit = async () => {
        submit.disabled = true;
        try {
          const token = await fetch('/admin/reauth', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: pw.value })
          });
          if (!token.ok) {
            err.textContent = 'Authentication failed';
            err.style.display = 'block';
            return;
          }
          const body = await token.json();
          modal.style.display = 'none';
          modal.setAttribute('aria-hidden','true');
          submit.disabled = false;
          resolve(body.token);
        } catch (e) {
          submit.disabled = false;
          reject(e);
        }
      };
      submit.onclick = onSubmit;
      modal.querySelector('#adminui-reauth-cancel').onclick = () => { modal.style.display = 'none'; modal.setAttribute('aria-hidden','true'); reject(new Error('cancelled')); };
    });
  }

  // Viewer modal
  function ensureViewer() {
    let v = document.getElementById('adminui-viewer');
    if (v) return v;
    v = el('div', { id: 'adminui-viewer' });
    v.innerHTML = `
      <div class="adminui-viewer-backdrop">
        <div class="adminui-viewer" role="dialog" aria-modal="true"><button class="adminui-viewer-close" aria-label="Close">✕</button><div id="adminui-viewer-content"></div></div>
      </div>`;
    document.body.appendChild(v);
    v.querySelector('.adminui-viewer-close').addEventListener('click', () => { v.style.display = 'none'; v.setAttribute('aria-hidden','true'); });
    v.addEventListener('click', (ev) => { if (ev.target === v) { v.style.display='none'; v.setAttribute('aria-hidden','true'); } });
    return v;
  }

  function showViewer(html) {
    const v = ensureViewer();
    const content = v.querySelector('#adminui-viewer-content');
    try {
      const obj = JSON.parse(html);
      content.innerHTML = '<pre>' + escapeHtml(JSON.stringify(obj, null, 2)) + '</pre>';
    } catch (e) {
      content.innerHTML = html;
    }
    v.style.display = 'block';
    v.setAttribute('aria-hidden','false');
  }

  function escapeHtml(s) {
    return String(s || '').replace(/[&<>'"]/g, (c) => ({ '&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;' }[c]));
  }

  async function fetchContent(id, token) {
    const headers = token ? { 'X-Admin-Reauth-Token': token } : {};
    const res = await fetch(`/admin/escalations/${id}/content`, { headers });
    if (!res.ok) throw new Error('fetch failed');
    return res.text();
  }

  // Toast
  function showToast(message, kind = 'info', timeout = 5000) {
    let container = document.getElementById('adminui-toast-container');
    if (!container) {
      container = document.createElement('div');
      container.id = 'adminui-toast-container';
      container.setAttribute('aria-live', 'polite');
      container.style.position = 'fixed';
      container.style.right = '16px';
      container.style.top = '16px';
      container.style.zIndex = '1100';
      document.body.appendChild(container);
    }
    const toast = document.createElement('div');
    toast.className = `adminui-toast adminui-toast-${kind}`;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(() => { toast.classList.add('adminui-toast-hide'); setTimeout(() => toast.remove(), 300); }, timeout);
  }

  // Tooltips: attach to elements with data-tooltip attribute
  function attachTooltips(root = document) {
    const nodes = root.querySelectorAll('[data-tooltip]');
    nodes.forEach((node) => {
      if (node._adminuiTooltipAttached) return;
      node._adminuiTooltipAttached = true;
      node.setAttribute('tabindex', node.getAttribute('tabindex') || '0');
      const show = (ev) => {
        let tip = document.createElement('div');
        tip.className = 'adminui-tooltip';
        tip.textContent = node.getAttribute('data-tooltip');
        document.body.appendChild(tip);
        const rect = node.getBoundingClientRect();
        tip.style.position = 'absolute';
        tip.style.left = `${rect.left + window.scrollX + rect.width / 2 - tip.offsetWidth / 2}px`;
        tip.style.top = `${rect.top + window.scrollY - tip.offsetHeight - 8}px`;
        node._adminuiTip = tip;
      };
      const hide = () => { if (node._adminuiTip) { node._adminuiTip.remove(); node._adminuiTip = null; } };
      node.addEventListener('mouseenter', show);
      node.addEventListener('focus', show);
      node.addEventListener('mouseleave', hide);
      node.addEventListener('blur', hide);
    });
  }

  // Public API
  window.AdminUI = {
    viewEscalation: async function(id) {
      try {
        const html = await fetchContent(id);
        showViewer(html);
        return;
      } catch (e) {
        // need reauth
      }
      try {
        const token = await showReauthModal();
        const html = await fetchContent(id, token);
        showViewer(html);
      } catch (err) {
        // cancelled or failed
        console.warn('View cancelled or failed', err);
      }
    },
    toast: showToast,
    attachTooltips: attachTooltips
  };

  // minimal styles
  const style = document.createElement('style');
  style.textContent = `
  .adminui-modal-backdrop, .adminui-viewer-backdrop{position:fixed;inset:0;background:rgba(0,0,0,0.45);display:flex;align-items:center;justify-content:center;z-index:1000}
  .adminui-modal, .adminui-viewer{background:var(--bg-primary);padding:20px;border-radius:8px;width:520px;max-width:95%;max-height:80vh;overflow:auto}
  .adminui-viewer{width:720px}
  .adminui-error{color:var(--danger)}
  `;
  document.head.appendChild(style);
})();
