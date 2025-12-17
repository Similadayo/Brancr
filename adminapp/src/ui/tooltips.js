export function attachTooltips(root = document){
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
