import { createButton } from './button.js';
import { createModal } from './modal.js';
import { createViewer } from './viewer.js';
import { attachTooltips } from './tooltips.js';

const viewer = createViewer();

export const AdminUI = {
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
