#!/usr/bin/env bash
set -euo pipefail

# scripts/generate_sbom.sh
# Generates SPDX 2.3 and CycloneDX Software Bill of Materials (SBOM) for FamilyBoard.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$PROJECT_ROOT"

VERSION="$(tr -d '\r\n' < VERSION)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
OUTPUT_DIR="${1:-dist/releases/${VERSION}}"
mkdir -p "$OUTPUT_DIR"

SPDX_FILE="${OUTPUT_DIR}/familyboard-${VERSION}.spdx.json"
CDX_FILE="${OUTPUT_DIR}/familyboard-${VERSION}.cdx.json"
TIMESTAMP="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

echo "Generating SBOM for FamilyBoard v${VERSION} (${COMMIT})..."

# 1. Generate SPDX 2.3 JSON
cat <<EOF > "$SPDX_FILE"
{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "FamilyBoard-${VERSION}",
  "documentNamespace": "https://github.com/Hardonian/FamilyBoard/spdx/${VERSION}",
  "creationInfo": {
    "creators": [
      "Tool: FamilyBoard-SBOM-Generator-1.0",
      "Organization: Hardonian"
    ],
    "created": "${TIMESTAMP}"
  },
  "packages": [
    {
      "name": "github.com/Hardonian/FamilyBoard",
      "SPDXID": "SPDXRef-Package-FamilyBoard",
      "versionInfo": "${VERSION}",
      "downloadLocation": "git+https://github.com/Hardonian/FamilyBoard.git@${COMMIT}",
      "filesAnalyzed": false,
      "licenseConcluded": "MIT",
      "licenseDeclared": "MIT",
      "supplier": "Organization: Hardonian",
      "description": "Sovereign smart household calendar appliance"
    }
  ],
  "relationships": [
    {
      "spdxElementId": "SPDXRef-DOCUMENT",
      "relationshipType": "DESCRIBES",
      "relatedSpdxElement": "SPDXRef-Package-FamilyBoard"
    }
  ]
}
EOF

# 2. Generate CycloneDX 1.5 JSON
cat <<EOF > "$CDX_FILE"
{
  "\$schema": "http://cyclonedx.org/schema/bom-1.5.schema.json",
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "serialNumber": "urn:uuid:$(od -x /dev/urandom | head -1 | awk '{OFS="-"; print $2$3,$4,$5,$6,$7$8$9}')",
  "version": 1,
  "metadata": {
    "timestamp": "${TIMESTAMP}",
    "tools": [
      {
        "vendor": "Hardonian",
        "name": "FamilyBoard-SBOM-Generator",
        "version": "1.0"
      }
    ],
    "component": {
      "type": "application",
      "name": "FamilyBoard",
      "version": "${VERSION}",
      "description": "Sovereign smart household calendar appliance",
      "licenses": [
        {
          "license": {
            "id": "MIT"
          }
        }
      ]
    }
  },
  "components": []
}
EOF

echo "SBOM files generated successfully:"
echo "  - ${SPDX_FILE}"
echo "  - ${CDX_FILE}"
