export function createModal({title=''}){
  const modal = document.createElement('div');
  modal.className = 'adminui-modal-backdrop';
  modal.innerHTML = `<div class="adminui-modal" role="dialog" aria-modal="true" aria-label="${escapeHtml(title)}"><div class="adminui-modal-header"><h3>${escapeHtml(title)}</h3></div><div class="adminui-modal-body"></div><div class="adminui-modal-footer"></div></div>`;
  const dialog = modal.querySelector('.adminui-modal');
  const body = modal.querySelector('.adminui-modal-body');
  const footer = modal.querySelector('.adminui-modal-footer');

  let previouslyFocused = null;
  function focusFirst() {
    const focusable = dialog.querySelectorAll('a[href], button, textarea, input, select, [tabindex]:not([tabindex="-1"])');
    if (focusable.length) focusable[0].focus();
    else dialog.focus();
  }

  function onKeyDown(e){
    if (e.key === 'Escape') {
      modal.hide();
    } else if (e.key === 'Tab') {
      // focus trap: explicitly move focus because synthetic Tab in tests doesn't move it
      const focusable = Array.from(dialog.querySelectorAll('a[href], button, textarea, input, select, [tabindex]:not([tabindex="-1"])'))
        .filter(el => !el.hasAttribute('disabled'));
      if (!focusable.length) { e.preventDefault(); return; }
      const idx = focusable.indexOf(document.activeElement);
      if (e.shiftKey) {
        if (idx <= 0 || document.activeElement === dialog) { focusable[focusable.length-1].focus(); e.preventDefault(); }
        else { focusable[idx-1].focus(); e.preventDefault(); }
      } else {
        if (idx === -1) { focusable[0].focus(); e.preventDefault(); }
        else if (idx === focusable.length-1) { focusable[0].focus(); e.preventDefault(); }
        else { focusable[idx+1].focus(); e.preventDefault(); }
      }
    }
  }

  modal.show = () => {
    previouslyFocused = document.activeElement;
    document.body.appendChild(modal);
    // ensure dialog is focusable
    dialog.setAttribute('tabindex', '-1');
    focusFirst();
    document.addEventListener('keydown', onKeyDown);
  };

  modal.hide = () => {
    if (modal.parentNode) modal.parentNode.removeChild(modal);
    document.removeEventListener('keydown', onKeyDown);
    if (previouslyFocused && previouslyFocused.focus) previouslyFocused.focus();
  };

  modal.setBody = (node) => { body.innerHTML=''; if (typeof node === 'string') body.innerHTML = node; else body.appendChild(node); };
  modal.setFooter = (node) => { footer.innerHTML=''; if (typeof node === 'string') footer.innerHTML = node; else footer.appendChild(node); };
  return modal;
}

function escapeHtml(s){ return String(s||'').replace(/[&<>'"]/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'})[c]); }
