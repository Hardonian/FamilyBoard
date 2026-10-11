#!/usr/bin/env bash
set -euo pipefail

echo "=== Running Frontend Quality Gate ==="

node --check web/static/js/kiosk.js
node --check web/static/js/admin.js
node --check web/static/sw.js

node tests/frontend_test.js

echo "Frontend quality gate passed!"
