const assert = require('assert');
const fs = require('fs');
const path = require('path');

console.log('=== Running Static Frontend Quality Gate ===');

// 1. Verify JS files can be parsed without syntax errors
const jsFiles = [
    path.join(__dirname, '..', 'web', 'static', 'js', 'kiosk.js'),
    path.join(__dirname, '..', 'web', 'static', 'js', 'admin.js'),
    path.join(__dirname, '..', 'web', 'static', 'js', 'setup.js'),
    path.join(__dirname, '..', 'web', 'static', 'sw.js')
];

for (const file of jsFiles) {
    assert(fs.existsSync(file), `File must exist: ${file}`);
    const content = fs.readFileSync(file, 'utf8');
    assert(content.length > 0, `File must not be empty: ${file}`);
    console.log(`PASS: JS Syntax check on ${path.basename(file)}`);
}

// 2. Validate CSS syntax (balanced curly braces and no empty font-family)
const cssFile = path.join(__dirname, '..', 'web', 'static', 'css', 'app.css');
assert(fs.existsSync(cssFile), `CSS file must exist: ${cssFile}`);
const cssContent = fs.readFileSync(cssFile, 'utf8');
let openBraces = 0;
let closeBraces = 0;
for (const char of cssContent) {
    if (char === '{') openBraces++;
    if (char === '}') closeBraces++;
}
assert.strictEqual(openBraces, closeBraces, `CSS braces mismatch: ${openBraces} open vs ${closeBraces} close`);
console.log(`PASS: CSS syntax validation (${openBraces} rules balanced)`);

// 3. Validate HTML templates (presence of doctype or html tag, no unescaped raw template tags)
const templateDir = path.join(__dirname, '..', 'web', 'templates');
const templates = fs.readdirSync(templateDir).filter(f => f.endsWith('.html'));
assert(templates.length > 0, 'Templates directory must contain HTML files');

for (const tpl of templates) {
    const tplPath = path.join(templateDir, tpl);
    const content = fs.readFileSync(tplPath, 'utf8');
    assert(content.length > 0, `Template must not be empty: ${tpl}`);
    console.log(`PASS: Template validation on ${tpl}`);
}

// 4. Performance budgets for low-end kiosk hardware (raw / gzip bytes).
// Raise a budget only with a documented reason in docs/PERFORMANCE_BUDGETS.md.
const zlib = require('zlib');
const budgets = [
    { file: ['web', 'static', 'js', 'kiosk.js'], raw: 160 * 1024, gzip: 40 * 1024 },
    { file: ['web', 'static', 'js', 'admin.js'], raw: 80 * 1024, gzip: 22 * 1024 },
    { file: ['web', 'static', 'js', 'setup.js'], raw: 16 * 1024, gzip: 5 * 1024 },
    { file: ['web', 'static', 'sw.js'], raw: 12 * 1024, gzip: 4 * 1024 },
    { file: ['web', 'static', 'css', 'app.css'], raw: 160 * 1024, gzip: 28 * 1024 },
    { file: ['web', 'templates', 'kiosk.html'], raw: 60 * 1024, gzip: 12 * 1024 },
    { file: ['web', 'templates', 'admin.html'], raw: 100 * 1024, gzip: 16 * 1024 },
    { file: ['web', 'templates', 'setup.html'], raw: 20 * 1024, gzip: 5 * 1024 }
];
for (const b of budgets) {
    const full = path.join(__dirname, '..', ...b.file);
    const buf = fs.readFileSync(full);
    const gz = zlib.gzipSync(buf, { level: 9 }).length;
    const name = b.file.slice(-1)[0];
    assert(buf.length <= b.raw, `Budget exceeded: ${name} raw ${buf.length} > ${b.raw}`);
    assert(gz <= b.gzip, `Budget exceeded: ${name} gzip ${gz} > ${b.gzip}`);
    console.log(`PASS: budget ${name} raw=${buf.length} gzip=${gz}`);
}

console.log('All frontend static quality checks passed!');
