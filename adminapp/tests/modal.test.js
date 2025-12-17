import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createModal } from '../src/ui/modal.js';

beforeEach(()=>{ document.body.innerHTML = ''; });
afterEach(()=>{ document.body.innerHTML = ''; });

describe('Modal accessibility', ()=>{
  it('focuses first focusable element and traps focus', ()=>{
    const modal = createModal({title:'Test'});
    const input = document.createElement('input');
    const btn1 = document.createElement('button'); btn1.textContent='A';
    const btn2 = document.createElement('button'); btn2.textContent='B';
    modal.setBody(input);
    const f = document.createElement('div'); f.appendChild(btn1); f.appendChild(btn2);
    modal.setFooter(f);

    document.body.appendChild(document.createElement('button')); // something before
    modal.show();
    // first focusable should be input
    expect(document.activeElement).toBe(input);

    // simulate Tab key to move focus
    const ev = new KeyboardEvent('keydown', { key: 'Tab' });
    document.dispatchEvent(ev);
    // after one Tab expect btn1
    expect(document.activeElement).toBe(btn1);

    // simulate Tab twice to wrap to btn2 then back to input
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab' }));
    expect(document.activeElement).toBe(btn2);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab' }));
    expect(document.activeElement === input || document.activeElement === btn1).toBeTruthy();

    modal.hide();
  });

  it('Shift+Tab moves focus backward and wraps', ()=>{
    const modal = createModal({title:'Shift'});
    const input = document.createElement('input');
    const btn1 = document.createElement('button'); btn1.textContent='A';
    const btn2 = document.createElement('button'); btn2.textContent='B';
    modal.setBody(input);
    const f = document.createElement('div'); f.appendChild(btn1); f.appendChild(btn2);
    modal.setFooter(f);

    document.body.appendChild(document.createElement('button')); // something before
    modal.show();
    // first focusable should be input
    expect(document.activeElement).toBe(input);

    // move forward to btn1
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab' }));
    expect(document.activeElement).toBe(btn1);

    // Shift+Tab should go back to input
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true }));
    expect(document.activeElement).toBe(input);

    modal.hide();
  });

  it('closes on Escape and returns focus to previously focused element', ()=>{
    const before = document.createElement('button'); before.textContent = 'before';
    document.body.appendChild(before);
    before.focus();
    expect(document.activeElement).toBe(before);

    const modal = createModal({title:'ESC'});
    modal.setBody('content');
    modal.show();
    // press Escape
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    // modal removed
    expect(document.querySelector('.adminui-modal-backdrop')).toBeNull();
    // focus restored
    expect(document.activeElement).toBe(before);
  });
});
