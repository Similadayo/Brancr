const fs = require('fs');
const path = require('path');

const srcDir = path.resolve(__dirname, '../src/ui');
const outFile = path.resolve(__dirname, '../dist/assets/ui.js');

const files = ['button.js','modal.js','viewer.js','tooltips.js','index.js'];
let out = '(function(){\n"use strict";\n';
files.forEach(f=>{
  const p = path.join(srcDir,f);
  let src = fs.readFileSync(p,'utf-8');
  // remove ES module imports
  src = src.replace(/^import .*;$/gm, '');
  // remove export keywords
  src = src.replace(/^export\s+default\s+/m, '');
  src = src.replace(/^export\s+/gm, '');
  // also remove inline export patterns
  src = src.replace(/export\s+const\s+/g, 'const ');
  src = src.replace(/export\s+function\s+/g, 'function ');
  out += '\n// ' + f + '\n';
  out += src + '\n';
});
out += '\n})();\n';
fs.writeFileSync(outFile, out);
console.log('Built ui.js ->', outFile);
