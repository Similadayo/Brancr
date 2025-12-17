export function createButton({text, variant='primary', onClick, attrs={}}) {
  const btn = document.createElement('button');
  btn.className = variant === 'primary' ? 'btn-primary' : variant === 'secondary' ? 'btn-secondary' : 'btn-ghost';
  btn.textContent = text;
  if (onClick) btn.addEventListener('click', onClick);
  Object.keys(attrs).forEach(k => btn.setAttribute(k, attrs[k]));
  return btn;
}
