#!/bin/bash

# 1. Load variables from .env properly
if [ -f .env ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    if [[ ! "$line" =~ ^# ]] && [[ "$line" == *=* ]]; then
      key=$(echo "$line" | cut -d'=' -f1)
      value=$(echo "$line" | cut -d'=' -f2- | sed 's/^"//;s/"$//')
      export "$key=$value"
    fi
  done < .env
fi

# Configuration
PROJECT_KEY="queueBuzz-server"
SONAR_URL="http://localhost:9000"
SCAN_DIR=".scannerwork"

if [ -z "$SONAR_TOKEN" ]; then
  echo "⚠️ SONAR_TOKEN is not set. Checking .env..."
fi

echo "🚀 Starting SonarQube Analysis (Backend)..."

# 2. Run the Native Scanner
if npx @sonar/scan; then
    echo "✅ Scan successful. Preparing report directory..."
    
    # 3. Ensure the directory exists
    mkdir -p "$SCAN_DIR"
    
    # 4. Fetch the report from the API directly into .scannerwork
    # Note: SONAR_TOKEN must be in .env or environment
    curl -s -u "${SONAR_TOKEN}:" \
         "${SONAR_URL}/api/issues/search?componentKeys=${PROJECT_KEY}&resolved=false&ps=500" \
         -o "$SCAN_DIR/sonar-report.json"

    # 5. Process with jq and save the lean audit to .scannerwork
    if [ -f "$SCAN_DIR/sonar-report.json" ]; then
        jq '[.issues[] | select(.severity == "CRITICAL" or .severity == "BLOCKER" or .severity == "MAJOR") | {file: .component, line: .line, issue: .message, severity: .severity}]' "$SCAN_DIR/sonar-report.json" > "$SCAN_DIR/sonar-lean-audit.json"
        
        echo "📄 Done. Reports saved to $SCAN_DIR/"
        echo "   - Full: $SCAN_DIR/sonar-report.json"
        echo "   - Lean: $SCAN_DIR/sonar-lean-audit.json"
    else
        echo "❌ Error: Failed to fetch report from SonarQube API."
    fi
else
    echo "❌ ERROR: Sonar analysis failed."
    exit 1
fi

echo "🔍 Detecting changes with GitNexus..."

# 5. Run GitNexus detection
npx gitnexus detect-changes --repo queuebuzz
