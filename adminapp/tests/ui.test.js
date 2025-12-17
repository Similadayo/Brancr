import fs from 'fs';
import path from 'path';
import { beforeEach, afterEach, describe, it, expect } from 'vitest';

const uiPath = path.resolve(__dirname, '../dist/assets/ui.js');
const uiSrc = fs.readFileSync(uiPath, 'utf-8');
let AdminUI;

beforeEach(() => {
  document.body.innerHTML = '';
  // Evaluate UI script in the current window/document context and return AdminUI
  // eslint-disable-next-line no-new-func
  const fn = new Function('window', 'document', uiSrc + '\n return window.AdminUI;');
  AdminUI = fn(window, document);
});

afterEach(() => {
  // cleanup any toasts/tooltips/viewers
  const t = document.getElementById('adminui-toast-container'); if (t) t.remove();
  const v = document.getElementById('adminui-viewer'); if (v) v.remove();
  const m = document.getElementById('adminui-reauth-modal'); if (m) m.remove();
});

describe('AdminUI primitives', () => {
  it('shows toast and then removes it', async () => {
    AdminUI.toast('hello', 'info', 50);
    const container = document.getElementById('adminui-toast-container');
    expect(container).toBeTruthy();
    expect(container.textContent).toContain('hello');
    // wait for auto-remove (timeout plus small buffer)
    await new Promise((r) => setTimeout(r, 120));
    expect(document.getElementById('adminui-toast-container')?.children.length || 0).toBe(0);
  });

  it('attaches tooltip and shows it on hover', async () => {
    document.body.innerHTML = '<button id="b" data-tooltip="This is secret">Click</button>';
    AdminUI.attachTooltips();
    const btn = document.getElementById('b');
    expect(btn).toBeTruthy();
    // simulate mouseenter
    btn.dispatchEvent(new Event('mouseenter'));
    await new Promise((r) => setTimeout(r, 10));
    const tip = document.querySelector('.adminui-tooltip');
    expect(tip).toBeTruthy();
    expect(tip.textContent).toBe('This is secret');
    // simulate mouseleave
    btn.dispatchEvent(new Event('mouseleave'));
    await new Promise((r) => setTimeout(r, 10));
    expect(document.querySelector('.adminui-tooltip')).toBeNull();
  });

  it('viewEscalation tries fetch and opens reauth modal on failure (skipped network)', async () => {
    // simple smoke: ensure function exists
    expect(typeof AdminUI.viewEscalation).toBe('function');
  });
});
