import { createModal } from './modal.js';
export function createViewer(){
  const modal = createModal({title:'Escalation content'});
  modal.showContent = (html) => {
    try{ const obj = JSON.parse(html); modal.setBody(`<pre>${escapeHtml(JSON.stringify(obj,null,2))}</pre>`); }catch(e){ modal.setBody(html); }
    modal.show();
  };
  return modal;
}
function escapeHtml(s){ return String(s||'').replace(/[&<>'"]/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'})[c]); }
