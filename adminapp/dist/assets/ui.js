(function(){
"use strict";

// button.js
function createButton({text, variant='primary', onClick, attrs={}}) {
  const btn = document.createElement('button');
  btn.className = variant === 'primary' ? 'btn-primary' : variant === 'secondary' ? 'btn-secondary' : 'btn-ghost';
  btn.textContent = text;
  if (onClick) btn.addEventListener('click', onClick);
  Object.keys(attrs).forEach(k => btn.setAttribute(k, attrs[k]));
  return btn;
}


// modal.js
function createModal({title=''}){
  const modal = document.createElement('div');
  modal.className = 'adminui-modal-backdrop';
  modal.innerHTML = `<div class="adminui-modal" role="dialog" aria-modal="true"><div class="adminui-modal-header"><h3>${title}</h3></div><div class="adminui-modal-body"></div><div class="adminui-modal-footer"></div></div>`;
  const body = modal.querySelector('.adminui-modal-body');
  const footer = modal.querySelector('.adminui-modal-footer');
  modal.show = () => { document.body.appendChild(modal); };
  modal.hide = () => { if (modal.parentNode) modal.parentNode.removeChild(modal); };
  modal.setBody = (node) => { body.innerHTML=''; if (typeof node === 'string') body.innerHTML = node; else body.appendChild(node); };
  modal.setFooter = (node) => { footer.innerHTML=''; if (typeof node === 'string') footer.innerHTML = node; else footer.appendChild(node); };
  return modal;
}


// viewer.js

function createViewer(){
  const modal = createModal({title:'Escalation content'});
  modal.showContent = (html) => {
    try{ const obj = JSON.parse(html); modal.setBody(`<pre>${escapeHtml(JSON.stringify(obj,null,2))}</pre>`); }catch(e){ modal.setBody(html); }
    modal.show();
  };
  return modal;
}
function escapeHtml(s){ return String(s||'').replace(/[&<>'"]/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'})[c]); }


// tooltips.js
function attachTooltips(root = document){
  const nodes = root.querySelectorAll('[data-tooltip]');
  nodes.forEach(node => {
    if (node._adminuiTooltipAttached) return;
    node._adminuiTooltipAttached = true;
    node.setAttribute('tabindex', node.getAttribute('tabindex')||'0');
    const show = ()=>{
      const tip = document.createElement('div'); tip.className='adminui-tooltip'; tip.textContent = node.getAttribute('data-tooltip'); document.body.appendChild(tip);
      const rect = node.getBoundingClientRect(); tip.style.position='absolute'; tip.style.left = `${rect.left+window.scrollX + rect.width/2 - tip.offsetWidth/2}px`; tip.style.top = `${rect.top+window.scrollY - tip.offsetHeight - 8}px`;
      node._adminuiTip = tip;
    };
    const hide = ()=>{ if (node._adminuiTip){ node._adminuiTip.remove(); node._adminuiTip=null; } };
    node.addEventListener('mouseenter', show); node.addEventListener('focus', show); node.addEventListener('mouseleave', hide); node.addEventListener('blur', hide);
  });
}


// index.js





const viewer = createViewer();

const AdminUI = {
  viewEscalation: async (id)=>{
    try{
      const html = await fetch(`/admin/escalations/${id}/content`).then(r=>{ if(!r.ok) throw new Error('need auth'); return r.text(); });
      viewer.showContent(html); return;
    }catch(e){}
    // need reauth
    const modal = createModal({title:'Re-authenticate for sensitive content'});
    const input = document.createElement('input'); input.type='password'; input.placeholder='Password';
    const err = document.createElement('div'); err.style.color='var(--danger)'; err.style.display='none';
    modal.setBody('Enter your admin password to view full escalation details.');
    const submit = createButton({text:'Submit', variant:'primary'});
    const cancel = createButton({text:'Cancel', variant:'secondary', onClick:()=>modal.hide()});
    modal.setFooter((()=>{const f=document.createElement('div'); f.appendChild(cancel); f.appendChild(submit); return f; })());
    modal.setBody((()=>{const b=document.createElement('div'); b.appendChild(input); b.appendChild(err); return b; })());
    submit.addEventListener('click', async ()=>{ submit.disabled=true; try{ const res = await fetch('/admin/reauth',{method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({password: input.value})}); if(!res.ok){ err.style.display='block'; err.textContent='Authentication failed'; return; } const out = await res.json(); modal.hide(); const html = await fetch(`/admin/escalations/${id}/content`, { headers: {'X-Admin-Reauth-Token': out.token }}).then(r=>r.text()); viewer.showContent(html); } finally { submit.disabled=false; } });
    modal.show();
  },
  attachTooltips,
  toast: (msg, kind='info', timeout = 4000)=>{
    let container = document.getElementById('adminui-toast-container'); if(!container){container=document.createElement('div');container.id='adminui-toast-container';container.style.position='fixed';container.style.right='16px';container.style.top='16px';container.style.zIndex='1100';document.body.appendChild(container);}const t=document.createElement('div');t.className=`adminui-toast adminui-toast-${kind}`;t.textContent=msg;container.appendChild(t);setTimeout(()=>{t.classList.add('adminui-toast-hide');setTimeout(()=>t.remove(),300);},timeout);
  }
};

// Auto-init on load
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', ()=>{ AdminUI.attachTooltips(); }); else AdminUI.attachTooltips();

// expose globally for legacy usage
window.AdminUI = window.AdminUI || AdminUI;


})();
