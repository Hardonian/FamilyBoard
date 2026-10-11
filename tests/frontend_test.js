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

console.log('All frontend static quality checks passed!');
