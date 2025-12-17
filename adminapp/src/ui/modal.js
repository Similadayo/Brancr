export function createModal({title=''}){
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
